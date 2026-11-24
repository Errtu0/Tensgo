#!/usr/bin/env bash
set -euo pipefail

TARGET_HOST="127.0.0.1:9090"
TARGET_URL="http://${TARGET_HOST}/json"
HEALTH_URL="http://${TARGET_HOST}/health"
DURATION="10s"
COOLDOWN_SEC=5

echo "==> Building tensgo (release flags)..."
go build -trimpath -ldflags="-s -w" -o tensgo .
TENSGO_MB=$(awk "BEGIN {printf \"%.2f\", $(wc -c < tensgo)/1048576}")

echo "==> Starting benchmark target server on ${TARGET_HOST}..."
go run ./benchmarks/target_server.go -addr "${TARGET_HOST}" >/dev/null 2>&1 &
SERVER_PID=$!
trap 'kill ${SERVER_PID} 2>/dev/null || true' EXIT

for _ in $(seq 1 40); do
  if curl -fsS "${HEALTH_URL}" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done

run_with_peak_rss() {
  local out_file="$1"
  shift
  "$@" >"${out_file}" 2>&1 &
  local pid=$!
  local peak_kb=0
  while kill -0 "${pid}" 2>/dev/null; do
    local rss_kb
    rss_kb=$(ps -o rss= -p "${pid}" 2>/dev/null | awk '{print $1}' || echo 0)
    if [ -n "${rss_kb}" ] && [ "${rss_kb}" -gt "${peak_kb}" ] 2>/dev/null; then
      peak_kb="${rss_kb}"
    fi
    sleep 0.05
  done
  wait "${pid}" || true
  awk "BEGIN {printf \"%.1f\", ${peak_kb}/1024}"
}

printf "\n%-8s | %-4s | %-10s | %-14s | %-10s | %-8s | %-8s | %-8s | %-9s | %-9s\n" \
  "Tool" "VUs" "Requests" "Errors" "RPS" "P50(ms)" "P95(ms)" "P99(ms)" "PeakMB" "BinaryMB"
printf "%s\n" "------------------------------------------------------------------------------------------------------------------"

FIRST_RUN=1
for VUS in 50 250; do
  if [ "${FIRST_RUN}" -eq 0 ]; then
    sleep "${COOLDOWN_SEC}"
  fi
  FIRST_RUN=0

  # 1. tensgo
  JSON_OUT="/tmp/tensgo-bench-${VUS}.json"
  LOG_OUT="/tmp/tensgo-log-${VUS}.txt"
  rm -f "${JSON_OUT}" "${LOG_OUT}"
  PEAK_MB=$(run_with_peak_rss "${LOG_OUT}" ./tensgo -u "${TARGET_URL}" -c "${VUS}" -d "${DURATION}" --no-tui --output-format json --output-file "${JSON_OUT}")
  if command -v jq >/dev/null 2>&1 && [ -f "${JSON_OUT}" ]; then
    REQS=$(jq -r '.metrics.total_requests' "${JSON_OUT}")
    ERRS=$(jq -r '"\(.metrics.error_count) (\(.metrics.error_rate_pct)%)"' "${JSON_OUT}")
    RPS=$(jq -r '.metrics.rps' "${JSON_OUT}")
    P50=$(jq -r '.metrics.latency.p50_ms' "${JSON_OUT}")
    P95=$(jq -r '.metrics.latency.p95_ms' "${JSON_OUT}")
    P99=$(jq -r '.metrics.latency.p99_ms' "${JSON_OUT}")
  else
    REQS="-"; ERRS="-"; RPS="-"; P50="-"; P95="-"; P99="-"
  fi
  printf "%-8s | %-4s | %-10s | %-14s | %-10s | %-8s | %-8s | %-8s | %-9s | %-9s\n" \
    "tensgo" "${VUS}" "${REQS}" "${ERRS}" "${RPS}" "${P50}" "${P95}" "${P99}" "${PEAK_MB}" "${TENSGO_MB}"
  rm -f "${JSON_OUT}" "${LOG_OUT}"

  # 2. hey
  if command -v hey >/dev/null 2>&1; then
    sleep "${COOLDOWN_SEC}"
    HEY_BIN=$(command -v hey)
    HEY_MB=$(awk "BEGIN {printf \"%.2f\", $(wc -c < "${HEY_BIN}")/1048576}")
    HEY_LOG="/tmp/hey-bench-${VUS}.txt"
    PEAK_HEY=$(run_with_peak_rss "${HEY_LOG}" hey -z "${DURATION}" -c "${VUS}" "${TARGET_URL}")
    HEY_RPS=$(awk '/Requests\/sec:/ {printf "%.1f", $2}' "${HEY_LOG}")
    HEY_P50=$(awk '/50% in/ {printf "%.2f", $3 * 1000}' "${HEY_LOG}")
    HEY_P95=$(awk '/95% in/ {printf "%.2f", $3 * 1000}' "${HEY_LOG}")
    HEY_P99=$(awk '/99% in/ {printf "%.2f", $3 * 1000}' "${HEY_LOG}")
    HEY_REQS=$(awk '/\[200\]/ {print $2}' "${HEY_LOG}")
    printf "%-8s | %-4s | %-10s | %-14s | %-10s | %-8s | %-8s | %-8s | %-9s | %-9s\n" \
      "hey" "${VUS}" "${HEY_REQS:-0}" "0 (0.00%)" "${HEY_RPS:-0}" "${HEY_P50:--}" "${HEY_P95:--}" "${HEY_P99:--}" "${PEAK_HEY}" "${HEY_MB}"
    rm -f "${HEY_LOG}"
  fi

  # 3. k6
  if command -v k6 >/dev/null 2>&1; then
    sleep "${COOLDOWN_SEC}"
    K6_BIN=$(command -v k6)
    K6_MB=$(awk "BEGIN {printf \"%.2f\", $(wc -c < "${K6_BIN}")/1048576}")
    K6_JS="/tmp/tensgo-k6-${VUS}.js"
    K6_SUM="/tmp/tensgo-k6-sum-${VUS}.json"
    K6_LOG="/tmp/tensgo-k6-log-${VUS}.txt"
    echo "import http from 'k6/http'; export default function () { http.get('${TARGET_URL}'); }" > "${K6_JS}"
    PEAK_K6=$(run_with_peak_rss "${K6_LOG}" k6 run --quiet --vus "${VUS}" --duration "${DURATION}" \
      --summary-trend-stats="min,avg,med,p(90),p(95),p(99),max" --summary-export="${K6_SUM}" "${K6_JS}")
    if command -v jq >/dev/null 2>&1 && [ -f "${K6_SUM}" ]; then
      K6_REQS=$(jq -r '.metrics.http_reqs.count // .metrics.http_reqs.values.count' "${K6_SUM}")
      K6_RPS=$(jq -r '(.metrics.http_reqs.rate // .metrics.http_reqs.values.rate) | (. * 10 | round / 10)' "${K6_SUM}")
      K6_P50=$(jq -r '(.metrics.http_req_duration.med // .metrics.http_req_duration.values.med) | (. * 100 | round / 100)' "${K6_SUM}")
      K6_P95=$(jq -r '(.metrics.http_req_duration["p(95)"] // .metrics.http_req_duration.values["p(95)"]) | (. * 100 | round / 100)' "${K6_SUM}")
      K6_P99=$(jq -r '(.metrics.http_req_duration["p(99)"] // .metrics.http_req_duration.values["p(99)"]) | (. * 100 | round / 100)' "${K6_SUM}")
      printf "%-8s | %-4s | %-10s | %-14s | %-10s | %-8s | %-8s | %-8s | %-9s | %-9s\n" \
        "k6" "${VUS}" "${K6_REQS}" "0 (0.00%)" "${K6_RPS}" "${K6_P50}" "${K6_P95}" "${K6_P99}" "${PEAK_K6}" "${K6_MB}"
    fi
    rm -f "${K6_JS}" "${K6_SUM}" "${K6_LOG}"
  fi
done
