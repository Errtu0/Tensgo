package profiles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProfilesSpikeAndSoak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Spike profile (3 x 1s phases)
	spikeRunner := NewRunner(Config{
		Type:          ProfileSpike,
		URL:           srv.URL,
		Method:        http.MethodGet,
		BaselineVUs:   2,
		PeakVUs:       5,
		PhaseDuration: time.Second,
		Timeout:       time.Second,
	})
	rep := spikeRunner.Run(context.Background())
	if rep.Summary.TotalRequests == 0 || len(rep.Buckets) != 3 || !rep.Recovered {
		t.Fatalf("unexpected spike report: total=%d buckets=%d recovered=%v", rep.Summary.TotalRequests, len(rep.Buckets), rep.Recovered)
	}
	snap := spikeRunner.Snapshot()
	if snap.TotalRequests == 0 {
		t.Fatalf("expected snapshot TotalRequests > 0")
	}

	// Soak profile (4s minimum)
	soakRunner := NewRunner(Config{
		Type:         ProfileSoak,
		URL:          srv.URL,
		SoakVUs:      2,
		SoakDuration: 4 * time.Second,
		Timeout:      time.Second,
	})
	soakRep := soakRunner.Run(context.Background())
	if soakRep.Summary.TotalRequests == 0 || len(soakRep.Buckets) != 4 {
		t.Fatalf("unexpected soak report: total=%d buckets=%d", soakRep.Summary.TotalRequests, len(soakRep.Buckets))
	}
}

func TestProfileRampUpBreakingPoint(t *testing.T) {
	var failMode atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failMode.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Trigger immediate error-rate breaking point (>5%) on step 1
	failMode.Store(true)
	rampRunner := NewRunner(Config{
		Type:          ProfileRampUp,
		URL:           srv.URL,
		StartVUs:      2,
		StepVUs:       2,
		MaxVUs:        10,
		PhaseDuration: time.Second,
		Timeout:       time.Second,
	})
	rep := rampRunner.Run(context.Background())
	if !rep.StoppedEarly || rep.BreakingPointVUs != 2 {
		t.Fatalf("expected early stop at 2 VUs, got stopped=%v breaking=%d", rep.StoppedEarly, rep.BreakingPointVUs)
	}
}
