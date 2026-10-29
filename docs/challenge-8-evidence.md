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

Bukti pengujian repository:

```text
delta=-2147483648: overflow=true error=resulting stock exceeds int32 limit
delta=1 with stock=0: insufficient=true error=product stock insufficient
```

Dengan demikian:

- delta positif lebih besar dari stok menghasilkan `ErrInsufficientStock`;
- delta negatif yang menyebabkan hasil lebih besar dari `MaxInt32` menghasilkan `ErrStockOverflow`;
- stok tidak boleh menjadi negatif;
- hasil stok tidak dapat overflow int32.

## 4. Verifikasi SELECT FOR UPDATE

Sesi A menjalankan:

```sql
BEGIN;
SELECT id, stock FROM products WHERE id = 9980 FOR UPDATE;
SELECT pg_sleep(4);
COMMIT;
```

Sesi B menjalankan query `SELECT ... FOR UPDATE` pada row yang sama sebelum Sesi A commit. Saat Sesi A masih tidur, PostgreSQL menunjukkan:

```text
pid | wait_event_type | state  | query
----+-----------------+--------+----------------------------------------------
359 | Lock            | active | BEGIN; SELECT id, stock FROM products
    |                 |        | WHERE id=9980 FOR UPDATE; COMMIT;
```

Sesi B baru memperoleh row setelah Sesi A melakukan `COMMIT`. Ini membuktikan lock berlaku pada row yang dibaca dan ditahan sampai transaksi selesai.

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
