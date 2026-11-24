package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

var (
	healthPayload = []byte(`{"status":"ok"}`)
	jsonPayload   = []byte(`{"id":"ord-9001","service":"tensgo-bench","status":"ok","items":[{"sku":"TENS-01","qty":2,"price":49.99},{"sku":"TENS-02","qty":1,"price":99.50}],"meta":{"region":"eu-central-1","cached":true}}`)
	healthLenStr  = strconv.Itoa(len(healthPayload))
	jsonLenStr    = strconv.Itoa(len(jsonPayload))
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9090", "Benchmark server listen address")
	flag.Parse()

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", healthLenStr)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(healthPayload)
	})

	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", jsonLenStr)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jsonPayload)
	})

	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, io.LimitReader(r.Body, 1<<20))
		_ = r.Body.Close()
	})

	srv := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("tensgo benchmark target server listening on http://%s (/health, /json, /echo)", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
