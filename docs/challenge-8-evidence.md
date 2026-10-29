# Challenge 8: Database Evidence

Tanggal pengujian: 2026-10-29

## 1. Verifikasi migration dan composite index

Migration dijalankan dengan:

```bash
migrate -path db/migrations \
  -database 'postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable' version
```

Output:

```text
5
```

Index diverifikasi langsung dari PostgreSQL, bukan hanya dari file migration:

```sql
SELECT schemaname, tablename, indexname, indexdef
FROM pg_indexes
WHERE tablename = 'products_audit_logs'
ORDER BY indexname;
```

Output penting:

```text
public | products_audit_logs | idx_audit_logs_product_created |
CREATE INDEX idx_audit_logs_product_created
ON public.products_audit_logs USING btree (product_id, created_at)
```

## 2. EXPLAIN ANALYZE BUFFERS

Query yang dianalisis:

```sql
ANALYZE products_audit_logs;

EXPLAIN (ANALYZE, BUFFERS)
SELECT id, product_id, user_id, action, old_stock, new_stock, created_at
FROM products_audit_logs
WHERE product_id = 9980
ORDER BY created_at DESC
LIMIT 20;
```

Setelah data audit ditambah hingga total 3.000 rows, hasil pada database:

```text
Limit  (cost=0.41..59.67 rows=20 width=45)
  (actual time=0.008..0.014 rows=20 loops=1)
  Buffers: shared hit=23
  -> Index Scan Backward using idx_audit_logs_product_created
       on products_audit_logs
       (cost=0.41..593.05 rows=200 width=45)
       (actual time=0.007..0.012 rows=20 loops=1)
       Index Cond: (product_id = 9980)
       Buffers: shared hit=23
  Planning:
    Buffers: shared hit=45
  Planning Time: 0.180 ms
  Execution Time: 0.026 ms
```

Interpretasi: setelah tabel berisi 3.000 rows, query audit untuk `product_id = 9980` menggunakan `Index Scan Backward` pada composite index. Index memenuhi kondisi `product_id` sekaligus menyediakan urutan `created_at DESC`, sehingga tidak ada node `Sort`. `LIMIT 20` membuat PostgreSQL dapat berhenti setelah menemukan 20 rows pertama.

## 3. Audit batas delta

`AdjustStockRequest.Delta` bertipe `int32`. JSON di luar rentang int32 ditolak oleh binding handler. Untuk mencegah overflow pada restock dengan nilai negatif ekstrem, perhitungan repository menggunakan `int64` terlebih dahulu:

```go
newStock64 := int64(stock) - int64(delta)
if newStock64 > int64(1<<31-1) {
    return 0, 0, ErrStockOverflow
}
```

Test otomatis tersedia di `src/repository/product_delta_integration_test.go` (build tag `integration`):

```bash
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable' \
  go test -tags integration ./src/repository/ \
  -run TestUpdateProductDeltaBoundaries -v
```

Hasil:

```text
--- PASS: TestUpdateProductDeltaBoundaries
    --- PASS: MaxInt32_melebihi_stok
    --- PASS: delta_lebih_besar_dari_stok
    --- PASS: delta_tepat_sama_dengan_stok_(stok_jadi_nol)
    --- PASS: delta_pada_stok_nol
    --- PASS: MinInt32_menyebabkan_overflow_int32
    --- PASS: delta_negatif_melakukan_restock
PASS
ok  	learn-go/src/repository	0.047s
```

Test yang sama juga dijalankan dengan race detector:

```bash
CGO_ENABLED=1 \
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable' \
go test -race -tags integration ./src/repository/ \
  -run TestUpdateProductDeltaBoundaries -v
```

Hasil:

```text
--- PASS: TestUpdateProductDeltaBoundaries (0.12s)
    --- PASS: MaxInt32_melebihi_stok (0.05s)
    --- PASS: delta_lebih_besar_dari_stok (0.01s)
    --- PASS: delta_tepat_sama_dengan_stok_(stok_jadi_nol) (0.01s)
    --- PASS: delta_pada_stok_nol (0.01s)
    --- PASS: MinInt32_menyebabkan_overflow_int32 (0.01s)
    --- PASS: delta_negatif_melakukan_restock (0.01s)
PASS
ok  	learn-go/src/repository	1.131s
```

Tidak ada laporan `DATA RACE`. Kenaikan waktu (0.047s → 1.131s) berasal dari overhead instrumentasi race detector, bukan masalah pada kode.

| Kasus | Stok awal | Delta | Hasil |
|---|---|---|---|
| MaxInt32 melebihi stok | 10 | 2147483647 | `ErrInsufficientStock` |
| delta lebih besar dari stok | 10 | 11 | `ErrInsufficientStock` |
| delta tepat sama dengan stok | 10 | 10 | sukses, stok 10 → 0 |
| delta pada stok nol | 0 | 1 | `ErrInsufficientStock` |
| MinInt32 menyebabkan overflow | 0 | -2147483648 | `ErrStockOverflow` |
| delta negatif melakukan restock | 0 | -5 | sukses, stok 0 → 5 |

Dengan demikian:

- delta positif lebih besar dari stok menghasilkan `ErrInsufficientStock`;
- delta negatif yang menyebabkan hasil lebih besar dari `MaxInt32` menghasilkan `ErrStockOverflow`;
- stok tidak boleh menjadi negatif;
- hasil stok tidak dapat overflow int32;
- `delta = 0` ditolak di layer binding (`ne=0`) sebelum mencapai repository.

## 4. Verifikasi SELECT FOR UPDATE

Pengujian ini memakai dua transaksi yang benar-benar mengubah stok row yang sama, bukan hanya mengamati lock. Produk uji dibuat dengan stok awal 100.

Sesi A menjalankan:

```sql
BEGIN;
SELECT stock FROM products WHERE id = <test-id> FOR UPDATE;
SELECT pg_sleep(3);
UPDATE products SET stock = stock - 10 WHERE id = <test-id>;
COMMIT;
```

Sesi B menjalankan, sekitar 1 detik kemudian:

```sql
BEGIN;
SELECT clock_timestamp() AS session_b_acquired_at, stock
FROM products WHERE id = <test-id> FOR UPDATE;
UPDATE products SET stock = stock - 10 WHERE id = <test-id>;
COMMIT;
```

Saat Sesi A masih memegang lock, PostgreSQL menunjukkan Sesi B menunggu:

```text
 pid | wait_event_type | query
-----+-----------------+--------------------------------------------------------
 649 | Lock            | BEGIN; SELECT clock_timestamp() AS session_b_acquired_at...
```

Hasil yang tercatat:

```text
Sesi A membaca stok : 100
Sesi B menunggu     : wait_event_type = Lock
Sesi B baru membaca : 90   (bukan 100 — membaca hasil commit Sesi A)
Stok akhir          : 80   (100 - 10 - 10)
```

Ini membuktikan tiga hal sekaligus: lock berlaku pada row yang dibaca, lock ditahan sampai transaksi berakhir, dan transaksi kedua membaca hasil transaksi pertama (tidak ada lost update).

## 5. Test rollback

Test:

```bash
./loadtest/run-rollback-audit-error.sh
```

Test memasang trigger sementara yang sengaja menggagalkan insert audit. Hasil:

```text
stok awal       : 100
stok akhir      : 100
audit log count : 0
intentional_audit_failures: 5
checks_succeeded: 100%
http_req_failed: 0%
PASS: UPDATE stok dan audit sama-sama rollback
```

Trigger otomatis dihapus setelah test selesai.
