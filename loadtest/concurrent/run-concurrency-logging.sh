#!/usr/bin/env bash
# Menjalankan k6 concurrency dengan log detail:
#   1. k6 mencatat user/VU mana yang update row mana + durasi
#   2. skrip ini menahan row lock dan polling pg_stat_activity
#      untuk mencatat sesi yang benar-benar waiting
#
# Jalankan (aplikasi Go harus sudah berjalan di :8080):
#   ./loadtest/concurrent/run-concurrency-logging.sh
#
# Opsi:
#   VUS=5 ITERATIONS=20 HOLD_SECONDS=6 ./loadtest/concurrent/run-concurrency-logging.sh
set -euo pipefail

cd "$(dirname "$0")/../.."

BASE_URL="${BASE_URL:-http://localhost:8080}"
VUS="${VUS:-5}"
ITERATIONS="${ITERATIONS:-20}"
HOLD_SECONDS="${HOLD_SECONDS:-6}"
STOCK="${STOCK:-1000}"
WAIT_LOG=/tmp/concurrency-waits.log
K6_LOG=/tmp/concurrency-k6.log

PSQL=(docker exec -i learn-go-postgres psql -U postgres -d learn_go -X)

if ! curl -s -o /dev/null --max-time 2 "$BASE_URL/"; then
  echo "ERROR: aplikasi belum berjalan di $BASE_URL (jalankan: go run main.go)"
  exit 1
fi

PRODUCT_ID=$("${PSQL[@]}" -qtAc \
  "INSERT INTO products (name, price, stock) VALUES ('concurrency-logging', 1000, $STOCK) RETURNING id;" \
  | head -1 | tr -d '[:space:]')
echo "test row: product_id=$PRODUCT_ID (stok awal $STOCK)"

cleanup() {
  # hentikan poller bila masih jalan
  if [ -n "${POLLER_PID:-}" ]; then
    kill "$POLLER_PID" 2>/dev/null || true
  fi
  "${PSQL[@]}" -c "
    DELETE FROM products_audit_logs WHERE product_id = $PRODUCT_ID;
    DELETE FROM products WHERE id = $PRODUCT_ID;
    DELETE FROM users WHERE name LIKE 'conclog_%';
  " >/dev/null 2>&1 || true
}
trap cleanup EXIT

# --- 1. Sesi penahan lock: mengunci row selama HOLD_SECONDS ---
"${PSQL[@]}" -c "
  BEGIN;
  SELECT stock FROM products WHERE id = $PRODUCT_ID FOR UPDATE;
  SELECT pg_sleep($HOLD_SECONDS);
  COMMIT;
" > /tmp/concurrency-lock-holder.log 2>&1 &
LOCK_HOLDER_PID=$!
sleep 1

# --- 2. Poller: catat sesi yang waiting setiap 0.5 detik ---
: > "$WAIT_LOG"
(
  for _ in $(seq 1 $((HOLD_SECONDS * 2 + 4))); do
    ts=$(date +%H:%M:%S)
    waiting=$("${PSQL[@]}" -qtAc \
      "SELECT count(*) FROM pg_stat_activity WHERE datname='learn_go' AND wait_event_type='Lock';" \
      | tr -d '[:space:]')
    detail=$("${PSQL[@]}" -qtAc \
      "SELECT string_agg(pid::text || ':' || left(query, 45), ' | ') FROM pg_stat_activity WHERE datname='learn_go' AND wait_event_type='Lock';")
    echo "$ts waiting=$waiting ${detail:+| $detail}" >> "$WAIT_LOG"
    sleep 0.5
  done
) &
POLLER_PID=$!

# --- 3. Jalankan k6 ---
echo "menjalankan k6 (VUS=$VUS ITERATIONS=$ITERATIONS) ..."
VUS="$VUS" ITERATIONS="$ITERATIONS" PRODUCT_ID="$PRODUCT_ID" \
  k6 run loadtest/concurrent/concurrency-logging.js 2>&1 | tee "$K6_LOG"

wait "$LOCK_HOLDER_PID" 2>/dev/null || true
wait "$POLLER_PID" 2>/dev/null || true

echo
echo "=== LOG SESI YANG MENUNGGU (pg_stat_activity) ==="
cat "$WAIT_LOG"
echo
echo "=== RINGKASAN DARI K6 ==="
grep -E "START update|DONE .*row=|MENUNGGU|teardown" "$K6_LOG" | tail -40
echo
echo "=== LOG LENGKAP ==="
echo "k6          : $K6_LOG"
echo "lock waits  : $WAIT_LOG"
