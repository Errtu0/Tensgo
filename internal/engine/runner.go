package engine

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

// Config defines the parameters for a load test run.
type Config struct {
	URL         string
	Method      string
	Headers     map[string]string
	Body        string
	Concurrency int
	Duration    time.Duration
	Timeout     time.Duration
	Completed   *atomic.Int64
}

// Run executes the HTTP load test according to cfg and returns all collected results
// along with the actual elapsed test duration.
func Run(cfg Config) ([]metrics.Result, time.Duration) {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.Method == "" {
		cfg.Method = http.MethodGet
	}

	transport := &http.Transport{
		MaxIdleConns:        cfg.Concurrency,
		MaxIdleConnsPerHost: cfg.Concurrency,
		IdleConnTimeout:     90 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
	}
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	defer cancel()

	resultsCh := make(chan metrics.Result, cfg.Concurrency*128)
	var wg sync.WaitGroup

	testStart := time.Now()

	for range cfg.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}

				var bodyReader io.Reader
				if cfg.Body != "" {
					bodyReader = strings.NewReader(cfg.Body)
				}

				req, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.URL, bodyReader)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					resultsCh <- metrics.Result{Err: err}
					if cfg.Completed != nil {
						cfg.Completed.Add(1)
					}
					continue
				}

				for k, v := range cfg.Headers {
					req.Header.Set(k, v)
				}

				reqStart := time.Now()
				resp, err := client.Do(req)
				elapsed := time.Since(reqStart)

				if err != nil {
					if ctx.Err() != nil {
						return
					}
					resultsCh <- metrics.Result{
						Duration: elapsed,
						Err:      err,
					}
					if cfg.Completed != nil {
						cfg.Completed.Add(1)
					}
					continue
				}

				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()

				resultsCh <- metrics.Result{
					Duration:   elapsed,
					StatusCode: resp.StatusCode,
				}
				if cfg.Completed != nil {
					cfg.Completed.Add(1)
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var results []metrics.Result
	for res := range resultsCh {
		results = append(results, res)
	}

	return results, time.Since(testStart)
}
