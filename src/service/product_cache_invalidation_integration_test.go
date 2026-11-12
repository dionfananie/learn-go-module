//go:build integration

package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"learn-go/src/cache"
	"learn-go/src/entities"
	"learn-go/src/repository"
)

// Integration test untuk cache invalidation race (double-delete).
//
// Jalankan dengan database + Redis nyata:
//
//	TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable' \
//	TEST_REDIS_ADDR='localhost:6379' \
//	  go test -tags integration ./src/service/ -run TestCacheInvalidationRace -v
//
// Tanpa double-delete, ada jendela di mana reader yang sudah membaca stok
// lama menulisnya kembali ke cache SETELAH writer menghapus cache, sehingga
// cache basi sampai TTL habis. Test ini membanjiri endpoint dengan reader +
// writer bersamaan, lalu memastikan stok di Redis selalu sama dengan DB.

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

func openTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:                  addr,
		MaxRetries:            -1,
		ContextTimeoutEnabled: true,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis tidak tersedia di %s: %v", addr, err)
	}
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

func TestCacheInvalidationRace(t *testing.T) {
	db := openTestDB(t)
	rdb := openTestRedis(t)
	ctx := context.Background()

	// Siapkan user (audit log butuh user_id) dan produk dengan stok besar.
	var uid string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (name, phone_number, password) VALUES ($1, $2, $3) RETURNING id`,
		"cache-race", fmt.Sprintf("08%014d", time.Now().UnixNano()%100000000000000), "secret123",
	).Scan(&uid); err != nil {
		t.Fatal(err)
	}

	const initialStock = 1000000
	var pid int
	if err := db.QueryRowContext(ctx,
		`INSERT INTO products (name, price, stock) VALUES ('cache-race', 1000, $1) RETURNING id`,
		initialStock,
	).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	key := productCacheKey(pid)

	t.Cleanup(func() {
		db.Exec(`DELETE FROM products_audit_logs WHERE product_id = $1`, pid)
		db.Exec(`DELETE FROM products WHERE id = $1`, pid)
		db.Exec(`DELETE FROM users WHERE id = $1`, uid)
		rdb.Del(context.Background(), key)
	})

	repo := repository.NewProductRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	svc := NewProductService(repo, auditRepo, cache.New(rdb, 100*time.Millisecond), nil)

	// Warm-up supaya race terjadi pada key yang sudah ada.
	if _, err := svc.GetProduct(ctx, pid); err != nil {
		t.Fatalf("warm-up gagal: %v", err)
	}

	// Burst: reader membaca terus sementara writer mengubah stok dan
	// meng-invalidate cache setiap commit.
	const (
		readers     = 8
		readerIters = 200
		writers     = 4
		writerIters = 50
	)

	var (
		wg       sync.WaitGroup
		firstErr error
		mu       sync.Mutex
	)
	recordErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	for g := 0; g < readers; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < readerIters; i++ {
				if _, err := svc.GetProduct(ctx, pid); err != nil {
					recordErr(err)
					return
				}
			}
		}()
	}
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < writerIters; i++ {
				if _, err := svc.Transaction(ctx, pid, 1, uid); err != nil {
					recordErr(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		t.Fatalf("burst gagal: %v", firstErr)
	}

	// Beri waktu delete kedua (AfterFunc, 200 ms) untuk selesai.
	time.Sleep(cacheInvalidateDelay + 300*time.Millisecond)

	// Stok akhir di DB harus konsisten: awal - (writer x iterasi).
	var dbStock int32
	if err := db.QueryRowContext(ctx,
		`SELECT stock FROM products WHERE id = $1`, pid,
	).Scan(&dbStock); err != nil {
		t.Fatal(err)
	}
	expected := int32(initialStock - writers*writerIters)
	if dbStock != expected {
		t.Fatalf("stok DB tidak konsisten: got %d want %d", dbStock, expected)
	}

	// Inti test: kalau cache masih ada, isinya tidak boleh basi.
	if raw, err := rdb.Get(ctx, key).Bytes(); err == nil {
		var cached entities.Product
		if json.Unmarshal(raw, &cached) == nil && cached.Stock != dbStock {
			t.Fatalf("STALE CACHE: cache stock=%d, DB stock=%d (double-delete gagal)", cached.Stock, dbStock)
		}
	}

	// Pembacaan normal setelah settle juga harus mengembalikan nilai benar.
	got, err := svc.GetProduct(ctx, pid)
	if err != nil {
		t.Fatalf("baca setelah settle gagal: %v", err)
	}
	if got.Stock != dbStock {
		t.Fatalf("GetProduct stale: got %d want %d", got.Stock, dbStock)
	}
}
