package service

import (
	"context"
	"learn-go/src/entities"
	"learn-go/src/helpers/password"
	"learn-go/src/repository"
)

type UserService struct {
	repo *repository.UsersRepository
}

func NewUserService(repo *repository.UsersRepository) *UserService {
	return &UserService{repo}
}

func (s *UserService) Register(ctx context.Context, user *entities.User) error {
	if user.Name == "" {
		return ErrUserNameRequired
	}
	if user.Password == "" {
		return ErrUserPasswordRequired
	}

	hashedPassword := password.Hash(user.Password)
	user.Password = hashedPassword
	return s.repo.RegisterUser(ctx, user)
}
