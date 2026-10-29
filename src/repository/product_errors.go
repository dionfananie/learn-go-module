package repository

import "errors"

var (
	ErrProductNotFound      = errors.New("product not found")
	ErrProductNotAuthorized = errors.New("You have no authorized to delete this product")
	ErrInsufficientStock    = errors.New("product stock insufficient")
	ErrStockOverflow        = errors.New("resulting stock exceeds int32 limit")
)
