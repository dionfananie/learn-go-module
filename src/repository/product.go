package repository

import (
	"context"
	"database/sql"
	"errors"
	"learn-go/src/entities"
)

type ProductRepository struct {
	db *sql.DB
}

func NewProductRepository(db *sql.DB) *ProductRepository {
	return &ProductRepository{db: db}

}
func (r *ProductRepository) Create(ctx context.Context, product *entities.Product, userId string) error {
	err := r.db.QueryRowContext(ctx,
		"INSERT INTO products (name, price, stock, created_by) VALUES ($1, $2, $3, $4) RETURNING id", product.Name, product.Price, product.Stock, userId,
	).Scan(&product.ID)

	return err
}

func (r *ProductRepository) GetProductAll(ctx context.Context) ([]entities.Product, error) {

	rows, err := r.db.QueryContext(ctx,
		"SELECT id, name, price, stock FROM products ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	products := make([]entities.Product, 0)

	for rows.Next() {
		var product entities.Product
		if err := rows.Scan(
			&product.ID,
			&product.Name,
			&product.Price,
			&product.Stock,
		); err != nil {
			return nil, err
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		return nil, err

	}
	return products, nil
}

func (r *ProductRepository) GetProduct(ctx context.Context, id int) (*entities.Product, error) {
	var product entities.Product

	err := r.db.QueryRowContext(ctx,
		"SELECT id, name, price, stock FROM products WHERE id = $1", id).Scan(&product.ID, &product.Name, &product.Price, &product.Stock)

	if err != nil {
		return nil, err

	}

	return &product, nil
}

func (r *ProductRepository) DeleteProduct(ctx context.Context, id int, userId string) error {
	query := `
		WITH target AS (
			SELECT id, created_by FROM products WHERE id = $1
		),
		deleted_product AS(
			DELETE FROM products WHERE id = $1 AND created_by = $2 RETURNING id
		)
		SELECT
			EXISTS(SELECT 1 FROM target) AS is_exists,
			EXISTS(SELECT 1 from deleted_product) AS has_deleted
			`
	var is_exists bool
	var has_deleted bool
	err := r.db.QueryRowContext(ctx, query, id, userId).Scan(&is_exists, &has_deleted)
	if err != nil {
		return err
	}
	if !is_exists {
		return ErrProductNotFound
	}
	if !has_deleted {
		return ErrProductNotAuthorized
	}

	return nil
}

func (r *ProductRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, nil)
}
func (r *ProductRepository) UpdateProduct(ctx context.Context, tx *sql.Tx, productId int, delta int32) (int, int, error) {
	var stock int32

	// 1) Kunci row + baca stok terkini.
	//    FOR UPDATE menahan lock sampai tx di-COMMIT/ROLLBACK, sehingga tidak
	//    ada transaksi lain yang bisa mengubah stok di antara langkah baca
	//    (di sini) dan langkah update (di bawah).
	err := tx.QueryRowContext(ctx,
		"SELECT stock FROM products WHERE id = $1 FOR UPDATE",
		productId,
	).Scan(&stock)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, ErrProductNotFound // produk tidak ada -> 404
		}
		return 0, 0, err
	}

	// 2) Keputusan bisnis dilakukan di Go — aman karena row terkunci.
	//    delta positif = kurangi stok; delta negatif = tambah (restock).
	if stock < delta {
		return 0, 0, ErrInsufficientStock // stok tidak cukup -> 400
	}

	// 3) Hitung menggunakan int64 terlebih dahulu agar delta negatif ekstrem
	//    (mis. math.MinInt32) tidak overflow saat restock.
	newStock64 := int64(stock) - int64(delta)
	if newStock64 > int64(1<<31-1) {
		return 0, 0, ErrStockOverflow
	}

	// Tulis nilai yang sudah dicek. Lock masih dipegang sampai Commit.
	newStock := int32(newStock64)
	_, err = tx.ExecContext(ctx,
		"UPDATE products SET stock = $1 WHERE id = $2",
		newStock, productId,
	)
	if err != nil {
		return 0, 0, err
	}

	return int(stock), int(newStock), nil
}

// TransferStock memindahkan sejumlah stok dari satu produk ke produk lain di dalam
// SATU transaksi. Urutan lock selalu: source (fromId) dulu, lalu destination (toId).
//
//	PERHATIAN — inilah sumber deadlock:
//	dua transfer berlawanan arah (A->B dan B->A) yang berjalan bersamaan akan
//	mengunci dalam urutan terbalik:
//	  tx1: kunci A, lalu menunggu B
//	  tx2: kunci B, lalu menunggu A
//	=> circular wait => PostgreSQL membatalkan salah satu dengan SQLSTATE 40P01.
func (r *ProductRepository) TransferStock(ctx context.Context, tx *sql.Tx, fromId, toId int, amount int32) (fromOld, fromNew, toOld, toNew int, err error) {
	// 1) Kunci + baca stok source.
	var fromStock int32
	err = tx.QueryRowContext(ctx,
		"SELECT stock FROM products WHERE id = $1 FOR UPDATE",
		fromId,
	).Scan(&fromStock)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, 0, 0, ErrProductNotFound
		}
		return 0, 0, 0, 0, err
	}

	// 2) Kunci + baca stok destination. Baris inilah yang memicu deadlock
	//    ketika lawan transaksi memegang lock destination dan sedang menunggu source.
	var toStock int32
	err = tx.QueryRowContext(ctx,
		"SELECT stock FROM products WHERE id = $1 FOR UPDATE",
		toId,
	).Scan(&toStock)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, 0, 0, ErrProductNotFound
		}
		return 0, 0, 0, 0, err
	}

	// 3) Validasi stok source cukup.
	if fromStock < amount {
		return 0, 0, 0, 0, ErrInsufficientStock
	}

	// 4) Hitung nilai baru (int64 dulu agar restock ekstrem tidak overflow).
	toNew64 := int64(toStock) + int64(amount)
	if toNew64 > int64(1<<31-1) {
		return 0, 0, 0, 0, ErrStockOverflow
	}

	fromNew32 := fromStock - amount
	toNew32 := int32(toNew64)

	// 5) Tulis kedua baris. Lock masih dipegang sampai tx di-COMMIT.
	if _, err = tx.ExecContext(ctx,
		"UPDATE products SET stock = $1 WHERE id = $2",
		fromNew32, fromId,
	); err != nil {
		return 0, 0, 0, 0, err
	}
	if _, err = tx.ExecContext(ctx,
		"UPDATE products SET stock = $1 WHERE id = $2",
		toNew32, toId,
	); err != nil {
		return 0, 0, 0, 0, err
	}

	return int(fromStock), int(fromNew32), int(toStock), int(toNew32), nil
}
