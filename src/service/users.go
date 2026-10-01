package service

import (
	"context"
	"learn-go/src/entities"
	jwt "learn-go/src/helpers"
	"learn-go/src/helpers/password"
	"learn-go/src/repository"
)

type UserService struct {
	repo *repository.UsersRepository
}

func NewUserService(repo *repository.UsersRepository) *UserService {
	return &UserService{repo}
}

func (s *UserService) Register(ctx context.Context, user *entities.User) (string, error) {
	hashedPassword := password.Hash(user.Password)
	user.Password = hashedPassword

	userId, err := s.repo.RegisterUser(ctx, user)
	if err != nil {
		return "", err
	}
	accessToken := jwt.Generate(&jwt.TokenPayload{UserId: userId})
	return accessToken, nil
}
