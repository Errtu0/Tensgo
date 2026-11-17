package profiles

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Errtu0/tensgo/internal/metrics"
)

const (
	ProfileSpike  = "spike"
	ProfileRampUp = "ramp-up"
	ProfileSoak   = "soak"
)

// Config defines parameters for a dynamic traffic profile test.
type Config struct {
	Type          string
	URL           string
	Method        string
	Headers       map[string]string
	Body          string
	Timeout       time.Duration
	BaselineVUs   int
	PeakVUs       int
	PhaseDuration time.Duration
	StartVUs      int
	StepVUs       int
	MaxVUs        int
	SoakVUs       int
	SoakDuration  time.Duration
}

// Bucket captures per-second or per-step metrics during a profile run.
type Bucket struct {
	Index     int
	Phase     string
	VUs       int
	Requests  int
	RPS       float64
	P95       time.Duration
	ErrorRate float64
}

// LiveSnapshot exposes real-time telemetry to the TUI while a profile is running.
type LiveSnapshot struct {
	Phase         string
	ActiveVUs     int
	Elapsed       time.Duration
	TotalDuration time.Duration
	CurrentRPS    float64
	CurrentP95    time.Duration
	CurrentErrPct float64
	TotalRequests int64
}

// Report contains aggregated metrics and profile-specific diagnostics.
type Report struct {
	Type             string
	Summary          metrics.Summary
	Buckets          []Bucket
	RecoveryTime     time.Duration
	Recovered        bool
	PeakP95          time.Duration
	PeakErrorRate    float64
	BreakingPointVUs int
	BreakingPointRPS float64
	BreakingReason   string
	MaxHealthyVUs    int
	MaxHealthyRPS    float64
	StoppedEarly     bool
	FirstQuarterP95  time.Duration
	LastQuarterP95   time.Duration
	DegradationPct   float64
}

type timedResult struct {
	res metrics.Result
	at  time.Duration
}

// Runner executes dynamic VU profiles and tracks real-time snapshots.
type Runner struct {
	cfg     Config
	client  *http.Client
	mu      sync.RWMutex
	snap    LiveSnapshot
	cancels []context.CancelFunc
	wg      sync.WaitGroup

	resultsMu sync.Mutex
	results   []timedResult
	totalReqs atomic.Int64
	startTime time.Time
}

// NewRunner initializes a profile runner with defaults applied.
func NewRunner(cfg Config) *Runner {
	if cfg.Method == "" {
		cfg.Method = http.MethodGet
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.BaselineVUs <= 0 {
		cfg.BaselineVUs = 20
	}
	if cfg.PeakVUs <= 0 {
		cfg.PeakVUs = 300
	}
	if cfg.PhaseDuration <= 0 {
		if cfg.Type == ProfileRampUp {
			cfg.PhaseDuration = 3 * time.Second
		} else {
			cfg.PhaseDuration = 5 * time.Second
		}
	}
	if cfg.StartVUs <= 0 {
		cfg.StartVUs = 10
	}
	if cfg.StepVUs <= 0 {
		cfg.StepVUs = 30
	}
	if cfg.MaxVUs <= 0 {
		cfg.MaxVUs = 400
	}
	if cfg.SoakVUs <= 0 {
		cfg.SoakVUs = 50
	}
	if cfg.SoakDuration <= 0 {
		cfg.SoakDuration = 30 * time.Second
	}

	maxIdle := max(cfg.PeakVUs, max(cfg.MaxVUs, cfg.SoakVUs))
	if maxIdle < 64 {
		maxIdle = 64
	}

	transport := &http.Transport{
		MaxIdleConns:        maxIdle,
		MaxIdleConnsPerHost: maxIdle,
		IdleConnTimeout:     90 * time.Second,
	}

	return &Runner{
		cfg: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
	}
}

// Snapshot returns the latest live telemetry snapshot safely.
func (r *Runner) Snapshot() LiveSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := r.snap
	s.TotalRequests = r.totalReqs.Load()
	if !r.startTime.IsZero() {
		s.Elapsed = time.Since(r.startTime)
	}
	return s
}

func (r *Runner) setPhase(phase string, vus int, totalDur time.Duration) {
	r.mu.Lock()
	r.snap.Phase = phase
	r.snap.ActiveVUs = vus
	r.snap.TotalDuration = totalDur
	r.mu.Unlock()
}

func (r *Runner) updateLiveStats(window []metrics.Result, windowDur time.Duration) {
	sum := metrics.Calculate(window, windowDur)
	var errPct float64
	if sum.TotalRequests > 0 {
		errPct = float64(sum.ErrorCount) / float64(sum.TotalRequests) * 100.0
	}
	r.mu.Lock()
	r.snap.CurrentRPS = sum.RPS
	r.snap.CurrentP95 = sum.P95
	r.snap.CurrentErrPct = errPct
	r.mu.Unlock()
}

func (r *Runner) scaleTo(ctx context.Context, targetVUs int) {
	current := len(r.cancels)
	if targetVUs > current {
		for i := 0; i < targetVUs-current; i++ {
			wCtx, wCancel := context.WithCancel(ctx)
			r.cancels = append(r.cancels, wCancel)
			r.wg.Add(1)
			go r.worker(wCtx)
		}
	} else if targetVUs < current {
		for i := current - 1; i >= targetVUs; i-- {
			r.cancels[i]()
		}
		r.cancels = r.cancels[:targetVUs]
	}
}

func (r *Runner) stopAll() {
	for _, cancel := range r.cancels {
		cancel()
	}
	r.cancels = nil
	r.wg.Wait()
	r.client.CloseIdleConnections()
}

func (r *Runner) worker(ctx context.Context) {
	defer r.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}

		var bodyReader io.Reader
		if r.cfg.Body != "" {
			bodyReader = strings.NewReader(r.cfg.Body)
		}

		req, err := http.NewRequestWithContext(ctx, r.cfg.Method, r.cfg.URL, bodyReader)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			r.record(metrics.Result{Err: err})
			continue
		}

		for k, v := range r.cfg.Headers {
			req.Header.Set(k, v)
		}

		reqStart := time.Now()
		resp, err := r.client.Do(req)
		elapsed := time.Since(reqStart)

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			r.record(metrics.Result{Duration: elapsed, Err: err})
			continue
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		r.record(metrics.Result{
			Duration:   elapsed,
			StatusCode: resp.StatusCode,
		})
	}
}

func (r *Runner) record(res metrics.Result) {
	at := time.Since(r.startTime)
	r.resultsMu.Lock()
	r.results = append(r.results, timedResult{res: res, at: at})
	r.resultsMu.Unlock()
	r.totalReqs.Add(1)
}

func (r *Runner) sliceSince(offset int) ([]metrics.Result, int) {
	r.resultsMu.Lock()
	defer r.resultsMu.Unlock()
	if offset >= len(r.results) {
		return nil, len(r.results)
	}
	out := make([]metrics.Result, len(r.results)-offset)
	for i, tr := range r.results[offset:] {
		out[i] = tr.res
	}
	return out, len(r.results)
}

// Run executes the configured chaos profile and returns the final report.
func (r *Runner) Run(ctx context.Context) Report {
	r.startTime = time.Now()
	defer r.stopAll()

	switch r.cfg.Type {
	case ProfileSpike:
		return r.runSpike(ctx)
	case ProfileRampUp:
		return r.runRampUp(ctx)
	default:
		return r.runSoak(ctx)
	}
}

func (r *Runner) runSpike(ctx context.Context) Report {
	phaseDur := r.cfg.PhaseDuration
	totalDur := phaseDur * 3
	rep := Report{Type: ProfileSpike}

	phases := []struct {
		name string
		vus  int
	}{
		{"BASELINE", r.cfg.BaselineVUs},
		{"SPIKE", r.cfg.PeakVUs},
		{"RECOVERY", r.cfg.BaselineVUs},
	}

	offset := 0
	var recoveryStart time.Time
	recovered := false
	var recoveryTime time.Duration

	for _, ph := range phases {
		r.setPhase(ph.name, ph.vus, totalDur)
		r.scaleTo(ctx, ph.vus)

		if ph.name == "RECOVERY" {
			recoveryStart = time.Now()
		}

		secs := int(phaseDur.Seconds())
		if secs < 1 {
			secs = 1
		}
		for s := 0; s < secs; s++ {
			select {
			case <-ctx.Done():
				return r.finalizeReport(rep)
			case <-time.After(1 * time.Second):
			}

			window, nextOffset := r.sliceSince(offset)
			offset = nextOffset
			r.updateLiveStats(window, time.Second)

			sum := metrics.Calculate(window, time.Second)
			var errPct float64
			if sum.TotalRequests > 0 {
				errPct = float64(sum.ErrorCount) / float64(sum.TotalRequests) * 100.0
			}

			rep.Buckets = append(rep.Buckets, Bucket{
				Index:     len(rep.Buckets) + 1,
				Phase:     ph.name,
				VUs:       ph.vus,
				Requests:  sum.TotalRequests,
				RPS:       sum.RPS,
				P95:       sum.P95,
				ErrorRate: errPct,
			})

			if ph.name == "SPIKE" {
				if sum.P95 > rep.PeakP95 {
					rep.PeakP95 = sum.P95
				}
				if errPct > rep.PeakErrorRate {
					rep.PeakErrorRate = errPct
				}
			}

			if ph.name == "RECOVERY" && !recovered && errPct <= 1.0 && sum.P95 < 500*time.Millisecond {
				recovered = true
				recoveryTime = time.Since(recoveryStart).Round(100 * time.Millisecond)
			}
		}
	}

	rep.Recovered = recovered
	rep.RecoveryTime = recoveryTime
	return r.finalizeReport(rep)
}

func (r *Runner) runRampUp(ctx context.Context) Report {
	stepDur := r.cfg.PhaseDuration
	rep := Report{Type: ProfileRampUp}

	vus := r.cfg.StartVUs
	offset := 0
	stepNum := 1
	estTotal := stepDur * time.Duration((r.cfg.MaxVUs-r.cfg.StartVUs)/r.cfg.StepVUs+1)

	for vus <= r.cfg.MaxVUs {
		phaseName := "STEP " + itoa(stepNum)
		r.setPhase(phaseName, vus, estTotal)
		r.scaleTo(ctx, vus)

		secs := int(stepDur.Seconds())
		if secs < 1 {
			secs = 1
		}
		stepStartOffset := offset
		for s := 0; s < secs; s++ {
			select {
			case <-ctx.Done():
				return r.finalizeReport(rep)
			case <-time.After(1 * time.Second):
			}
			secWindow, nextOffset := r.sliceSince(offset)
			offset = nextOffset
			r.updateLiveStats(secWindow, time.Second)
		}

		stepWindow, _ := r.sliceSince(stepStartOffset)
		sum := metrics.Calculate(stepWindow, stepDur)
		var errPct float64
		if sum.TotalRequests > 0 {
			errPct = float64(sum.ErrorCount) / float64(sum.TotalRequests) * 100.0
		}

		rep.Buckets = append(rep.Buckets, Bucket{
			Index:     stepNum,
			Phase:     phaseName,
			VUs:       vus,
			Requests:  sum.TotalRequests,
			RPS:       sum.RPS,
			P95:       sum.P95,
			ErrorRate: errPct,
		})

		if sum.P95 > time.Second || errPct > 5.0 {
			rep.StoppedEarly = true
			rep.BreakingPointVUs = vus
			rep.BreakingPointRPS = sum.RPS
			if sum.P95 > time.Second {
				rep.BreakingReason = "P95 latency > 1000ms (" + metrics.FormatDuration(sum.P95) + ")"
			} else {
				rep.BreakingReason = "Error rate > 5% (" + formatPct(errPct) + ")"
			}
			break
		}

		rep.MaxHealthyVUs = vus
		rep.MaxHealthyRPS = sum.RPS
		vus += r.cfg.StepVUs
		stepNum++
	}

	return r.finalizeReport(rep)
}

func (r *Runner) runSoak(ctx context.Context) Report {
	dur := r.cfg.SoakDuration
	rep := Report{Type: ProfileSoak}

	r.setPhase("SOAK", r.cfg.SoakVUs, dur)
	r.scaleTo(ctx, r.cfg.SoakVUs)

	secs := int(dur.Seconds())
	if secs < 4 {
		secs = 4
	}
	offset := 0
	for s := 0; s < secs; s++ {
		select {
		case <-ctx.Done():
			return r.finalizeReport(rep)
		case <-time.After(1 * time.Second):
		}

		window, nextOffset := r.sliceSince(offset)
		offset = nextOffset
		r.updateLiveStats(window, time.Second)

		sum := metrics.Calculate(window, time.Second)
		var errPct float64
		if sum.TotalRequests > 0 {
			errPct = float64(sum.ErrorCount) / float64(sum.TotalRequests) * 100.0
		}

		rep.Buckets = append(rep.Buckets, Bucket{
			Index:     s + 1,
			Phase:     "SOAK",
			VUs:       r.cfg.SoakVUs,
			Requests:  sum.TotalRequests,
			RPS:       sum.RPS,
			P95:       sum.P95,
			ErrorRate: errPct,
		})
	}

	rep = r.finalizeReport(rep)

	// Compare first-quarter P95 vs last-quarter P95.
	r.resultsMu.Lock()
	var q1, q4 []metrics.Result
	q1End := dur / 4
	q4Start := (dur * 3) / 4
	for _, tr := range r.results {
		if tr.at <= q1End {
			q1 = append(q1, tr.res)
		} else if tr.at >= q4Start {
			q4 = append(q4, tr.res)
		}
	}
	r.resultsMu.Unlock()

	s1 := metrics.Calculate(q1, q1End)
	s4 := metrics.Calculate(q4, dur-q4Start)
	rep.FirstQuarterP95 = s1.P95
	rep.LastQuarterP95 = s4.P95
	if s1.P95 > 0 {
		rep.DegradationPct = float64(s4.P95-s1.P95) / float64(s1.P95) * 100.0
	}

	return rep
}

func (r *Runner) finalizeReport(rep Report) Report {
	elapsed := time.Since(r.startTime)
	r.resultsMu.Lock()
	raw := make([]metrics.Result, len(r.results))
	for i, tr := range r.results {
		raw[i] = tr.res
	}
	r.resultsMu.Unlock()

	rep.Summary = metrics.Calculate(raw, elapsed)
	return rep
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func formatPct(v float64) string {
	whole := int(v)
	frac := int((v - float64(whole)) * 100)
	if frac < 0 {
		frac = -frac
	}
	return itoa(whole) + "." + itoa(frac) + "%"
}
