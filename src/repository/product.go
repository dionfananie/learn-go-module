package repository

import (
	"context"
	"database/sql"
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
	var (
		productExists bool
		oldStock      sql.NullInt64
		newStock      sql.NullInt64
	)
	err := tx.QueryRowContext(ctx, `
		WITH target AS (
			SELECT id FROM products WHERE id = $2
		),
		updated AS (
			UPDATE products
			SET stock = stock - $1
			WHERE id = $2 AND stock >= $1
			RETURNING stock + $1 AS old_stock, stock AS new_stock
		)
		SELECT
			EXISTS(SELECT 1 FROM target) AS product_exists,
			(SELECT old_stock FROM updated) AS old_stock,
			(SELECT new_stock FROM updated) AS new_stock`,
		delta, productId).Scan(&productExists, &oldStock, &newStock)
	if err != nil {
		return 0, 0, err
	}

	if !productExists {
		return 0, 0, ErrProductNotFound // produknya memang tidak ada -> 404
	}
	if !oldStock.Valid || !newStock.Valid {
		return 0, 0, ErrInsufficientStock // produk ada, tapi stok tidak cukup -> 400
	}
	return int(oldStock.Int64), int(newStock.Int64), nil
}
