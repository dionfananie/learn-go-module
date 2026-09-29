package database

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func Connect() (*sql.DB, error) {
	dsn := "postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Printf("Error Connect Dbase %v", err)
		return nil, fmt.Errorf("Error Open Database %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		log.Printf("Error Ping %v", err)
		return nil, fmt.Errorf("Error Open Database %w", err)

	}

	return db, nil

}
