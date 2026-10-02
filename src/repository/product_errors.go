package repository

import "errors"

var (
	ErrProductNotFound      = errors.New("product not found")
	ErrProductNotAuthorized = errors.New("You have no authorized to delete this product")
)
