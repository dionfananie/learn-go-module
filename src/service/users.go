package service

import (
	"context"
	"database/sql"
	"errors"
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

func (s *UserService) Register(ctx context.Context, user *entities.User) (*entities.UserResponse, error) {
	hashedPassword, err := password.Hash(user.Password)
	if err != nil {
		return nil, err
	}
	user.Password = hashedPassword
	userResponse, err := s.repo.RegisterUser(ctx, user)
	if err != nil {
		return nil, err
	}

	userResponse.AccessToken = jwt.Generate(&jwt.TokenPayload{UserId: userResponse.ID})
	return userResponse, nil
}

func (s *UserService) LoginUser(ctx context.Context, user *entities.UserLoginRequest) (*entities.UserResponse, error) {
	userResponse, err := s.repo.LoginUser(ctx, user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	if err := password.Verify(userResponse.Password, user.Password); err != nil {
		return nil, ErrInvalidCredentials
	}
	return &entities.UserResponse{
			ID:          userResponse.ID,
			Name:        userResponse.Name,
			AccessToken: jwt.Generate(&jwt.TokenPayload{userResponse.ID}),
			CreatedAt:   userResponse.CreatedAt},
		nil
}
