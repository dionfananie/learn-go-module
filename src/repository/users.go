package repository

import (
	"context"
	"database/sql"
	"learn-go/src/entities"
)

type UsersRepository struct {
	db *sql.DB
}

func (r *UsersRepository) RegisterUser(ctx context.Context, user *entities.User) error {

	err := r.db.QueryRowContext(ctx,
		"INSERT INTO users (name, password) VALUES($1, $2) RETURNING id", user.Name, user.Password).Scan(&user.ID)
	return err
}
