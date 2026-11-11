//go:build integration

package main_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"learn-go/src/cache"
	"learn-go/src/handler"
	"learn-go/src/repository"
	"learn-go/src/service"
)

// inventoryFixture menyiapkan stack HTTP nyata (handler -> service -> repository)
// di atas database PostgreSQL sungguhan.
type inventoryFixture struct {
	db     *sql.DB
	router *gin.Engine
	userID string
}

// newInventoryFixture membuka database dari TEST_DATABASE_URL dan membuat user
// uji karena products.created_by dan products_audit_logs.user_id punya FK ke users.
func newInventoryFixture(t *testing.T) *inventoryFixture {
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

	ctx := context.Background()
	var userID string
	// phone_number unik dan maksimal 16 karakter (lihat migrasi users).
	phone := fmt.Sprintf("9%015d", time.Now().UnixNano()%1_000_000_000_000_000)
	err = db.QueryRowContext(ctx, `
		INSERT INTO users (name, phone_number, password)
		VALUES ($1, $2, $3)
		RETURNING id::text`,
		"inventory-integration-test", phone, "password123",
	).Scan(&userID)
	if err != nil {
		db.Close()
		t.Fatalf("insert test user: %v", err)
	}

	// Cleanup berjalan LIFO: produk & audit log dibersihkan lebih dulu
	// (lihat createProduct), baru setelah itu user.
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Logf("cleanup user %s: %v", userID, err)
		}
		db.Close()
	})

	gin.SetMode(gin.TestMode)

	redisAddr := os.Getenv("TEST_REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() { rdb.Close() })

	productRepo := repository.NewProductRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	// Cache bersifat best-effort; Redis yang mati hanya membuat cache miss.
	cacheClient := cache.New(rdb, 100*time.Millisecond)
	productService := service.NewProductService(productRepo, auditRepo, cacheClient)
	productHandler := handler.NewProductHandler(productService)

	router := gin.New()
	router.Use(gin.Recovery())

	// Pengganti middleware.AuthMiddleware: kontraknya sama, yaitu mengisi
	// context key "userId" yang dibaca handler.
	withTestUser := func(c *gin.Context) {
		c.Set("userId", userID)
		c.Next()
	}
	router.PATCH("/products/:id/update-stock", withTestUser, productHandler.Transaction)

	return &inventoryFixture{db: db, router: router, userID: userID}
}

func (f *inventoryFixture) createProduct(t *testing.T, name string, stock int32) int {
	t.Helper()

	var id int
	err := f.db.QueryRow(`
		INSERT INTO products (name, price, stock, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		name, 1000, stock, f.userID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test product: %v", err)
	}

	t.Cleanup(func() {
		// Hapus audit log dulu karena ada FK ke products.
		if _, err := f.db.Exec(`DELETE FROM products_audit_logs WHERE product_id = $1`, id); err != nil {
			t.Logf("cleanup audit logs for product %d: %v", id, err)
		}
		if _, err := f.db.Exec(`DELETE FROM products WHERE id = $1`, id); err != nil {
			t.Logf("cleanup product %d: %v", id, err)
		}
	})

	return id
}

func (f *inventoryFixture) updateStock(t *testing.T, productID int, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(
		http.MethodPatch,
		fmt.Sprintf("/products/%d/update-stock", productID),
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func (f *inventoryFixture) stock(t *testing.T, productID int) int32 {
	t.Helper()

	var stock int32
	if err := f.db.QueryRow(`SELECT stock FROM products WHERE id = $1`, productID).Scan(&stock); err != nil {
		t.Fatalf("read product stock: %v", err)
	}
	return stock
}

func (f *inventoryFixture) auditCount(t *testing.T, productID int) int {
	t.Helper()

	var count int
	if err := f.db.QueryRow(
		`SELECT COUNT(*) FROM products_audit_logs WHERE product_id = $1`, productID,
	).Scan(&count); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	return count
}

// TestInventoryUpdateFlow menguji alur lengkap:
//
//  1. Kirim request inventory update (PATCH /products/:id/update-stock).
//  2. Backend memvalidasi body request.
//  3. Database memperbarui stok.
//  4. Audit log di-insert dalam transaksi yang sama.
//  5. Verifikasi state database yang sebenarnya (bukan hanya response API).
//
// Jalankan dengan database nyata:
//
//	TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5000/learn_go?sslmode=disable' \
//	  go test -tags integration . -run TestInventoryUpdate -v
func TestInventoryUpdateFlow(t *testing.T) {
	f := newInventoryFixture(t)
	ctx := context.Background()

	const initialStock int32 = 100
	const delta int32 = 30
	const wantStock int32 = initialStock - delta

	productID := f.createProduct(t, "integration-inventory-update", initialStock)

	// 1 & 2) Kirim request; handler memvalidasi body (delta wajib != 0).
	res := f.updateStock(t, productID, fmt.Sprintf(`{"delta":%d}`, delta))
	if res.Code != http.StatusOK {
		t.Fatalf("status: want %d, got %d, body=%s", http.StatusOK, res.Code, res.Body.String())
	}

	var response struct {
		Message string `json:"message"`
		Data    struct {
			AuditID int64 `json:"audit_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", res.Body.String(), err)
	}
	if response.Message != "Transaction success" {
		t.Fatalf("message: want %q, got %q", "Transaction success", response.Message)
	}
	if response.Data.AuditID == 0 {
		t.Fatalf("audit_id tidak boleh nol: %s", res.Body.String())
	}

	// 3) Verifikasi stok di database benar-benar berubah.
	if got := f.stock(t, productID); got != wantStock {
		t.Fatalf("stock di database: want %d, got %d", wantStock, got)
	}

	// 4 & 5) Verifikasi audit log yang benar-benar tersimpan di database.
	var (
		auditProductID int
		auditUserID    string
		action         string
		oldStock       int
		newStock       int
	)
	err := f.db.QueryRowContext(ctx, `
		SELECT product_id, user_id::text, action, old_stock, new_stock
		FROM products_audit_logs
		WHERE id = $1`,
		response.Data.AuditID,
	).Scan(&auditProductID, &auditUserID, &action, &oldStock, &newStock)
	if err != nil {
		t.Fatalf("audit log %d tidak ditemukan: %v", response.Data.AuditID, err)
	}

	if auditProductID != productID {
		t.Errorf("audit product_id: want %d, got %d", productID, auditProductID)
	}
	if auditUserID != f.userID {
		t.Errorf("audit user_id: want %s, got %s", f.userID, auditUserID)
	}
	if action != "subtract" {
		t.Errorf("audit action: want %q, got %q", "subtract", action)
	}
	if oldStock != int(initialStock) || newStock != int(wantStock) {
		t.Errorf(
			"audit stock: want old=%d new=%d, got old=%d new=%d",
			initialStock, wantStock, oldStock, newStock,
		)
	}
	if got := f.auditCount(t, productID); got != 1 {
		t.Errorf("jumlah audit log: want 1, got %d", got)
	}
}

// TestInventoryUpdateRejectsInvalidRequest memastikan validasi dan rollback
// transaksi tidak meninggalkan perubahan pada database maupun audit log.
func TestInventoryUpdateRejectsInvalidRequest(t *testing.T) {
	f := newInventoryFixture(t)

	cases := []struct {
		name       string
		stock      int32
		body       string
		wantStatus int
	}{
		{
			name:       "delta nol ditolak validator",
			stock:      100,
			body:       `{"delta":0}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "delta tidak dikirim",
			stock:      100,
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "body bukan JSON valid",
			stock:      100,
			body:       `{`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "stok tidak cukup ditolak service",
			stock:      10,
			body:       `{"delta":11}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			productID := f.createProduct(t, "integration-invalid-"+tc.name, tc.stock)

			res := f.updateStock(t, productID, tc.body)
			if res.Code != tc.wantStatus {
				t.Fatalf("status: want %d, got %d, body=%s", tc.wantStatus, res.Code, res.Body.String())
			}

			// Stok harus tetap seperti semula.
			if got := f.stock(t, productID); got != tc.stock {
				t.Errorf("stock di database berubah: want %d, got %d", tc.stock, got)
			}

			// Tidak boleh ada audit log untuk request yang gagal.
			if got := f.auditCount(t, productID); got != 0 {
				t.Errorf("audit log tidak boleh dibuat saat gagal: got %d", got)
			}
		})
	}
}
