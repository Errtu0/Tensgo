package engine

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunHTTP(t *testing.T) {
	var seenAuth atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer test-tok" {
			seenAuth.Add(1)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var completed atomic.Int64
	results, elapsed := Run(Config{
		URL:         srv.URL,
		Method:      http.MethodPost,
		Headers:     map[string]string{"Authorization": "Bearer test-tok"},
		Body:        `{"ok":true}`,
		Concurrency: 2,
		Duration:    150 * time.Millisecond,
		Timeout:     time.Second,
		Completed:   &completed,
	})

	if elapsed <= 0 || len(results) == 0 {
		t.Fatalf("expected requests to run, got count=%d elapsed=%s", len(results), elapsed)
	}
	if completed.Load() != int64(len(results)) {
		t.Fatalf("completed=%d != len(results)=%d", completed.Load(), len(results))
	}
	if seenAuth.Load() == 0 {
		t.Fatalf("expected Authorization header to be sent")
	}
}

func TestRunDefaultsAndInvalidURL(t *testing.T) {
	results, _ := Run(Config{
		URL:         "http://127.0.0.1:1", // closed port
		Concurrency: 0,                    // defaults to 1
		Duration:    80 * time.Millisecond,
		Timeout:     50 * time.Millisecond,
	})
	if len(results) == 0 {
		t.Fatalf("expected at least 1 failed result")
	}
	if results[0].Err == nil {
		t.Fatalf("expected connection error on closed port")
	}
}
