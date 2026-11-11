# Load test cache

Dua skenario k6 untuk melihat dampak cache pada endpoint produk:

| File | Skenario | Yang diukur |
|---|---|---|
| `hot-key.js` | banyak VU membaca beberapa produk yang sama (cache selalu hit) | latensi baca saat cache hit |
| `stampede.js` | 1 invalidator (`PATCH .../update-stock`) tiap detik + banyak reader | lonjakan latensi/DB saat key dihapus serentak |

Keduanya dipanggil lewat `run-cache-load.sh`, yang juga mencatat
`keyspace_hits`/`keyspace_misses` Redis sebelum dan sesudah run.

## Menjalankan

```bash
# pastikan app + redis hidup
docker compose up -d redis
go run main.go

# cek cepat (5 detik, 2 VU)
SMOKE=1 ./loadtest/cache/run-cache-load.sh

# pengukuran sebenarnya
./loadtest/cache/run-cache-load.sh                 # MODE=hot
MODE=stampede ./loadtest/cache/run-cache-load.sh
MODE=both VUS=200 DURATION=1m ./loadtest/cache/run-cache-load.sh
```

Hasil juga bisa dikirim ke InfluxDB/Grafana yang sudah ada di `compose.yaml`:

```bash
K6_OUT=influxdb=http://localhost:8086/k6 ./loadtest/cache/run-cache-load.sh
```

## Cara membaca hasil

- `cache_read_duration` p95 pada `MODE=hot` idealnya < 10 ms (lokal). Kalau
  angkanya puluhan ms, kemungkinan cache sering miss/Redis lambat.
- `hit_rate` dari Redis pada `MODE=hot` seharusnya mendekati 100% setelah
  warmup. Angka rendah berarti key sering hilang/expired.
- Pada `MODE=stampede`, key sengaja dihapus tiap `INVALIDATE_RPS` detik, jadi
  yang paling penting bukan hit-rate (bisa tetap ~98% karena throughput
  reader besar), melainkan:
  - jumlah `misses` pada baris `Redis keyspace` (itu jumlah request yang
    menembus ke PostgreSQL),
  - `p(99)`/`max` `cache_read_duration` yang melonjak saat key dihapus.
- Naikkan `INVALIDATE_RPS` (mis. 10) kalau efek stampede belum terlihat.
- Bandingkan p95/p99 `hot` vs `stampede`. Selisih besar = bukti stampede.

Contoh hasil lokal (PostgreSQL + Redis di Docker, app lokal):

| Mode | VUs | Durasi | Misses | p95 baca | p99/max |
|---|---|---|---|---|---|
| `hot` | 50 | 15s | 5 | 5.9 ms | ~100 ms (outlier awal) |
| `stampede` (`INVALIDATE_RPS=10`) | 50 | 20s | 5.261 | 8.4 ms | 148 ms |

Angka absolut bergantung mesin; yang perlu dibandingkan adalah selisih
antar-mode pada mesin yang sama.


## Catatan

- Script menghapus key `productId:*` sebelum tiap mode, bukan `FLUSHALL`.
- `stampede.js` membuat user + produk baru di `setup()`; tidak menyentuh data
  produk yang sudah ada, kecuali stok produk buatannya sendiri.
- Kalau nama container Redis berbeda, set `REDIS_CONTAINER=nama-container`.
