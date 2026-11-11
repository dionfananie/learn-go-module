package service

import (
	"context"
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"learn-go/src/cache"
)

// TestMain membuang output log cache. Timer AfterFunc dari invalidateCache
// kadang menyala setelah test selesai (client sudah ditutup) dan menulis
// "Redis failed: client is closed" — itu noise, bukan kegagalan.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

// newTestCache menjalankan Redis in-memory supaya test invalidasi tidak
// butuh container Redis.
func newTestCache(t *testing.T) (*cache.Client, *miniredis.Miniredis) {
	t.Helper()

	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{
		Addr:                  srv.Addr(),
		MaxRetries:            -1,
		ContextTimeoutEnabled: true,
		ReadTimeout:           time.Second,
		WriteTimeout:          time.Second,
	})
	t.Cleanup(func() { _ = rdb.Close() })

	return cache.New(rdb, 100*time.Millisecond), srv
}

// waitGone menunggu sampai key hilang atau timeout. Dipakai untuk menunggu
// delete kedua yang berjalan async (AfterFunc), supaya tidak flaky.
func waitGone(t *testing.T, srv *miniredis.Miniredis, key string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !srv.Exists(key) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("key %q seharusnya terhapus (delete kedua tidak jalan)", key)
}

func TestInvalidateCacheDeletesImmediately(t *testing.T) {
	c, srv := newTestCache(t)
	s := &ProductService{cache: c}

	srv.Set("productId:1", `{"id":1,"stock":5}`)
	s.invalidateCache(context.Background(), "productId:1")

	if srv.Exists("productId:1") {
		t.Fatal("delete pertama harus sinkron: key langsung hilang")
	}
}

func TestInvalidateCacheDeletesAgainAfterStaleWrite(t *testing.T) {
	c, srv := newTestCache(t)
	s := &ProductService{cache: c}
	ctx := context.Background()

	// Simulasi race: key ada (nilai lama), lalu invalidate.
	srv.Set("productId:1", `{"id":1,"stock":5}`)
	s.invalidateCache(ctx, "productId:1")
	if srv.Exists("productId:1") {
		t.Fatal("delete pertama gagal")
	}

	// Reader yang sempat membaca nilai lama menulisnya KEMBALI setelah delete.
	// Inilah race yang ditutup oleh delete kedua.
	srv.Set("productId:1", `{"id":1,"stock":5}`)

	waitGone(t, srv, "productId:1")
}

func TestInvalidateCacheCopiesKeys(t *testing.T) {
	c, srv := newTestCache(t)
	s := &ProductService{cache: c}

	srv.Set("productId:1", `{"id":1}`)
	srv.Set("productId:999", `{"id":999}`) // key lain, tidak boleh ikut terhapus

	keys := []string{"productId:1"}
	s.invalidateCache(context.Background(), keys...)

	// Mutasi slice PEMANGGIL setelah invalidateCache kembali: delete kedua
	// harus tetap memakai salinan key asli, bukan slice yang di-mutasi.
	keys[0] = "productId:999"

	waitGone(t, srv, "productId:1")
	if !srv.Exists("productId:999") {
		t.Fatal("delete kedua salah menarget key yang di-mutasi; clone tidak dipakai")
	}
}

func TestInvalidateCacheNoKeys(t *testing.T) {
	c, _ := newTestCache(t)
	s := &ProductService{cache: c}

	// Tidak boleh panic saat dipanggil tanpa key.
	s.invalidateCache(context.Background())
}
