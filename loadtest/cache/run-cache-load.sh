#!/usr/bin/env bash
# Load test cache: mengukur latensi baca produk dan hit-rate Redis.
#
# Prasyarat:
#   - aplikasi Go berjalan (default http://localhost:8080)
#   - container Redis berjalan (default: learn-go-redis)
#   - k6 terpasang
#
# Pemakaian:
#   ./loadtest/cache/run-cache-load.sh                  # MODE=hot
#   MODE=both ./loadtest/cache/run-cache-load.sh        # hot lalu stampede
#   MODE=stampede VUS=200 DURATION=1m ./loadtest/cache/run-cache-load.sh
#   SMOKE=1 ./loadtest/cache/run-cache-load.sh          # cek cepat 5 detik
#
# Opsi env:
#   BASE_URL, MODE (hot|stampede|both), VUS, DURATION,
#   HOT_KEYS, INVALIDATE_RPS, REDIS_CONTAINER,
#   K6_OUT (mis. influxdb=http://localhost:8086/k6 untuk Grafana)
set -euo pipefail

cd "$(dirname "$0")/../.."

BASE_URL="${BASE_URL:-http://localhost:8080}"
MODE="${MODE:-hot}"
VUS="${VUS:-100}"
DURATION="${DURATION:-30s}"
HOT_KEYS="${HOT_KEYS:-5}"
INVALIDATE_RPS="${INVALIDATE_RPS:-1}"
REDIS_CONTAINER="${REDIS_CONTAINER:-learn-go-redis}"
K6_OUT="${K6_OUT:-}"

if ! command -v k6 >/dev/null 2>&1; then
  echo "ERROR: k6 tidak ditemukan di PATH"
  exit 1
fi

if ! curl -s -o /dev/null --max-time 2 "$BASE_URL/"; then
  echo "ERROR: aplikasi belum berjalan di $BASE_URL (jalankan: go run main.go)"
  exit 1
fi

if ! docker exec "$REDIS_CONTAINER" redis-cli ping >/dev/null 2>&1; then
  echo "ERROR: Redis '$REDIS_CONTAINER' tidak berjalan (docker compose up -d redis)"
  exit 1
fi

# Hapus hanya key produk, jangan FLUSHALL (Redis mungkin dipakai hal lain).
reset_cache() {
  docker exec "$REDIS_CONTAINER" redis-cli --scan --pattern 'productId:*' \
    | xargs -r -n 100 docker exec -i "$REDIS_CONTAINER" redis-cli DEL >/dev/null 2>&1 || true
}

# Output: "<hits> <misses>"
redis_hits_misses() {
  docker exec "$REDIS_CONTAINER" redis-cli INFO stats \
    | awk -F: '
        /^keyspace_hits:/   { h = $2 }
        /^keyspace_misses:/ { m = $2 }
        END { printf "%d %d", h, m }
      '
}

print_delta() {
  awk -v h0="$1" -v m0="$2" -v h1="$3" -v m1="$4" 'BEGIN {
    hits = h1 - h0
    misses = m1 - m0
    total = hits + misses
    printf "Redis keyspace  : hits=%d misses=%d", hits, misses
    if (total > 0) {
      printf " hit_rate=%.2f%%\n", hits * 100 / total
    } else {
      printf " hit_rate=n/a\n"
    }
  }'
}

run_mode() {
  local name="$1"
  local script="$2"

  echo
  echo "=================================================="
  echo "MODE $name | VUs=$VUS | duration=$DURATION"
  echo "=================================================="

  reset_cache
  read -r h0 m0 <<<"$(redis_hits_misses)"

  local args=(-e BASE_URL="$BASE_URL" -e VUS="$VUS" -e DURATION="$DURATION")
  args+=(-e HOT_KEYS="$HOT_KEYS" -e INVALIDATE_RPS="$INVALIDATE_RPS")
  [[ -n "${SMOKE:-}" ]] && args+=(-e SMOKE=1)
  [[ -n "$K6_OUT" ]] && args+=(--out "$K6_OUT")

  k6 run "${args[@]}" "$script"

  read -r h1 m1 <<<"$(redis_hits_misses)"
  echo
  print_delta "$h0" "$m0" "$h1" "$m1"
  echo "Catatan: pada MODE=stampede, perhatikan jumlah misses dan p99/max"
  echo "         cache_read_duration. Naikkan INVALIDATE_RPS untuk efek lebih jelas."
}

case "$MODE" in
  hot)
    run_mode hot loadtest/cache/hot-key.js
    ;;
  stampede)
    run_mode stampede loadtest/cache/stampede.js
    ;;
  both)
    run_mode hot loadtest/cache/hot-key.js
    run_mode stampede loadtest/cache/stampede.js
    ;;
  *)
    echo "ERROR: MODE tidak dikenal: $MODE (pakai hot|stampede|both)"
    exit 1
    ;;
esac
