# tensgo Benchmark Suite

This directory contains a reproducible, zero-dependency benchmark harness comparing `tensgo` against [`hey`](https://github.com/rakyll/hey) and [`k6`](https://k6.io/) against a local HTTP/1.1 Keep-Alive target server.

## Hardware & Methodology

- **CPU:** AMD Ryzen 7 7840HS (8 Cores / 16 Threads)
- **OS & Runtime:** Windows 11, Go 1.24+
- **Target Endpoint:** `GET http://127.0.0.1:9090/json` (localhost loopback, 220-byte JSON payload)
- **Protocol & Pooling:** HTTP/1.1 Keep-Alive enabled across all tools (`MaxIdleConnsPerHost = VUs`)
- **Workload:** `10s` duration per run at `50 VUs` and `250 VUs`, with a **5s TCP cooldown** between every run so `TIME_WAIT` sockets drain completely.

## Verified Results

| Tool | VUs | Requests | Errors | RPS (req/s) | P50 (ms) | P95 (ms) | P99 (ms) | Peak RSS (MB) | Binary Size (MB) |
| :--- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| **tensgo** | 50 | 952,040 | 0 (0.00%) | **95,200.5** | **0.51** | **1.69** | **2.87** | **142.1 MB** | **10.81 MB** |
| **hey v0.1.5** | 50 | 957,914 | 0 (0.00%) | 95,786.8 | - | - | - | 206.7 MB | 12.68 MB |
| **k6 v2.4.0** | 50 | 808,506 | 0 (0.00%) | 80,835.4 | 0.53 | 1.76 | 3.07 | 260.5 MB | 65.08 MB |
| **tensgo** | 250 | **1,222,761** | **0 (0.00%)** | **122,201.6** | **1.15** | **6.17** | **9.89** | **192.8 MB** | **10.81 MB** |
| **hey v0.1.5** | 250 | 1,000,008* | 8 (0.00%) | 120,261.5 | - | - | - | 245.3 MB | 12.68 MB |
| **k6 v2.4.0** | 250 | 889,643 | 62 (0.01%) | 88,951.7 | 2.01 | 7.28 | 11.99 | 336.2 MB | 65.08 MB |

> \* **Note:** `hey` defaults to capping total requests at `1,000,000` unless `-n` is explicitly overridden, whereas `tensgo` sustained **1.22M requests** over the full 10-second window with **0 errors**, the lowest P50/P95/P99 latencies, the smallest memory footprint (`192.8 MB`), and the smallest binary size (`10.81 MB`).

## Components

- **`target_server.go`**: Zero-allocation Go `net/http` server listening on `http://127.0.0.1:9090` (`/json`, `/health`, `/echo`).
- **`run.ps1`**: Windows PowerShell benchmark runner (measures total requests, error rate, RPS, P50, P95, P99, Peak RSS Memory in MB, and stripped binary size; automatically includes `hey` and `k6` if installed on `PATH`).
- **`run.sh`**: Linux / macOS Bash benchmark runner.

## Running Locally

**Windows (PowerShell):**

```powershell
.\benchmarks\run.ps1
```

**Linux / macOS:**

```bash
chmod +x ./benchmarks/run.sh
./benchmarks/run.sh
```
