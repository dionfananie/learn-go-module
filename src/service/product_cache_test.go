package service

import (
	"testing"
	"time"
)

// Key cache dipakai untuk invalidasi (DELETE/SET) setelah operasi tulis.
// Kalau formatnya berubah tanpa disadari, cache akan basi, jadi dikunci di test.
func TestProductCacheKey(t *testing.T) {
	if got, want := productCacheKey(42), "productId:42"; got != want {
		t.Fatalf("productCacheKey(42) = %q, mau %q", got, want)
	}
}

func TestProductCacheTTLWajar(t *testing.T) {
	if productCacheTTL <= 0 {
		t.Fatalf("TTL cache harus positif, dapat %v", productCacheTTL)
	}
	if productCacheTTL > time.Hour {
		t.Fatalf("TTL cache terlalu lama untuk data stok: %v", productCacheTTL)
	}
}
