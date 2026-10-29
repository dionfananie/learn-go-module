//go:build integration

package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"learn-go/src/repository"
)

// Integration test untuk batas nilai delta pada UpdateProduct.
// Jalankan dengan database nyata:
//
//	TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable' \
//	  go test -tags integration ./src/repository/ -run TestUpdateProductDeltaBoundaries -v
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL tidak diset; lewati integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestProduct(t *testing.T, db *sql.DB, stock int) int {
	t.Helper()
	var id int
	err := db.QueryRow(
		`INSERT INTO products (name, price, stock) VALUES ($1, 1000, $2) RETURNING id`,
		"delta-boundary-test", stock,
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM products WHERE id = $1`, id); err != nil {
			t.Logf("cleanup product %d: %v", id, err)
		}
	})
	return id
}

func TestUpdateProductDeltaBoundaries(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := repository.NewProductRepository(db)

	cases := []struct {
		name    string
		stock   int
		delta   int32
		wantErr error
		wantOld int
		wantNew int
	}{
		{
			name:    "MaxInt32 melebihi stok",
			stock:   10,
			delta:   math.MaxInt32,
			wantErr: repository.ErrInsufficientStock,
		},
		{
			name:    "delta lebih besar dari stok",
			stock:   10,
			delta:   11,
			wantErr: repository.ErrInsufficientStock,
		},
		{
			name:    "delta tepat sama dengan stok (stok jadi nol)",
			stock:   10,
			delta:   10,
			wantOld: 10,
			wantNew: 0,
		},
		{
			name:    "delta pada stok nol",
			stock:   0,
			delta:   1,
			wantErr: repository.ErrInsufficientStock,
		},
		{
			name:    "MinInt32 menyebabkan overflow int32",
			stock:   0,
			delta:   math.MinInt32,
			wantErr: repository.ErrStockOverflow,
		},
		{
			name:    "delta negatif melakukan restock",
			stock:   0,
			delta:   -5,
			wantOld: 0,
			wantNew: 5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := newTestProduct(t, db, tc.stock)

			tx, err := repo.BeginTx(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()

			oldStock, newStock, err := repo.UpdateProduct(ctx, tx, id, tc.delta)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error: want %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if oldStock != tc.wantOld {
				t.Fatalf("old stock: want %d, got %d", tc.wantOld, oldStock)
			}
			if newStock != tc.wantNew {
				t.Fatalf("new stock: want %d, got %d", tc.wantNew, newStock)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
