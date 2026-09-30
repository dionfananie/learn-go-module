package repository

import (
	"context"
	"database/sql"
	"fmt"
	"learn-go/src/entities"
)

type ProductRepository struct {
	db *sql.DB
}

func NewProductRepository(db *sql.DB) *ProductRepository {
	return &ProductRepository{db: db}

}
func (r *ProductRepository) Create(ctx context.Context, product *entities.Product) error {
	err := r.db.QueryRowContext(ctx,
		"INSERT INTO products (name, price, stock) VALUES ($1, $2, $3) RETURNING id", product.Name, product.Price, product.Stock,
	).Scan(&product.ID)

	if err != nil {
		return err
	}
	return nil
}

func (r *ProductRepository) GetProductAll(ctx context.Context) ([]entities.Product, error) {

	rows, err := r.db.QueryContext(ctx,
		"SELECT id, name, price, stock FROM products ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("Error fetching all products %v", err)
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

func (r *ProductRepository) GetProduct(ctx context.Context, id string) (*entities.Product, error) {
	var product entities.Product

	err := r.db.QueryRowContext(ctx,
		"SELECT id, name, price, stock FROM products WHERE id = $1", id).Scan(&product.ID, &product.Name, &product.Price, &product.Stock)

	if err != nil {
		return nil, err

	}

	return &product, nil
}
