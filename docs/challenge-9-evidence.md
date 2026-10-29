# Challenge 9: Concurrency & Race Condition — Evidence

Tanggal pengujian: 2026-10-29

## Ringkasan

| Jenis race | Alat deteksi | Status |
|---|---|---|
| Data race pada kode repository | `go test -race` (integration test) | tidak ada `DATA RACE` |
| Race condition di database (lost update) | dua sesi psql + assertion | terbukti ada tanpa lock |
| Race pada flow stok aplikasi | k6 concurrency + assertion | tidak ada lost update/oversell |
| Rollback saat audit gagal | k6 + trigger | stok & audit sama-sama rollback |

---

## 1. Race detector pada integration test repository

Perintah:

```bash
CGO_ENABLED=1 \
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable' \
go test -race -count=1 -tags integration ./src/repository/ \
  -run TestUpdateProductDeltaBoundaries -v
```

Output:

```text
=== RUN   TestUpdateProductDeltaBoundaries
=== RUN   TestUpdateProductDeltaBoundaries/MaxInt32_melebihi_stok
=== RUN   TestUpdateProductDeltaBoundaries/delta_lebih_besar_dari_stok
=== RUN   TestUpdateProductDeltaBoundaries/delta_tepat_sama_dengan_stok_(stok_jadi_nol)
=== RUN   TestUpdateProductDeltaBoundaries/delta_pada_stok_nol
=== RUN   TestUpdateProductDeltaBoundaries/MinInt32_menyebabkan_overflow_int32
=== RUN   TestUpdateProductDeltaBoundaries/delta_negatif_melakukan_restock
--- PASS: TestUpdateProductDeltaBoundaries (0.06s)
    --- PASS: MaxInt32_melebihi_stok (0.01s)
    --- PASS: delta_lebih_besar_dari_stok (0.00s)
    --- PASS: delta_tepat_sama_dengan_stok_(stok_jadi_nol) (0.01s)
    --- PASS: delta_pada_stok_nol (0.00s)
    --- PASS: MinInt32_menyebabkan_overflow_int32 (0.00s)
    --- PASS: delta_negatif_melakukan_restock (0.01s)
PASS
ok  	learn-go/src/repository	1.072s
```

Interpretasi:

- Kode repository — termasuk `SELECT ... FOR UPDATE`, perhitungan stok, dan penanganan overflow — dijalankan di bawah race detector.
- Tidak ada laporan `DATA RACE`.
- `-count=1` memaksa eksekusi ulang (bukan cache), sehingga hasilnya benar-benar dari run saat ini.
- Seluruh kasus batas delta tetap PASS: MaxInt32, delta > stok, delta = stok, stok nol, MinInt32 (overflow), dan restock.

Catatan: race detector hanya mendeteksi race di **memori Go**, bukan race di database atau logical race (check-then-act). Race database dibuktikan pada bagian 2.

---

## 2. Race condition di database

Script: `loadtest/concurrent/race-condition-demo.sh`

Perintah:

```bash
./loadtest/concurrent/race-condition-demo.sh
```

### Skenario 1 — tanpa lock (read-then-write)

Dua sesi membaca stok yang sama, lalu masing-masing menulis nilai absolut hasil hitungannya sendiri:

```sql
-- sesi A dan B
BEGIN;
SELECT stock FROM race_condition_demo WHERE id = 1;   -- keduanya membaca 100
SELECT pg_sleep(1.5);
UPDATE race_condition_demo SET stock = 90 WHERE id = 1;
COMMIT;
```

Output:

```text
--- sesi A ---
 a_saw
-------
   100

--- sesi B ---
 b_saw
-------
   100

stok akhir tanpa lock : 90 (seharusnya 80)
```

Interpretasi: 100 − 10 − 10 seharusnya **80**, tetapi hasilnya **90**. Satu update hilang karena kedua sesi memakai nilai baca yang sama (stale read).

### Skenario 2 — dengan `SELECT ... FOR UPDATE`

```sql
-- sesi A
BEGIN;
SELECT stock FROM race_condition_demo WHERE id = 1 FOR UPDATE;
SELECT pg_sleep(1.5);
UPDATE race_condition_demo SET stock = stock - 10 WHERE id = 1;
COMMIT;

-- sesi B (menunggu sampai A commit)
BEGIN;
SELECT stock FROM race_condition_demo WHERE id = 1 FOR UPDATE;
UPDATE race_condition_demo SET stock = stock - 10 WHERE id = 1;
COMMIT;
```

Output:

```text
--- saat A memegang lock ---
 pid  | wait_event_type
------+-----------------
 1478 | Lock

--- sesi A ---
 a_saw
-------
   100

--- sesi B ---
 b_saw
-------
    90

stok akhir FOR UPDATE : 80 (seharusnya 80)
```

Interpretasi: sesi B **menunggu** (`wait_event_type = Lock`), lalu membaca **90** (hasil commit A), bukan 100. Stok akhir **80** — benar.

### Hasil script

```text
PASS:
  - tanpa lock  : race tereproduksi (stok 90, seharusnya 80)
  - FOR UPDATE  : benar (stok 80)
  - sesi kedua menunggu lock, lalu membaca nilai hasil commit sesi pertama
```

---

## 3. Flow stok aplikasi (k6)

Perintah:

```bash
k6 run loadtest/concurrent/for-update-concurrency.js
```

Hasil:

```text
10 VU, 300 request, stok awal 200
200 update sukses, 100 ditolak (stok habis)
stok akhir : 0
audit log  : 200
konsisten  : true
rantai audit ok : true
http_req_failed : 0.00%
PASS: tidak ada lost update / oversell
```

Assertion yang dipakai (`teardown`):

```js
finalStock === data.stock - auditCount   // tidak ada lost update
finalStock >= 0                          // tidak ada stok negatif
chainOk                                  // old_stock -> new_stock tersambung
successful_updates <= TARGET_STOCK       // tidak oversell
```

---

## 4. Rollback saat audit gagal

Perintah:

```bash
./loadtest/concurrent/run-rollback-audit-error.sh
```

Hasil:

```text
stok awal       : 100
stok akhir      : 100
audit log count : 0
intentional_audit_failures: 5
checks_succeeded: 100%
PASS: UPDATE stok dan audit sama-sama rollback
```

Trigger sengaja menggagalkan `INSERT` audit; `UPDATE` stok ikut dibatalkan oleh rollback transaksi.

---

## 5. Negative control

Agar test bermakna, test harus bisa **GAGAL** saat bug ada. Skenario 1 di atas adalah negative control untuk `FOR UPDATE`: tanpa lock, test mendeteksi lost update (90 ≠ 80). Dengan lock, hasilnya benar (80). Ini membuktikan test tidak sekadar selalu hijau.

---

## Kesimpulan

- Kode repository dijalankan di bawah `-race`: **tidak ada data race** pada jalur yang diuji (FOR UPDATE, perhitungan stok, overflow).
- Race condition **terbukti ada** di level database: tanpa lock, stok akhir 90 (lost update) padahal seharusnya 80.
- Perbaikan **terbukti bekerja**: `SELECT ... FOR UPDATE` membuat sesi kedua menunggu, lalu membaca nilai terbaru; stok akhir 80.
- Flow aplikasi **tidak menunjukkan** lost update/oversell berdasarkan k6 + assert invariant.
- Lock hanya memblokir penulis pada row yang sama; pembaca biasa dan row lain tidak terpengaruh (MVCC).
