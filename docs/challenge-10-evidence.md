# Challenge 10: Deadlock — Evidence

Tanggal pengujian: 2026-10-31

## Ringkasan

| Aspek | Alat / Cara | Hasil |
|---|---|---|
| Deadlock dua transaksi berlawanan arah (kode lama) | k6 + endpoint `transfer-stock` | **276 deadlock** terdeteksi (HTTP 409) |
| Korban deadlock di-rollback penuh | teardown k6 (stok + audit) | total stok **20000 → 20000**, audit **rekonsiliasi** |
| Sesi saling menunggu lock | `pg_stat_activity` | **waiting = 10** (semua VU) |
| Transaksi dibatalkan PostgreSQL | `pg_stat_database.deadlocks` | **+276** selama run |
| Request gagal tak terduga | `http_req_failed` | **0.00%** |
| Iterasi selesai | k6 | **300/300**, 0 dropped |
| **Setelah fix (urutan lock konsisten)** | k6, beban sama | **0 deadlock**, **300/300 sukses**, avg **70 ms** |
| **Regression test fix** | `go test -tags integration` | **PASS**, `deadlock=0` |

---

## 1. Skenario

Endpoint baru: `POST /products/transfer-stock`

```json
{
  "from_id": 10019,
  "to_id": 10020,
  "amount": 1
}
```

Di dalam **satu transaksi**, service memindahkan stok dari source ke destination
dan mengunci kedua baris dengan urutan **tetap**: source dulu, lalu destination.

```sql
BEGIN;
SELECT stock FROM products WHERE id = $source      FOR UPDATE;  -- lock #1
SELECT stock FROM products WHERE id = $destination FOR UPDATE;  -- lock #2
UPDATE products SET stock = $1 WHERE id = $source;
UPDATE products SET stock = $2 WHERE id = $destination;
INSERT INTO products_audit_logs (... 'transfer_out', old, new);
INSERT INTO products_audit_logs (... 'transfer_in',  old, new);
COMMIT;
```

Dua transfer **berlawanan arah** yang berjalan bersamaan akan mengunci dalam
urutan terbalik:

```text
tx1 (A -> B):  lock A  ──menunggu──>  B
tx2 (B -> A):  lock B  ──menunggu──>  A
                      circular wait
```

PostgreSQL mendeteksi circular wait setelah `deadlock_timeout` (default **±1 detik**),
membatalkan salah satu transaksi dengan **SQLSTATE `40P01` (`deadlock_detected`)**,
lalu transaksi yang menang melanjutkan dan commit.

---

## 2. Pemetaan error deadlock → HTTP

| Layer | Perilaku |
|---|---|
| Repository (`TransferStock`) | meneruskan error mentah dari driver pgx |
| Service | `isDeadlock(err)` mendeteksi `pgconn.PgError.Code == "40P01"` → `ErrDeadlock` |
| Handler | `ErrDeadlock` → **409 Conflict** `{"error":"deadlock detected, please retry"}` |

Deteksi deadlock:

```go
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}
```

Catatan desain: handler transfer memakai timeout **30 detik** (bukan 5 detik
seperti endpoint lain). Dengan 5 detik, transaksi yang sehat tetapi mengantre
lock panjang ikut dipotong menjadi 500 sebelum sempat terdeteksi sebagai
deadlock. Timeout 30 detik memberi ruang bagi `deadlock_timeout` PostgreSQL
(±1 detik) untuk bekerja sehingga hasilnya jelas: **200 (sukses) atau 409 (deadlock)**.

---

## 3. Menjalankan

```bash
# terminal 1
go run main.go

# terminal 2 — full run (10 VU, 300 iterasi) + telemetri PostgreSQL
./loadtest/deadlock/run-deadlock.sh

# atau langsung k6
k6 run loadtest/deadlock/deadlock-transfer.js

# smoke cepat (2 VU, 40 iterasi; belum tentu menghasilkan deadlock)
SMOKE=1 k6 run loadtest/deadlock/deadlock-transfer.js
```

Default k6: `VUS=10`, `ITERATIONS=300`, `STOCK=10000`, `AMOUNT=1`.
Setengah VU melakukan `A -> B`, setengahnya `B -> A` (`__VU % 2`) sehingga
selalu ada dua aliran berlawanan arah.

---

## 4. Hasil k6

Perintah:

```bash
./loadtest/deadlock/run-deadlock.sh
```

Ringkasan:

```text
checks_total.......: 307     2.806647/s
checks_succeeded...: 100.00% 307 out of 307
checks_failed......: 0.00%   0 out of 307

deadlocks......................: 276    2.52324/s
transfer_duration..............: avg=3.62s min=9.19ms med=3.01s max=14.02s p(90)=6.99s p(95)=8s
transfers_ok...................: 24     0.219412/s
http_req_failed................: 0.00%  0 out of 308
http_reqs......................: 308    2.815789/s

iterations.....................: 300    2.742652/s   (0 dropped)
```

Interpretasi:

- **276 dari 300** iterasi berakhir sebagai korban deadlock (HTTP 409).
- **24** transfer berhasil commit (HTTP 200).
- **0** request gagal karena sebab lain (tidak ada 500 / timeout).
- Durasi transfer panjang (median 3 detik, maks 14 detik) karena setiap
  deadlock menunggu `deadlock_timeout` PostgreSQL lalu antre lagi — ini justru
  bukti adanya lock contention.
- Threshold `deadlocks: count>0` pada full run memastikan skenario benar-benar
  menghasilkan deadlock; `SMOKE=1` tidak memakai threshold ini (concurrency
  rendah bisa 0 deadlock).

---

## 5. Log setiap deadlock (dari k6)

Setiap balasan **409** dicatat oleh k6 (`console.log`) lengkap dengan VU, arah
transfer, durasi, dan pesan error API:

```text
[VU 9]  DEADLOCK: transfer 10020 -> 10019 amount=1 status=409 durasi=1009.099662ms | {"error":"deadlock detected, please retry"}
[VU 7]  DEADLOCK: transfer 10020 -> 10019 amount=1 status=409 durasi=2014.630809ms | {"error":"deadlock detected, please retry"}
[VU 10] DEADLOCK: transfer 10019 -> 10020 amount=1 status=409 durasi=1002.554305ms | {"error":"deadlock detected, please retry"}
[VU 3]  DEADLOCK: transfer 10020 -> 10019 amount=1 status=409 durasi=4019.152332ms | {"error":"deadlock detected, please retry"}
[VU 4]  DEADLOCK: transfer 10019 -> 10020 amount=1 status=409 durasi=6018.876683ms | {"error":"deadlock detected, please retry"}
```

Perhatikan dua arah (`A -> B` dan `B -> A`) sama-sama muncul — bukti kedua
transaksi saling mengunci. Total baris `DEADLOCK:` = jumlah counter `deadlocks`.

---

## 6. Verifikasi konsistensi (teardown)

Korban deadlock harus **rollback penuh**: stok tidak boleh berubah dan audit
tidak boleh tercatat. Teardown memeriksa dua invariant:

1. **Total stok kekal** — transfer hanya memindahkan, tidak menciptakan/menghilangkan stok.
2. **Audit trail rekonsiliasi** — untuk tiap produk, `final - initial` harus sama
   dengan `Σ(new_stock - old_stock)` dari audit log.

Output:

```text
=== VERIFIKASI DEADLOCK TRANSFER ===
produk A (id 10019): stok 10000 -> 10002 (delta 2)
produk B (id 10020): stok 10000 -> 9998 (delta -2)
total stok  : awal 20000 / akhir 20000
audit A     : 24 log, delta 2
audit B     : 24 log, delta -2
=== PASS: total stok konsisten & audit trail rekonsiliasi ===
```

Interpretasi:

- Stok A naik **+2**, stok B turun **−2**, total tetap **20000** — tidak ada
  stok yang hilang/tercipta meskipun 276 transaksi menjadi korban deadlock.
- Ada **24 transfer sukses** → masing-masing menulis **2 audit log**
  (`transfer_out` + `transfer_in`) = **48 baris**, terbagi 24 per produk.
- `Σdelta audit` per produk **sama persis** dengan perubahan stok fisik
  (A: +2, B: −2) → transaksi yang di-rollback **tidak meninggalkan audit**.

---

## 7. Telemetri PostgreSQL

Runner memantau `pg_stat_activity` dan `pg_stat_database` selama k6 berjalan.

Sesi yang menunggu lock (`wait_event_type='Lock'`):

```text
10:02:57 waiting=0  deadlocks=1479
10:02:57 waiting=10 deadlocks=1479
10:02:58 waiting=10 deadlocks=1480
10:02:59 waiting=10 deadlocks=1483
10:03:00 waiting=10 deadlocks=1484
...
10:03:33 waiting=10 deadlocks=1571
```

Counter deadlock dari `pg_stat_database`:

```text
deadlock counter awal  : 1479
deadlock counter akhir : 1755
deadlock baru terdeteksi: 276
```

Interpretasi:

- **Semua 10 VU** teramati `waiting` pada lock dalam waktu bersamaan →
  benar-benar terjadi antrean/circular wait, bukan sekadar error aplikasi.
- Counter `deadlocks` PostgreSQL bertambah **276**, **sama persis** dengan
  jumlah respons 409 yang dicatat k6 → membuktikan 409 berasal dari deadlock
  database, bukan dari error lain.

---

## 8. Interpretasi & pencegahan

Deadlock bukan bug aplikasi semata, melainkan kondisi yang wajar saat beberapa
transaksi mengunci banyak baris dengan urutan berbeda. Yang penting adalah
perilakunya **terdefinisi**: PostgreSQL mendeteksi, membatalkan satu transaksi,
dan aplikasi memetakannya ke respons yang bisa di-retry.

Cara mencegah/mengurangi deadlock:

1. **Urutan lock konsisten** — selalu kunci baris dengan `id` lebih kecil dulu,
   misalnya `SELECT ... WHERE id IN ($1, $2) ORDER BY id FOR UPDATE`. Ini
   menghilangkan circular wait paling murah.
2. **Retry on `40P01`** — korban deadlock aman di-retry (transaksi sudah
   rollback), dengan backoff + jitter.
3. **Perpendek transaksi** — jangan menahan lock saat melakukan I/O lambat.
4. **Turunkan `deadlock_timeout`** bila ingin deteksi lebih cepat (mis. 200ms),
   dengan konsekuensi biaya deteksi lebih sering.

---

## 9. Perbaikan: urutan lock konsisten (deadlock → 0)

### Akar masalah

`TransferStock` mengunci baris mengikuti **arah transfer** (source dulu):

```go
// SEBELUM: source dulu, destination belakangan
SELECT stock FROM products WHERE id = $from FOR UPDATE;  // lock #1
SELECT stock FROM products WHERE id = $to   FOR UPDATE;  // lock #2
```

Dua transfer berlawanan arah jadi mengunci dengan urutan terbalik → circular wait.

### Perbaikan

Kedua baris dikunci dengan **urutan deterministik berdasarkan id** (id terkecil
dulu), apa pun arah transfernya:

```go
// SESUDAH: id terkecil selalu dikunci lebih dulu
firstId, secondId := fromId, toId
if firstId > secondId {
    firstId, secondId = secondId, firstId
}
// lock firstId, lalu lock secondId  (SELECT ... FOR UPDATE)
```

Dengan begitu `A -> B` dan `B -> A` sama-sama mengunci `min(A,B)` lebih dulu.
Transaksi kedua hanya **menunggu** transaksi pertama commit — tidak ada lagi
saling menunggu, sehingga deadlock tidak mungkin terbentuk.

### Hasil sebelum vs sesudah

Perintah (aplikasi fixed):

```bash
go run main.go
k6 run loadtest/deadlock/deadlock-transfer.js
```

| Metrik (10 VU, 300 iterasi) | Sebelum fix | Sesudah fix |
|---|---|---|
| `deadlocks` (HTTP 409) | **274** | **0** |
| `transfers_ok` (HTTP 200) | 24 | **300 / 300** |
| `http_req_failed` | 0.00% | 0.00% |
| `transfer_duration` avg | 3.62 s | **70.56 ms** |
| `transfer_duration` p(95) | 8 s | **214.73 ms** |
| Konsistensi stok + audit | PASS | PASS |

Output k6 sesudah fix:

```text
checks_succeeded...: 100.00% 307 out of 307
deadlocks......................: 0      0/s
transfer_duration..............: avg=70.56ms min=4.49ms med=45.01ms max=338.47ms p(90)=170.86ms p(95)=214.73ms
transfers_ok...................: 300    129.881655/s
http_req_failed................: 0.00%  0 out of 308
iterations.....................: 300    129.881655/s

=== VERIFIKASI DEADLOCK TRANSFER ===
produk A (id 10027): stok 10000 -> 9990 (delta -10)
produk B (id 10028): stok 10000 -> 10010 (delta 10)
total stok  : awal 20000 / akhir 20000
audit A     : 300 log, delta -10
audit B     : 300 log, delta 10
=== PASS: total stok konsisten & audit trail rekonsiliasi ===
```

Interpretasi:

- **0 deadlock** pada beban yang sama yang sebelumnya menghasilkan 274 deadlock.
- Semua **300** transfer sukses; tidak ada lagi transaksi yang dibatalkan.
- Latensi turun drastis (avg 3.62 s → **70 ms**, p95 8 s → **215 ms**) karena
  tidak ada lagi `deadlock_timeout` ±1 detik per konflik.
- Invariant tetap terjaga: total stok 20000, audit 300 log/produk, rekonsiliasi.

### Regression test (Go)

Ditambahkan `src/repository/product_deadlock_integration_test.go`: 20 goroutine
melakukan 300 transfer dua arah bersamaan, lalu memastikan **0 error `40P01`**
dan total stok kekal.

```bash
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable' \
  go test -tags integration ./src/repository/ -run TestTransferStockNoDeadlock -v -count=1
```

```text
=== RUN   TestTransferStockNoDeadlock
    product_deadlock_integration_test.go:90: transfer sukses=300 deadlock=0 error lain=0
--- PASS: TestTransferStockNoDeadlock (0.91s)
PASS
ok  	learn-go/src/repository	0.919s
```

Catatan: test k6 tetap menyediakan mode `EXPECT_DEADLOCK=1` untuk membuktikan
skenario bug lama masih bisa mereproduksi deadlock (negative control). Secara
default, test **mewajibkan** `deadlocks == 0` setelah perbaikan.

---

## Kesimpulan

- Skenario deadlock **terbukti tereproduksi** pada kode lama: 276 deadlock
  (SQLSTATE `40P01`) dari 300 iterasi transfer berlawanan arah, terlihat juga di
  `pg_stat_database` dan `pg_stat_activity` (`waiting=10`).
- Endpoint memetakan deadlock ke **409 Conflict** yang jelas dan bisa di-retry,
  bukan 500 generik.
- Korban deadlock **di-rollback penuh**: total stok tetap 20000 dan audit trail
  rekonsiliasi dengan perubahan stok — **tidak ada lost update**.
- Setiap deadlock **tercatat di log k6** (`[VU n] DEADLOCK: ...`) sehingga mudah
  diaudit dan dihitung.
- **Perbaikan berhasil**: dengan urutan lock konsisten (id terkecil dulu),
  deadlock turun dari **274 → 0** pada beban yang sama, semua 300 transfer sukses,
  dan latensi turun dari avg 3.62 s → 70 ms. Regression test Go juga PASS.
