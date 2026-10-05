package service

import (
	"context"
	"database/sql"
	"errors"
	"learn-go/src/entities"
	"learn-go/src/repository"
)

type ProductService struct {
	repo      *repository.ProductRepository
	auditRepo *repository.AuditRepository
}

func NewProductService(repo *repository.ProductRepository, auditRepo *repository.AuditRepository) *ProductService {
	return &ProductService{repo, auditRepo}
}

func (s *ProductService) Create(ctx context.Context, product *entities.Product, userId string) error {
	if product.Name == "" {
		return ErrProductNameRequired
	}

	if product.Price <= 0 {
		return ErrProductPriceInvalid
	}

	if product.Stock < 0 {
		return ErrProductStockInvalid
	}
	return s.repo.Create(ctx, product, userId)
}

func (s *ProductService) GetProductAll(ctx context.Context) ([]entities.Product, error) {
	products, err := s.repo.GetProductAll(ctx)

	if err != nil {
		return nil, err
	}
	return products, nil
}

func (s *ProductService) GetProduct(ctx context.Context, id int) (*entities.Product, error) {
	product, err := s.repo.GetProduct(ctx, id)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProductNotFound
	}
	if err != nil {
		return nil, err
	}
	return product, nil
}

func (s *ProductService) DeleteProduct(ctx context.Context, id int, userId string) error {
	err := s.repo.DeleteProduct(ctx, id, userId)
	switch {
	case errors.Is(err, repository.ErrProductNotFound):
		return ErrProductNotFound // 404
	case errors.Is(err, repository.ErrProductNotAuthorized):
		return ErrProductNotAuthorized // 403
	}
	return err

}

type AdjustStockResult struct {
	AuditID int64
}

func (s *ProductService) Transaction(ctx context.Context, productId int, delta int32, userId string, action string) (*AdjustStockResult, error) {
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	oldStock, newStock, err := s.repo.UpdateProduct(ctx, tx, productId, delta, userId, action)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInsufficientStock // WHERE gagal = stok kurang / produk tak ada
		}
		return nil, err
	}
	auditID, err := s.auditRepo.Create(ctx, tx, productId, userId, action, oldStock, newStock)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &AdjustStockResult{AuditID: auditID}, nil
}
