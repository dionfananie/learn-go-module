# Load test message queue

Memverifikasi dua hal saat fitur MQ/async aktif:

1. **API tetap benar & cepat** saat setiap operasi stok mem-publish event.
2. **Worker sanggup mengikuti** (queue depth kembali ~0, DLQ tetap 0).

| Yang diukur | Metrik |
|---|---|
| Latensi adjust-stock (sekarang + publish event) | `stock_adjust_duration` |
| Latensi transfer-stock (publish 2 event) | `stock_transfer_duration` |
| Konsistensi stok setelah adjust + transfer | check `stock A == 992`, `stock B == 1003` |
| Kecepatan konsumsi worker | `messages_ready` queue `stock.changed.worker` setelah run |
| Event gagal | `messages_ready` queue `stock.events.dlq` (harus 0) |

## Menjalankan

```bash
docker compose up -d rabbitmq
go run main.go

SMOKE=1 ./loadtest/message-queue/run-mq-load.sh   # cek cepat 5 detik
./loadtest/message-queue/run-mq-load.sh           # pengukuran penuh
./loadtest/reset-db.sh                            # bersihkan data test
```

## Cara membaca hasil

- `stock_adjust_duration`/`stock_transfer_duration` p95 naik sedikit dibanding
  sebelum MQ (ada overhead publish). Kalau melonjak drastis, kemungkinan broker
  lambat atau publish blocking.
- `flow_failures` harus < 1%: publish event (best-effort) tidak boleh membuat
  request utama gagal.
- `messages_ready` sesudah run harus kembali ~0 — artinya worker mengikuti laju
  publish. Kalau terus menumpuk, worker tidak sanggup (naikkan Qos / jumlah
  consumer).
- `stock.events.dlq` harus 0 — tidak ada event yang gagal diproses.
