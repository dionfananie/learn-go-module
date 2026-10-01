package repository

import (
	"context"
	"database/sql"
	"learn-go/src/entities"
)

type UsersRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UsersRepository {
	return &UsersRepository{db}
}

func (r *UsersRepository) RegisterUser(ctx context.Context, user *entities.User) (*entities.UserResponse, error) {
	var resp entities.UserResponse
	err := r.db.QueryRowContext(ctx,
		"INSERT INTO users (name, password, phone_number) VALUES($1, $2, $3) RETURNING id, name, created_at", user.Name, user.Password, user.PhoneNumber).Scan(&resp.ID, &resp.Name, &resp.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}
