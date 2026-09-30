package service

import (
	"context"
	"errors"
	"learn-go/src/entities"
	"learn-go/src/repository"
)

var (
	ErrProductNameRequired = errors.New("product name is required")
	ErrProductPriceInvalid = errors.New("product price must be greater than 0")
	ErrProductStockInvalid = errors.New("product stock cannot be negative")
)

type ProductService struct {
	repo *repository.ProductRepository
}

func NewProductService(repo *repository.ProductRepository) *ProductService {
	return &ProductService{repo}
}

func (s *ProductService) Create(ctx context.Context, product *entities.Product) error {
	if product.Name == "" {
		return ErrProductNameRequired
	}

	if product.Price <= 0 {
		return ErrProductPriceInvalid
	}

	if product.Stock < 0 {
		return ErrProductStockInvalid
	}
	return s.repo.Create(ctx, product)
}
