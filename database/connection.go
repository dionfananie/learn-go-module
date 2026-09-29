package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

func Connect() (*sql.DB, error) {
	err := godotenv.Load()
	if err != nil {
		log.Printf("Error Read Env")
	}
	dsn := os.Getenv("DATABASE_URL")
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
