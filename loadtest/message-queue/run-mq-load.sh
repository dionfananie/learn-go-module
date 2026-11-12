#!/usr/bin/env bash
# Load test message queue: memastikan publish event tidak merusak API,
# dan worker async sanggup mengonsumsi semua event (queue depth kembali ~0).
#
# Pemakaian:
#   ./loadtest/message-queue/run-mq-load.sh
#   SMOKE=1 ./loadtest/message-queue/run-mq-load.sh
#   K6_OUT=influxdb=http://localhost:8086/k6 ./loadtest/message-queue/run-mq-load.sh
set -euo pipefail

cd "$(dirname "$0")/../.."

BASE_URL="${BASE_URL:-http://localhost:8080}"
RABBITMQ_CONTAINER="${RABBITMQ_CONTAINER:-learn-go-rabbitmq}"
K6_OUT="${K6_OUT:-}"

if ! command -v k6 >/dev/null 2>&1; then
  echo "ERROR: k6 tidak ditemukan di PATH"; exit 1
fi
if ! curl -s -o /dev/null --max-time 2 "$BASE_URL/"; then
  echo "ERROR: aplikasi belum berjalan di $BASE_URL (go run main.go)"; exit 1
fi
if ! docker exec "$RABBITMQ_CONTAINER" rabbitmqctl status >/dev/null 2>&1; then
  echo "ERROR: RabbitMQ '$RABBITMQ_CONTAINER' tidak berjalan (docker compose up -d rabbitmq)"; exit 1
fi

# Baris queue: "nama  messages_ready  messages_unacknowledged"
queue_line() {
  docker exec "$RABBITMQ_CONTAINER" \
    rabbitmqctl list_queues name messages_ready messages_unacknowledged 2>/dev/null \
    | grep -F "$1" || echo "$1 <belum ada>"
}

echo "=== Sebelum ==="
echo "worker : $(queue_line stock.changed.worker)"
echo "dlq    : $(queue_line stock.events.dlq)"

args=(-e BASE_URL="$BASE_URL")
[[ -n "${SMOKE:-}" ]] && args+=(-e SMOKE=1)
[[ -n "$K6_OUT" ]] && args+=(--out "$K6_OUT")

k6 run "${args[@]}" loadtest/message-queue/stock-event-flow.js

# Beri jeda agar worker menghabiskan sisa pesan.
sleep 3

echo
echo "=== Sesudah ==="
echo "worker : $(queue_line stock.changed.worker)   # messages_ready harus kembali ~0"
echo "dlq    : $(queue_line stock.events.dlq)      # harus tetap 0 (tidak ada event gagal)"
