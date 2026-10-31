#!/usr/bin/env bash
# Menjalankan k6 deadlock test sambil memantau PostgreSQL:
#   - jumlah sesi yang waiting (wait_event_type='Lock') -> circular wait
#   - kenaikan counter pg_stat_database.deadlocks -> transaksi yang dibatalkan
#
# Jalankan (aplikasi Go harus sudah berjalan di :8080):
#   ./loadtest/deadlock/run-deadlock.sh
#
# Opsi:
#   VUS=30 ITERATIONS=2000 STOCK=50000 ./loadtest/deadlock/run-deadlock.sh
set -euo pipefail

cd "$(dirname "$0")/../.."

BASE_URL="${BASE_URL:-http://localhost:8080}"
VUS="${VUS:-10}"
ITERATIONS="${ITERATIONS:-300}"
STOCK="${STOCK:-10000}"
EXPECT_DEADLOCK="${EXPECT_DEADLOCK:-0}"
WAIT_LOG=/tmp/deadlock-waits.log
K6_LOG=/tmp/deadlock-k6.log

PSQL=(docker exec -i learn-go-postgres psql -U postgres -d learn_go -X)

if ! curl -s -o /dev/null --max-time 2 "$BASE_URL/"; then
  echo "ERROR: aplikasi belum berjalan di $BASE_URL (jalankan: go run main.go)"
  exit 1
fi

deadlocks_now() {
  "${PSQL[@]}" -qtAc "SELECT deadlocks FROM pg_stat_database WHERE datname='learn_go';" | tr -d '[:space:]'
}

DEADLOCK_BEFORE=$(deadlocks_now)
echo "deadlock counter awal : $DEADLOCK_BEFORE"

: > "$WAIT_LOG"
(
  # Poller berjalan sampai dimatikan setelah k6 selesai (maks ±5 menit).
  for _ in $(seq 1 600); do
    ts=$(date +%H:%M:%S)
    waiting=$("${PSQL[@]}" -qtAc \
      "SELECT count(*) FROM pg_stat_activity WHERE datname='learn_go' AND wait_event_type='Lock';" | tr -d '[:space:]')
    dl=$(deadlocks_now)
    echo "$ts waiting=$waiting deadlocks=$dl" >> "$WAIT_LOG"
    sleep 0.5
  done
) &
POLLER_PID=$!

echo "menjalankan k6 (VUS=$VUS ITERATIONS=$ITERATIONS STOCK=$STOCK EXPECT_DEADLOCK=$EXPECT_DEADLOCK) ..."
VUS="$VUS" ITERATIONS="$ITERATIONS" STOCK="$STOCK" EXPECT_DEADLOCK="$EXPECT_DEADLOCK" \
  k6 run loadtest/deadlock/deadlock-transfer.js 2>&1 | tee "$K6_LOG"

kill "$POLLER_PID" 2>/dev/null || true
wait "$POLLER_PID" 2>/dev/null || true

DEADLOCK_AFTER=$(deadlocks_now)
echo
echo "=== PEMANTAUAN LOCK & DEADLOCK (pg_stat_activity / pg_stat_database) ==="
cat "$WAIT_LOG"
echo
echo "deadlock counter akhir  : $DEADLOCK_AFTER"
echo "deadlock baru terdeteksi: $((DEADLOCK_AFTER - DEADLOCK_BEFORE))"
echo
echo "=== RINGKASAN K6 ==="
grep -E "deadlocks|transfers_ok|insufficient|transfer_duration|http_req_failed|PASS|INKONSISTEN" "$K6_LOG" | tail -20
echo
echo "=== CONTOH LOG DEADLOCK (dari k6) ==="
grep -m 10 -E '\[VU [0-9]+\] DEADLOCK:' "$K6_LOG" || true
echo
deadlock_lines=$(grep -cE '\[VU [0-9]+\] DEADLOCK:' "$K6_LOG" || true)
echo "total baris DEADLOCK: ${deadlock_lines:-0}"
echo
echo "=== LOG LENGKAP ==="
echo "k6           : $K6_LOG"
echo "lock/deadlock: $WAIT_LOG"
