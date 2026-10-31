//go:build integration

package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"learn-go/src/repository"
)

func isDeadlockErr(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

// TestTransferStockNoDeadlock memastikan transfer dua arah yang berjalan
// bersamaan TIDAK menghasilkan deadlock setelah urutan lock dibuat konsisten
// (baris dengan id terkecil selalu dikunci lebih dulu).
//
// Jalankan dengan database nyata:
//
//	TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable' \
//	  go test -tags integration ./src/repository/ -run TestTransferStockNoDeadlock -v
func TestTransferStockNoDeadlock(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := repository.NewProductRepository(db)

	const initialStock = 100000
	const workers = 20
	const rounds = 15
	const amount int32 = 1

	a := newTestProduct(t, db, initialStock)
	b := newTestProduct(t, db, initialStock)

	runTransfer := func(from, to int) error {
		tx, err := repo.BeginTx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, _, _, _, err := repo.TransferStock(ctx, tx, from, to, amount); err != nil {
			return err
		}
		return tx.Commit()
	}

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		deadlocks int
		ok        int
		otherErrs []error
	)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				from, to := a, b
				if w%2 == 1 {
					from, to = b, a // arah berlawanan
				}

				err := runTransfer(from, to)

				mu.Lock()
				switch {
				case err == nil:
					ok++
				case isDeadlockErr(err):
					deadlocks++
				default:
					otherErrs = append(otherErrs, err)
				}
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	t.Logf("transfer sukses=%d deadlock=%d error lain=%d", ok, deadlocks, len(otherErrs))

	if deadlocks != 0 {
		t.Fatalf("masih terjadi deadlock: %d (urutan lock belum konsisten)", deadlocks)
	}
	if len(otherErrs) > 0 {
		t.Fatalf("error tak terduga: %v", otherErrs[0])
	}

	// Invariant: transfer hanya memindahkan stok, total harus kekal.
	var total int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(stock), 0) FROM products WHERE id IN ($1, $2)`, a, b,
	).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2*initialStock {
		t.Fatalf("total stok berubah: want %d, got %d", 2*initialStock, total)
	}
}
