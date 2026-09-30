package repository

import (
	"context"
	"database/sql"
	"learn-go/entities"
)

type ProductRepository struct {
	db *sql.DB
}

func NewProductRepository(db *sql.DB) *ProductRepository {
	return &ProductRepository{db: db}

}
func (r *ProductRepository) Create(ctx context.Context, product *entities.Product) error {
	err := r.db.QueryRow(
		"INSERT INTO products (name, price, stock) VALUES ($1, $2, $3) RETURNING id", product.Name, product.Price, product.Stock,
	).Scan(&product.ID)

	if err != nil {
		return err
	}
	return nil
}
