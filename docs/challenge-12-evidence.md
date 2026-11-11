# Challenge 12: Cache Invalidation Race Condition — Evidence

Tanggal pengujian: 2026-11-11

## Ringkasan

| Aspek | Alat / Cara | Hasil |
|---|---|---|
| Identifikasi race | analisis urutan reader vs writer | reader bisa menulis nilai basi **setelah** writer `DEL` |
| Solusi | double-delete (`invalidateCache`) | hapus segera + hapus kedua setelah 200 ms |
| Titik penerapan | service | `DeleteProduct`, `Transaction`, `TransferStock` |
| Unit test mekanisme | `go test -race ./src/service` | **4/4 PASS**, termasuk simulasi stale-write deterministik |
| Integration test end-to-end | `go test -tags integration`, 8 reader + 4 writer | **PASS** (1.08 s), konsisten DB = Redis, `-race` juga PASS |
| Regresi yang dicegah | stale cache sampai TTL (5 menit) | stok Redis selalu sama dengan DB setelah burst |

---

## 1. Masalah

Cache invalidation bukan sekadar "hapus setelah update". Ada jendela kecil
antara pembacaan DB oleh reader dan penulisan cache, yang bisa disusupi writer.

```text
Reader A                         Writer B
────────                         ────────
GET cache → miss
SELECT stok = 5
                                 BEGIN
                                 UPDATE stok = 6
                                 COMMIT
                                 DEL productId:1
SET productId:1 = {stok:5}  ←   ditulis SETELAH delete
                                 (delete B hilang, cache basi 5 menit)
```

Akibatnya pembaca berikutnya mendapat **stok 5** padahal DB sudah **stok 6**,
sampai TTL 5 menit habis.

### Kenapa `singleflight` tidak menutupnya

`singleflight` hanya menggabungkan **reader dengan reader** yang bersamaan.
Race di atas melibatkan **reader vs writer** — dua alur yang tidak berbagi
satu panggilan, jadi singleflight tidak relevan di sini.

---

## 2. Solusi: double-delete

`src/service/product.go`:

```go
const cacheInvalidateDelay = 200 * time.Millisecond

func (s *ProductService) invalidateCache(ctx context.Context, keys ...string) {
	if len(keys) == 0 {
		return
	}
	s.cache.Delete(ctx, keys...)

	clone := make([]string, len(keys))
	copy(clone, keys)

	// time.AfterFunc lebih ringan daripada go func + time.Sleep (tidak
	// meninggalkan goroutine tidur yang menumpuk).
	time.AfterFunc(cacheInvalidateDelay, func() {
		// jangan pakai ctx request: client bisa saja sudah disconnect.
		s.cache.Delete(context.Background(), clone...)
	})
}
```

Cara kerjanya:

```text
writer COMMIT
   ├─ DEL (segera)          → hapus nilai lama
   └─ ... 200 ms ...
        DEL (kedua)         → hapus nilai basi yang ditulis reader di sela-sela
```

- **Delete pertama** menghentikan pembaca yang belum sempat membaca DB.
- **Delete kedua** menangkap reader yang sudah membaca nilai lama sebelum
  commit dan menulisnya kembali setelah delete pertama.
- `clone` memastikan slice pemanggil yang di-mutasi tidak mengubah target
  delete kedua.

Titik penerapan (setelah operasi tulis sukses):

| Method | Keys |
|---|---|
| `DeleteProduct` | `productId:{id}` |
| `Transaction` | `productId:{id}` |
| `TransferStock` | `productId:{from}`, `productId:{to}` |
| `Create` | tidak perlu (id baru) |

---

## 3. Unit test

File: `src/service/product_invalidate_cache_test.go`

```bash
go test ./src/service/ -race -count=1 -run TestInvalidateCache -v
```

```text
=== RUN   TestInvalidateCacheDeletesImmediately
--- PASS: TestInvalidateCacheDeletesImmediately (0.01s)
=== RUN   TestInvalidateCacheDeletesAgainAfterStaleWrite
--- PASS: TestInvalidateCacheDeletesAgainAfterStaleWrite (0.21s)
=== RUN   TestInvalidateCacheCopiesKeys
--- PASS: TestInvalidateCacheCopiesKeys (0.00s)
=== RUN   TestInvalidateCacheNoKeys
--- PASS: TestInvalidateCacheNoKeys (0.00s)
PASS
ok  	learn-go/src/service	1.241s
```

Yang dikunci oleh tiap test:

| Test | Membuktikan |
|---|---|
| `...DeletesImmediately` | delete pertama sinkron |
| `...DeletesAgainAfterStaleWrite` | **deterministik**: nilai basi ditulis ulang setelah delete → delete kedua menghapusnya |
| `...CopiesKeys` | mutasi slice pemanggil tidak mengubah target delete kedua |
| `...NoKeys` | tidak panic saat tanpa key |

Test memakai `miniredis` (Redis in-memory), hermetic.

---

## 4. Integration test

File: `src/service/product_cache_invalidation_integration_test.go`
(build tag `//go:build integration`)

```bash
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable' \
TEST_REDIS_ADDR='localhost:6379' \
  go test -tags integration ./src/service/ -run TestCacheInvalidationRace -v -count=1
```

```text
=== RUN   TestCacheInvalidationRace
--- PASS: TestCacheInvalidationRace (1.08s)
PASS
ok  	learn-go/src/service	1.090s
```

Dengan race detector:

```bash
... -race ...
```

```text
ok  	learn-go/src/service	2.193s
```

Skenario test:

- Siapkan user + produk stok **1.000.000**, warm-up cache.
- Burst: **8 reader × 200 `GetProduct`** bersamaan dengan
  **4 writer × 50 `Transaction`** (delta 1, masing-masing commit lalu invalidate).
- Tunggu delete kedua selesai (`cacheInvalidateDelay` + 300 ms).

Assertion:

1. **Konsistensi DB** — stok akhir harus tepat `1.000.000 − 200 = 999.800`.
2. **Anti-stale (inti)** — kalau key cache masih ada, stok di dalamnya **harus
   sama** dengan stok DB. Kalau double-delete gagal, test gagal dengan pesan:
   `STALE CACHE: cache stock=..., DB stock=...`.
3. **Baca normal** — `GetProduct` setelah settle mengembalikan nilai yang sama
   dengan DB.

---

## 5. Verifikasi manual (opsional)

Melihat double-delete langsung dari sisi Redis:

```bash
# terminal 1
docker exec -it learn-go-redis redis-cli MONITOR

# terminal 2
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable' \
  go test -tags integration ./src/service/ -run TestCacheInvalidationRace -count=1
```

`MONITOR` akan menampilkan pola `DEL productId:{id}` dua kali dalam jarak
±200 ms pada setiap `Transaction`, serta `GET`/`SET` dari reader. Kalau ada
`SET` (stok lama) yang muncul di antara dua `DEL`, delete kedua yang
membersihkannya — itulah bukti race ditutup.

---

## 6. Batasan

1. **Masih ada jendela 200 ms** di mana reader bisa melihat nilai lama. Ini
   trade-off dari double-delete; untuk nol-stale butuh version guard (kolom
   `version` + compare-and-set), yang belum diterapkan di project ini.
2. **Restart proses** sebelum timer `AfterFunc` jalan → delete kedua hilang,
   mengandalkan TTL sebagai batas atas staleness.
3. **Write sangat tinggi** → timer menumpuk. Untuk ribuan write/detik, ganti
   dengan debounce per key (map key → timer) atau queue terpusat.
4. Integration test adalah **regression guard probabilistik**: tidak pernah
   false-fail saat fix benar, dan peluang menangkap bug naik seiring banyaknya
   goroutine/iterasi. Reproduksi deterministik race-nya sudah di-cover unit
   test `...DeletesAgainAfterStaleWrite`.

---

## Kesimpulan

- Race "reader menulis nilai basi setelah writer delete" **teridentifikasi**
  dan tidak bisa diatasi singleflight (yang hanya mendedupe reader).
- Double-delete (`DEL` segera + `DEL` tertunda 200 ms dengan key yang di-clone)
  **menutup jendela race** pada ketiga jalur tulis.
- **4 unit test** (termasuk simulasi stale-write deterministik) dan
  **1 integration test** (burst reader + writer dengan PostgreSQL + Redis
  nyata) semuanya **PASS**, termasuk di bawah `-race`.
- Invariant yang dijaga: **stok di Redis selalu sama dengan stok di PostgreSQL**
  setelah periode invalidasi selesai.
