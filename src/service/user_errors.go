package service

import "errors"

var (
	ErrUserNotFound         = errors.New("user not found")
	ErrUserNameRequired     = errors.New("user name is required")
	ErrUserPasswordRequired = errors.New("user password must be filled")
	ErrUserPhoneRequired    = errors.New("user phone must be filled")
)
