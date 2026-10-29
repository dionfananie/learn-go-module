#!/usr/bin/env bash
# Demo/test race condition pada level database.
# Skenario 1: TANPA lock (read-then-write absolut) -> lost update.
# Skenario 2: DENGAN SELECT ... FOR UPDATE -> hasil benar.
#
# Jalankan:
#   ./loadtest/concurrent/race-condition-demo.sh
set -euo pipefail

cd "$(dirname "$0")/../.."

TABLE=race_condition_demo
PSQL=(docker exec -i learn-go-postgres psql -U postgres -d learn_go -X)

cleanup() {
  "${PSQL[@]}" -c "DROP TABLE IF EXISTS $TABLE;" >/dev/null 2>&1 || true
}
trap cleanup EXIT

reset() {
  "${PSQL[@]}" -c "
    DROP TABLE IF EXISTS $TABLE;
    CREATE TABLE $TABLE (id int PRIMARY KEY, stock int NOT NULL);
    INSERT INTO $TABLE VALUES (1, 100);
  " >/dev/null
}

final_stock() {
  "${PSQL[@]}" -qtAc "SELECT stock FROM $TABLE WHERE id = 1;" | head -1 | tr -d '[:space:]'
}

echo "=== SKENARIO 1: TANPA LOCK (read-then-write) ==="
reset
# Kedua sesi membaca stok yang sama (100), lalu masing-masing menulis
# nilai absolut hasil hitungannya sendiri (100 - 10 = 90).
"${PSQL[@]}" -c "BEGIN; SELECT stock AS a_saw FROM $TABLE WHERE id=1; SELECT pg_sleep(1.5); UPDATE $TABLE SET stock = 90 WHERE id=1; COMMIT;" > /tmp/race-unsafe-a.log 2>&1 &
"${PSQL[@]}" -c "BEGIN; SELECT stock AS b_saw FROM $TABLE WHERE id=1; SELECT pg_sleep(1.5); UPDATE $TABLE SET stock = 90 WHERE id=1; COMMIT;" > /tmp/race-unsafe-b.log 2>&1 &
wait
echo "--- sesi A ---"; cat /tmp/race-unsafe-a.log
echo "--- sesi B ---"; cat /tmp/race-unsafe-b.log
UNSAFE_FINAL=$(final_stock)
echo "stok akhir tanpa lock : $UNSAFE_FINAL (seharusnya 80)"
echo

echo "=== SKENARIO 2: DENGAN SELECT ... FOR UPDATE ==="
reset
"${PSQL[@]}" -c "BEGIN; SELECT stock AS a_saw FROM $TABLE WHERE id=1 FOR UPDATE; SELECT pg_sleep(1.5); UPDATE $TABLE SET stock = stock - 10 WHERE id=1; COMMIT;" > /tmp/race-safe-a.log 2>&1 &
"${PSQL[@]}" -c "BEGIN; SELECT stock AS b_saw FROM $TABLE WHERE id=1 FOR UPDATE; UPDATE $TABLE SET stock = stock - 10 WHERE id=1; COMMIT;" > /tmp/race-safe-b.log 2>&1 &
sleep 0.5
echo "--- saat A memegang lock ---"
"${PSQL[@]}" -c "SELECT pid, wait_event_type FROM pg_stat_activity WHERE datname='learn_go' AND wait_event_type='Lock';"
wait
echo "--- sesi A ---"; cat /tmp/race-safe-a.log
echo "--- sesi B ---"; cat /tmp/race-safe-b.log
SAFE_FINAL=$(final_stock)
echo "stok akhir FOR UPDATE : $SAFE_FINAL (seharusnya 80)"
echo

if [ "$UNSAFE_FINAL" -eq 80 ]; then
  echo "GAGAL: race tidak tereproduksi pada skenario tanpa lock"
  exit 1
fi
if [ "$SAFE_FINAL" -ne 80 ]; then
  echo "GAGAL: FOR UPDATE tidak menghasilkan stok yang benar"
  exit 1
fi

echo "PASS:"
echo "  - tanpa lock  : race tereproduksi (stok $UNSAFE_FINAL, seharusnya 80)"
echo "  - FOR UPDATE  : benar (stok $SAFE_FINAL)"
echo "  - sesi kedua menunggu lock, lalu membaca nilai hasil commit sesi pertama"
