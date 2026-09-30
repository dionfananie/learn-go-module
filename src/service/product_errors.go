package service

import "errors"

var (
	ErrProductNotFound     = errors.New("product not found")
	ErrProductNameRequired = errors.New("product name is required")
	ErrProductPriceInvalid = errors.New("product price must be greater than 0")
	ErrProductStockInvalid = errors.New("product stock cannot be negative")
)
