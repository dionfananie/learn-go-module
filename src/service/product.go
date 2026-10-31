package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"learn-go/src/cache"
	"learn-go/src/entities"
	"learn-go/src/repository"
	"time"
)

const productCacheTTL = 5 * time.Minute

func productCacheKey(id int) string {
	return fmt.Sprintf("productId:%d", id)
}

type ProductService struct {
	repo      *repository.ProductRepository
	auditRepo *repository.AuditRepository
	cache     *cache.Client
}

func NewProductService(repo *repository.ProductRepository, auditRepo *repository.AuditRepository, cacheClient *cache.Client) *ProductService {
	return &ProductService{repo: repo, auditRepo: auditRepo, cache: cacheClient}
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
	key := productCacheKey(id)

	var cached entities.Product
	if s.cache.GetJSON(ctx, key, &cached) {
		return &cached, nil
	}

	product, err := s.repo.GetProduct(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProductNotFound
	}
	if err != nil {
		return nil, err
	}

	s.cache.SetJSON(ctx, key, product, productCacheTTL)
	return product, nil
}

func (s *ProductService) GetProductAuditLogs(ctx context.Context, productId int) ([]entities.AuditLog, error) {
	logs, err := s.auditRepo.GetByProductID(ctx, productId)
	if err != nil {
		return nil, err
	}
	return logs, nil
}

func (s *ProductService) DeleteProduct(ctx context.Context, id int, userId string) error {
	err := s.repo.DeleteProduct(ctx, id, userId)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrProductNotFound):
			return ErrProductNotFound // 404
		case errors.Is(err, repository.ErrProductNotAuthorized):
			return ErrProductNotAuthorized // 403
		}
		return err
	}
	s.cache.Delete(ctx, productCacheKey(id))
	return nil
}

type AdjustStockResult struct {
	AuditID int64
}

func (s *ProductService) Transaction(ctx context.Context, productId int, delta int32, userId string) (*AdjustStockResult, error) {
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var action string
	if delta >= 0 {
		action = "subtract" // delta positif = stok berkurang
	} else {
		action = "add" // delta negatif = stok bertambah (restock)
	}

	oldStock, newStock, err := s.repo.UpdateProduct(ctx, tx, productId, delta)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrProductNotFound):
			return nil, ErrProductNotFound // 404
		case errors.Is(err, repository.ErrInsufficientStock):
			return nil, ErrInsufficientStock // 400
		case errors.Is(err, repository.ErrStockOverflow):
			return nil, ErrStockOverflow // 400
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
	s.cache.Delete(ctx, productCacheKey(productId))
	return &AdjustStockResult{AuditID: auditID}, nil
}

type TransferStockResult struct {
	FromOld int
	FromNew int
	ToOld   int
	ToNew   int
}

// TransferStock memindahkan sejumlah stok antar dua produk dalam satu transaksi.
// Deadlock (SQLSTATE 40P01) dipetakan menjadi ErrDeadlock agar handler bisa
// membalas dengan status yang jelas (409) alih-alih 500 generik.
func (s *ProductService) TransferStock(ctx context.Context, fromId, toId int, amount int32, userId string) (*TransferStockResult, error) {
	if fromId == toId {
		return nil, ErrTransferSameProduct
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	fromOld, fromNew, toOld, toNew, err := s.repo.TransferStock(ctx, tx, fromId, toId, amount)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrProductNotFound):
			return nil, ErrProductNotFound // 404
		case errors.Is(err, repository.ErrInsufficientStock):
			return nil, ErrInsufficientStock // 400
		case errors.Is(err, repository.ErrStockOverflow):
			return nil, ErrStockOverflow // 400
		case isDeadlock(err):
			return nil, ErrDeadlock // 409
		}
		return nil, err
	}

	// Catat jejak audit untuk kedua sisi transfer.
	if _, err := s.auditRepo.Create(ctx, tx, fromId, userId, "transfer_out", fromOld, fromNew); err != nil {
		return nil, err
	}
	if _, err := s.auditRepo.Create(ctx, tx, toId, userId, "transfer_in", toOld, toNew); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		if isDeadlock(err) {
			return nil, ErrDeadlock // 409
		}
		return nil, err
	}
	s.cache.Delete(ctx, productCacheKey(fromId), productCacheKey(toId))

	return &TransferStockResult{FromOld: fromOld, FromNew: fromNew, ToOld: toOld, ToNew: toNew}, nil
}
