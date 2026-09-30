package service

import (
	"context"
	"learn-go/src/entities"
	"learn-go/src/repository"
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

func (s *ProductService) GetProductAll(ctx context.Context) ([]entities.Product, error) {
	products, err := s.repo.GetProductAll(ctx)
	if err != nil {
		return nil, err
	}
	return products, nil
}

func (s *ProductService) GetProduct(ctx context.Context, id string) (*entities.Product, error) {
	product, err := s.repo.GetProduct(ctx, id)
	if err != nil {
		return nil, err
	}
	return product, nil
}
