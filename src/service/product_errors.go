package service

import "errors"

var (
	ErrProductNotFound      = errors.New("product not found")
	ErrProductNotAuthorized = errors.New("You have no authorized to delete this product")
	ErrProductNameRequired  = errors.New("product name is required")
	ErrProductPriceInvalid  = errors.New("product price must be greater than 0")
	ErrProductStockInvalid  = errors.New("product stock cannot be negative")
	ErrInsufficientStock    = errors.New("product stock insufficient")
	ErrStockOverflow        = errors.New("resulting stock exceeds int32 limit")
	ErrDeadlock             = errors.New("deadlock detected, please retry")
	ErrTransferSameProduct  = errors.New("from_id and to_id must be different")
)
