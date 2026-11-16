# tensgo

**tensgo** is a fast, lightweight HTTP load testing CLI written in Go, featuring a live Charmbracelet TUI progress view, styled terminal summary cards, CI/CD quality gate thresholds, and standalone dark-mode HTML reporting.

## Features

- **High-Throughput Worker Engine**: Connection-pooled `http.Client` (`MaxIdleConns` and `MaxIdleConnsPerHost` tuned to concurrency) with proper response body draining to prevent socket leaks.
- **Custom HTTP Requests**: Supports custom methods (`-m`), multiple headers (`-H`), and request bodies (`-b`).
- **Live Terminal UI**: Real-time `bubbletea` spinner, progress bar, elapsed timer, and completed request counter (automatically bypassed in non-TTY environments or via `--no-tui`).
- **Styled Result Cards**: Dark-theme `lipgloss` rounded-border cards for throughput, latency percentiles (`Min`, `P50`, `P95`, `P99`, `Max`), and color-coded HTTP status code distribution.
- **CI/CD Quality Gates**: Enforce `--max-p95` and `--max-error-rate` thresholds with automatic `PASS` / `FAIL` reporting and non-zero exit codes (`os.Exit(1)`) on violations.
- **Standalone HTML Reports**: Zero-dependency dark-mode HTML report generation via `--html`.

## Installation

```bash
go build -o tensgo.exe .
```

Or cross-compile for Windows, Linux, and macOS using PowerShell:

```powershell
.\build.ps1
```

## Usage

```bash
tensgo -u https://example.com -c 20 -d 15s
```

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-u` | *(required)* | Target URL to load test |
| `-m` | `GET` | HTTP method |
| `-H` | `""` | HTTP header in `'Key: Value'` format (repeatable) |
| `-b` | `""` | Request body string |
| `-c` | `10` | Number of concurrent workers |
| `-d` | `10s` | Test duration |
| `-t` | `5s` | Per-request timeout |
| `--no-tui` | `false` | Disable live TUI progress animation |
| `--max-p95` | `0` | CI/CD threshold: maximum allowed P95 latency (e.g. `300ms`) |
| `--max-error-rate` | `-1` | CI/CD threshold: maximum allowed error rate in `%` (e.g. `1.0`) |
| `--html` | `""` | Path to write a standalone dark-mode HTML report |

### Examples

**POST load test with headers, body, HTML report, and CI/CD thresholds:**

```bash
tensgo -u http://localhost:8080/echo \
  -m POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer token123" \
  -b '{"hello":"world"}' \
  -c 25 \
  -d 10s \
  --max-p95 300ms \
  --max-error-rate 1.0 \
  --html report.html
```

**CI/CD Pipeline Mode (non-interactive):**

```bash
tensgo -u http://localhost:8080/health -c 50 -d 30s --no-tui --max-p95 150ms --max-error-rate 0.5
```

## Local Mock Server

A local test server is included in `examples/mock_server.go` for rate-limit-free testing:

```bash
go run ./examples/mock_server.go -addr :8080
```

Available endpoints:
- `GET /health` — Immediate `200 OK` JSON response
- `GET /slow` — `200 OK` with random `50–150ms` artificial latency
- `POST /echo` — Validates JSON payload and echoes it back with `200 OK`
