# Challenge 11: Caching (Redis) — Evidence

Tanggal pengujian: 2026-11-11

## Ringkasan

| Aspek | Alat / Cara | Hasil |
|---|---|---|
| Cache hit end-to-end | `curl` + `redis-cli TTL` | miss **12.2 ms** → hit **1.0 ms**, TTL **300 s** |
| Hit rate saat beban baca | k6 `hot-key.js`, 50 VU / 15 s | **100.00%** (297.821 hit, 5 miss warmup) |
| Latensi baca tanpa cache | k6 `hot-key.js`, 50 VU / 15 s | p95 **74.33 ms**, 3.877 req/s |
| Latensi baca dengan cache | k6 `hot-key.js`, 50 VU / 15 s | p95 **4.67–5.71 ms**, 19.8k–23.2k req/s |
| Stampede (10 invalidasi/detik) | k6 `stampede.js`, 50 VU / 10 s | **0 error**, p95 **5.57 ms**, p99/`max` **26 ms** |
| Query DB saat stampede | `pg_stat_user_tables.idx_scan` | **43.033 → 2.664 → 376** (tanpa cache → cache → cache+singleflight) |
| Dedupe singleflight | log aplikasi | 3.805 miss, **3.678 follower**, **127 load DB** |
| Redis mati | `docker pause` | tetap **HTTP 200**; 101 ms sekali, lalu **~0.8 ms** (cooldown 5 s) |
| Timeout cache dari env | `CACHE_TIMEOUT_MS=50/100` | **56 ms / 104 ms** saat Redis hang |
| Unit test | `go test -race` | **11 test cache + 2 test service PASS**, race detector bersih |

---

## 1. Desain

Pola yang dipakai: **cache-aside** dengan Redis.

```text
GET /products/:id
  ├─ GET productId:{id}
  │    ├─ hit  → return (tanpa DB)
  │    └─ miss → singleflight:
  │              ├─ cek cache sekali lagi (double-check)
  │              ├─ SELECT ... WHERE id = $1
  │              ├─ SET productId:{id} (TTL 5 menit)
  │              └─ return
  └─ error Redis → anggap miss (fail-open), mulai cooldown 5 detik
```

| Keputusan | Nilai | Alasan |
|---|---|---|
| Key | `productId:{id}` | granular; invalidasi per produk |
| TTL | 5 menit | batas atas data basi kalau invalidasi terlewat |
| Invalidasi | `DEL` setelah tulis sukses | `DeleteProduct`, `Transaction` (per id), `TransferStock` (dua id) |
| Fail-open | error Redis = miss | Redis bukan sumber kebenaran; DB tetap melayani |
| Timeout | `CACHE_TIMEOUT_MS` (default 100 ms) | Redis lambat tidak boleh menahan request |
| Cooldown | 5 detik setelah gagal | berhenti membayar timeout di setiap request |
| Stampede | `singleflight` per key | 1 query DB untuk N request bersamaan |

---

## 2. Implementasi

### 2.1 `src/cache/cache.go` — mekanisme cache

```go
const failureCooldown = 5 * time.Second

type Client struct {
	rdb       *redis.Client
	timeout   time.Duration
	skipUntil atomic.Int64 // unix nano; > sekarang = Redis dilewati
}

func (c *Client) GetJSON(ctx context.Context, key string, dst any) bool {
	if c.skipping() {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	raw, err := c.rdb.Get(cctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			log.Printf("[cache] MISS %s", key)
			return false
		}
		if ctx.Err() != nil { // client batal ≠ Redis rusak
			return false
		}
		c.markFailure(err) // mulai/skala ulang cooldown 5 s
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return false
	}
	return true
}
```

`SetJSON` dan `Delete` juga melewati Redis saat cooldown aktif, sehingga satu kegagalan `GET` tidak diikuti `SET` yang ikut menunggu timeout.

### 2.2 `src/service/product.go` — kebijakan cache + singleflight

```go
func (s *ProductService) GetProduct(ctx context.Context, id int) (*entities.Product, error) {
	key := productCacheKey(id)

	var cached entities.Product
	if s.cache.GetJSON(ctx, key, &cached) {
		return &cached, nil
	}

	v, err, _ := s.sf.Do(key, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), productLoadTimeout)
		defer cancel()

		var fresh entities.Product
		if s.cache.GetJSON(loadCtx, key, &fresh) { // double-check
			return &fresh, nil
		}

		product, err := s.repo.GetProduct(loadCtx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrProductNotFound
		}
		if err != nil {
			return nil, err
		}

		s.cache.SetJSON(loadCtx, key, product, productCacheTTL)
		return product, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*entities.Product), nil
}
```

`context.WithoutCancel` mencegah satu client yang disconnect membatalkan query yang sedang ditunggu request lain.

### 2.3 Titik invalidasi

| Method | Kapan | Key |
|---|---|---|
| `DeleteProduct` | setelah delete sukses | `productId:{id}` |
| `Transaction` | setelah `tx.Commit()` | `productId:{id}` |
| `TransferStock` | setelah `tx.Commit()` | `productId:{from}`, `productId:{to}` |
| `Create` | — | tidak perlu: list tidak di-cache |

---

## 3. Menjalankan

```bash
# prasyarat
docker compose up -d postgres redis
go run main.go

# unit test
go test ./src/cache/ ./src/service/ -race -count=1 -v

# load test (butuh k6)
SMOKE=1 ./loadtest/cache/run-cache-load.sh
MODE=hot      VUS=50 DURATION=15s ./loadtest/cache/run-cache-load.sh
MODE=stampede VUS=50 DURATION=10s INVALIDATE_RPS=10 ./loadtest/cache/run-cache-load.sh
```

Script mencetak delta `keyspace_hits`/`keyspace_misses` Redis sebelum dan sesudah run.

---

## 4. Bukti cache hit

```text
$ docker exec learn-go-redis redis-cli DEL productId:1
$ curl -w '%{time_total}' localhost:8080/products/1
status=200 time=0.012247s
$ docker exec learn-go-redis redis-cli TTL productId:1
300
$ docker exec learn-go-redis redis-cli GET productId:1
{"id":1,"name":"Stock Flow 1791182003577","price":7174,"stock":40}

$ curl -w '%{time_total}' localhost:8080/products/1     # kedua
status=200 time=0.001042s
```

Log aplikasi:

```text
[cache] MISS productId:1
[cache] MISS productId:1     ← double-check di dalam singleflight
[cache] HIT  productId:1
```

Dua baris `MISS` untuk satu key dingin itu **normal**: pengecekan pertama di luar
`singleflight`, lalu double-check di dalam `singleflight`. Request berikutnya
langsung `HIT`.

---

## 5. Unit test

```bash
go test ./src/cache/ ./src/service/ -race -count=1 -v
```

```text
=== RUN   TestGetJSONHit
--- PASS: TestGetJSONHit (0.02s)
=== RUN   TestGetJSONMiss
--- PASS: TestGetJSONMiss (0.00s)
=== RUN   TestGetJSONInvalidJSON
--- PASS: TestGetJSONInvalidJSON (0.00s)
=== RUN   TestSetJSONMenyimpanDenganTTL
--- PASS: TestSetJSONMenyimpanDenganTTL (0.00s)
=== RUN   TestSetJSONGagalMarshal
--- PASS: TestSetJSONGagalMarshal (0.00s)
=== RUN   TestDelete
--- PASS: TestDelete (0.00s)
=== RUN   TestErrorMemicuCooldown
--- PASS: TestErrorMemicuCooldown (0.00s)
=== RUN   TestCooldownJugaMelewatiSetDanDelete
--- PASS: TestCooldownJugaMelewatiSetDanDelete (0.00s)
=== RUN   TestContextDibatalkanTidakMemicuCooldown
--- PASS: TestContextDibatalkanTidakMemicuCooldown (0.00s)
=== RUN   TestGetJSONMenghormatiTimeout
--- PASS: TestGetJSONMenghormatiTimeout (0.10s)
=== RUN   TestConcurrentAccessAman
--- PASS: TestConcurrentAccessAman (0.05s)
PASS
ok  	learn-go/src/cache	1.203s

=== RUN   TestProductCacheKey
--- PASS: TestProductCacheKey (0.00s)
=== RUN   TestProductCacheTTLWajar
--- PASS: TestProductCacheTTLWajar (0.00s)
PASS
ok  	learn-go/src/service	1.009s
```

Test memakai `miniredis` (Redis in-memory), jadi hermetic: tidak butuh container
maupun database. Yang dikunci oleh test:

- miss vs hit vs JSON rusak,
- TTL tersimpan,
- error Redis memicu cooldown; miss/cancel tidak,
- selama cooldown `GET/SET/DEL` dilewati,
- timeout per operasi dihormati (server "hang" disimulasikan TCP listener),
- akses 25 goroutine bersamaan aman di bawah `-race`.

---

## 6. Load test: hot vs stampede

Konfigurasi: 50 VU, PostgreSQL + Redis di Docker, app lokal.

| Skenario | Durasi | Reads | Throughput | p95 baca | max baca | Error |
|---|---|---|---|---|---|---|
| `hot` run 1 | 15 s | 297.821 | 19.836/s | **5.71 ms** | 41.87 ms | 0.00% |
| `hot` run 2 (ulangan) | 15 s | 348.010 | 23.181/s | **4.67 ms** | 28.79 ms | 0.00% |
| `stampede` (10 invalidasi/detik) | 10 s | 150.706 | 14.911/s | **7.63 ms** | 180.86 ms | 0.00% |
| `stampede` + singleflight | 10 s | 197.082 | 19.503/s | **5.57 ms** | **26.36 ms** | 0.00% |

Dua run `hot` menunjukkan variasi alami mesin lokal: p95 4.67–5.71 ms dan
throughput 19.8k–23.2k req/s. Pada run ulangan, build dengan singleflight
justru sedikit lebih cepat (p95 4.67 ms) daripada tanpa singleflight
(p95 5.68 ms, 19.579/s) — biaya bookkeeping singleflight tidak terukur,
sedangkan manfaatnya muncul saat key diinvalidasi (tabel bagian 7).

Invalidasi (`PATCH .../update-stock`) pada stampede + singleflight:
avg 8.32 ms, p95 **12.83 ms** — lebih cepat daripada tanpa singleflight
(p95 20.81 ms) karena DB tidak dibanjiri query baca.

Catatan: throughput absolut antar-run bervariasi ±20% (mesin lokal), jadi yang
dibandingkan adalah relasi antar-konfigurasi pada sesi yang sama.

---

## 7. Stampede: tanpa cache vs cache vs cache+singleflight

Tiga build dijalankan dengan skenario identik (`stampede.js`, 50 VU, 10 s,
10 invalidasi/detik). Query DB dihitung dari delta
`pg_stat_user_tables.idx_scan` tabel `products`.

| Konfigurasi | Reads | p95 baca | Invalidasi p95 | `idx_scan` (10 s) | Redis miss |
|---|---|---|---|---|---|
| Tanpa cache (commit `cff7688`) | 42.708 | 73.09 ms | 118.97 ms | **43.033** | — |
| Cache, tanpa singleflight (`39a859e`) | 150.706 | 7.63 ms | 20.81 ms | **2.664** | 2.461 |
| Cache + singleflight (working tree) | 197.082 | **5.57 ms** | **12.83 ms** | **376** | 3.805 |

`idx_scan` bukan murni query baca produk. Per run ada ±100 `UPDATE products`
(invalidator) dan ±100 pengecekan foreign key `products_audit_logs.product_id →
products.id`, sehingga:

```text
tanpa cache          : 42.708 baca + ~200       = 43.033   ✓
cache tanpa sf       :  2.461 load + ~200       =  2.664   ✓
cache + singleflight :    127 load + ~200       =    376   (sisa ~49: warmup/stat timing)
```

Jumlah load DB pada konfigurasi singleflight dihitung dari log:

```text
[cache] MISS            : 3805
[Singleflight] followers: 3678
                          -----
load DB (leader)        :  127
```

Interpretasi:

- Cache saja sudah memangkas akses DB **16×** (43.033 → 2.664) pada beban yang sama.
- Singleflight memangkas lagi **7×** (2.664 → 376) dan menekan `max` latensi
  dari 180.86 ms menjadi **26.36 ms**.
- Secara total, query baca produk turun dari **42.708 → 127** pada jendela 10 detik
  yang sama (±336×), meski Redis tetap mencatat ribuan `miss` — karena setiap
  request tetap bertanya ke Redis; singleflight hanya menggabungkan pengambilan
  datanya.

---

## 8. Ketahanan saat Redis bermasalah

### 8.1 Redis hang (`docker pause`)

```text
req1 status=200 time=0.101387s   ← GET timeout 100 ms
req2 status=200 time=0.000827s   ← cooldown aktif, Redis dilewati
req3 status=200 time=0.000802s

[cache] Redis failed (read tcp ...: i/o timeout), skipping cache for 5s
```

Setelah Redis di-`unpause` dan cooldown 5 detik lewat, cache pulih otomatis
tanpa restart:

```text
req4 status=200 time=0.002807s   ← miss, query DB, isi cache
req5 status=200 time=0.000761s   ← hit
TTL productId:3 = 300
```

### 8.2 Timeout dari env

`CACHE_TIMEOUT_MS` dibaca dari `.env` (fallback 100 ms). Redis di-pause agar
`GET` menggantung:

| `CACHE_TIMEOUT_MS` | Request pertama | Request berikutnya (cooldown) |
|---|---|---|
| 50 | **56.5 ms** | 0.75–0.83 ms |
| 100 (default) | **104.2 ms** | 0.80–0.89 ms |

Artinya nilai env benar-benar menjadi batas waktu operasi cache, dan cooldown
membuat request berikutnya tidak membayar timeout lagi.

---

## 9. Batasan

1. **Singleflight hanya per proses.** Dengan 3 replica, maksimal 3 query DB per
   key (satu per replica). Untuk klaster besar perlu lock Redis atau
   stale-while-revalidate.
2. **`misses` Redis bukan jumlah query DB.** Semua request yang datang saat key
   kosong mencatat `miss`; dedupe terjadi di singleflight. Metrik DB yang benar
   adalah `idx_scan`/log leader.
3. **Negative caching belum ada.** Request untuk id yang tidak ada selalu miss
   dan menembus DB; TTL pendek untuk penanda 404 layak ditambahkan.
4. **TTL tanpa jitter.** Key yang dibuat bersamaan akan expired bersamaan;
   jitter (mis. 5–6 menit) mengurangi risiko gelombang expiry.
5. **Data stok cepat berubah.** TTL 5 menit aman karena invalidasi eksplisit
   setelah `Transaction`/`TransferStock`; jika invalidasi terlewat, data bisa
   basi sampai 5 menit.

---

## Kesimpulan

- Cache-aside Redis menurunkan latensi baca dari p95 **74.33 ms → 5.71 ms**
  dan menaikkan throughput **~5×** pada 50 VU yang sama.
- Hit rate pada beban baca mencapai **100%**; TTL 300 detik dan invalidasi
  setelah operasi tulis menjaga konsistensi.
- Saat stampede (10 invalidasi/detik), **tidak ada request gagal** dan ekor
  latensi tetap rendah (p95 5.57 ms, max 26 ms).
- Singleflight menurunkan query DB dari **2.461 → 127** pada skenario yang sama,
  dan total akses DB (termasuk update + FK check) dari **43.033 → 376**.
- Redis yang mati/hang tidak membuat API gagal: request tetap 200, dengan
  penalti satu kali timeout (sesuai `CACHE_TIMEOUT_MS`) lalu cooldown 5 detik.
- Semua perilaku di atas dikunci oleh 13 unit test (11 cache + 2 service) yang
  lolos di bawah `-race`.
