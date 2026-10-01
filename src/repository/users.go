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

func (r *UsersRepository) RegisterUser(ctx context.Context, user *entities.User) (string, error) {
	err := r.db.QueryRowContext(ctx,
		"INSERT INTO users (name, password, phone_number) VALUES($1, $2, $3) RETURNING id", user.Name, user.Password, user.PhoneNumber).Scan(&user.ID)
	if err != nil {
		return "", err
	}
	return user.ID, nil
}
