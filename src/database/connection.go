package database

import (
	"context"
	"database/sql"
	"fmt"
	"learn-go/src/config"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

func Connect() (*sql.DB, error) {
	err := godotenv.Load()
	if err != nil {
		log.Printf("Error Read Env")
	}

	strConnection := fmt.Sprintf("postgres://%v:%v@%v:%v/%v?%v", config.DB_USERNAME, config.DB_PASSWORD, config.DB_HOST, config.DB_PORT, config.DB_NAME, config.DB_PARAMS)
	// Define connection pool parameters (adjust as needed)
	maxOpenConns := 100 // Maximum number of open connections in the pool
	maxIdleConns := 10  // Maximum number of idle connections in the pool
	db, err := sql.Open("pgx", strConnection)
	if err != nil {
		log.Printf("Error Connect Dbase %v", err)
		return nil, fmt.Errorf("Error Open Database %w", err)
	}

	// Create connection pool
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		log.Printf("Error Ping %v", err)
		return nil, fmt.Errorf("Error Open Database %w", err)

	}

	return db, nil

}
