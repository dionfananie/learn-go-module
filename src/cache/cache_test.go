package cache

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type sample struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestMain(m *testing.M) {
	// Cache menulis log HIT/MISS/ERROR; matikan agar output test tetap bersih.
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

// newTestClient menjalankan Redis in-memory (miniredis) supaya unit test
// tidak butuh container Redis.
func newTestClient(t *testing.T, timeout time.Duration) (*Client, *miniredis.Miniredis) {
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

	return New(rdb, timeout), srv
}

func TestGetJSONHit(t *testing.T) {
	c, srv := newTestClient(t, 100*time.Millisecond)
	srv.Set("productId:1", `{"id":1,"name":"kopi"}`)

	var got sample
	if !c.GetJSON(context.Background(), "productId:1", &got) {
		t.Fatal("harus hit")
	}
	if got.ID != 1 || got.Name != "kopi" {
		t.Fatalf("data tidak sesuai: %+v", got)
	}
}

func TestGetJSONMiss(t *testing.T) {
	c, _ := newTestClient(t, 100*time.Millisecond)

	var got sample
	if c.GetJSON(context.Background(), "productId:404", &got) {
		t.Fatal("key tidak ada, harus miss")
	}
	// Miss bukan kegagalan Redis: cooldown tidak boleh aktif.
	if c.skipping() {
		t.Fatal("cache miss tidak boleh memicu cooldown")
	}
}

func TestGetJSONInvalidJSON(t *testing.T) {
	c, srv := newTestClient(t, 100*time.Millisecond)
	srv.Set("productId:1", "{bukan json")

	var got sample
	if c.GetJSON(context.Background(), "productId:1", &got) {
		t.Fatal("JSON rusak harus dianggap miss")
	}
	if c.skipping() {
		t.Fatal("JSON rusak bukan kegagalan Redis, cooldown tidak boleh aktif")
	}
}

func TestSetJSONMenyimpanDenganTTL(t *testing.T) {
	c, srv := newTestClient(t, 100*time.Millisecond)
	ctx := context.Background()

	c.SetJSON(ctx, "productId:7", sample{ID: 7, Name: "teh"}, 2*time.Minute)

	raw, err := srv.Get("productId:7")
	if err != nil {
		t.Fatalf("key harus tersimpan: %v", err)
	}
	var got sample
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("nilai bukan JSON valid: %v", err)
	}
	if got.ID != 7 || got.Name != "teh" {
		t.Fatalf("data tidak sesuai: %+v", got)
	}

	ttl := srv.TTL("productId:7")
	if ttl <= 0 || ttl > 2*time.Minute {
		t.Fatalf("TTL tidak sesuai: %v", ttl)
	}
}

func TestSetJSONGagalMarshal(t *testing.T) {
	c, srv := newTestClient(t, 100*time.Millisecond)

	// channel tidak bisa di-marshal ke JSON
	c.SetJSON(context.Background(), "productId:9", make(chan int), time.Minute)

	if srv.Exists("productId:9") {
		t.Fatal("nilai yang gagal di-marshal tidak boleh tersimpan")
	}
	if c.skipping() {
		t.Fatal("gagal marshal bukan kegagalan Redis")
	}
}

func TestDelete(t *testing.T) {
	c, srv := newTestClient(t, 100*time.Millisecond)
	ctx := context.Background()

	srv.Set("productId:1", `{"id":1}`)
	srv.Set("productId:2", `{"id":2}`)

	c.Delete(ctx, "productId:1", "productId:2")

	if srv.Exists("productId:1") || srv.Exists("productId:2") {
		t.Fatal("kedua key harus terhapus")
	}

	// Tanpa key: tidak boleh panic / menyentuh Redis.
	c.Delete(ctx)
}

func TestErrorMemicuCooldown(t *testing.T) {
	c, srv := newTestClient(t, 100*time.Millisecond)
	ctx := context.Background()

	// Simulasikan Redis bermasalah (mis. LOADING / down).
	srv.SetError("boom")

	var got sample
	if c.GetJSON(ctx, "productId:1", &got) {
		t.Fatal("Redis error harus dianggap miss")
	}
	if !c.skipping() {
		t.Fatal("error Redis harus memulai cooldown")
	}

	// Redis "pulih", tapi selama cooldown tetap dilewati.
	srv.SetError("")
	srv.Set("productId:1", `{"id":1,"name":"kopi"}`)

	if c.GetJSON(ctx, "productId:1", &got) {
		t.Fatal("selama cooldown cache tidak boleh dibaca")
	}

	// Setelah cooldown habis, cache dipakai lagi tanpa restart.
	c.skipUntil.Store(time.Now().Add(-time.Millisecond).UnixNano())
	if !c.GetJSON(ctx, "productId:1", &got) {
		t.Fatal("setelah cooldown harus hit lagi")
	}
	if got.Name != "kopi" {
		t.Fatalf("data tidak sesuai: %+v", got)
	}
}

func TestCooldownJugaMelewatiSetDanDelete(t *testing.T) {
	c, srv := newTestClient(t, 100*time.Millisecond)
	ctx := context.Background()

	srv.Set("productId:1", `{"id":1}`)
	c.skipUntil.Store(time.Now().Add(time.Hour).UnixNano())

	c.SetJSON(ctx, "productId:2", sample{ID: 2}, time.Minute)
	c.Delete(ctx, "productId:1")

	if srv.Exists("productId:2") {
		t.Fatal("SET harus dilewati selama cooldown")
	}
	if !srv.Exists("productId:1") {
		t.Fatal("DELETE harus dilewati selama cooldown")
	}
}

func TestContextDibatalkanTidakMemicuCooldown(t *testing.T) {
	c, _ := newTestClient(t, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var got sample
	if c.GetJSON(ctx, "productId:1", &got) {
		t.Fatal("context batal harus dianggap miss")
	}
	// Client putus bukan salah Redis; request lain jangan ikut melewati cache.
	if c.skipping() {
		t.Fatal("context batal tidak boleh memicu cooldown")
	}
}

func TestGetJSONMenghormatiTimeout(t *testing.T) {
	// Server yang menerima koneksi tapi tidak pernah menjawab:
	// mensimulasikan Redis yang hang (bukan connection refused).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				time.Sleep(2 * time.Second)
			}(conn)
		}
	}()

	rdb := redis.NewClient(&redis.Options{
		Addr:                  ln.Addr().String(),
		DialTimeout:           time.Second,
		ReadTimeout:           5 * time.Second, // sengaja besar: ctx yang harus membatasi
		WriteTimeout:          5 * time.Second,
		MaxRetries:            -1,
		ContextTimeoutEnabled: true,
	})
	t.Cleanup(func() { _ = rdb.Close() })

	c := New(rdb, 100*time.Millisecond)

	start := time.Now()
	var got sample
	if c.GetJSON(context.Background(), "productId:1", &got) {
		t.Fatal("Redis hang harus dianggap miss")
	}
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("timeout cache tidak dihormati, butuh %s", elapsed)
	}
	if elapsed < 50*time.Millisecond {
		t.Fatalf("terlalu cepat (%s); sepertinya timeout tidak teruji", elapsed)
	}
	// Timeout Redis (bukan cancel dari client) harus memicu cooldown.
	if !c.skipping() {
		t.Fatal("timeout Redis harus memicu cooldown")
	}
}

func TestConcurrentAccessAman(t *testing.T) {
	c, srv := newTestClient(t, 100*time.Millisecond)
	ctx := context.Background()
	srv.Set("productId:1", `{"id":1,"name":"kopi"}`)

	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				var got sample
				c.GetJSON(ctx, "productId:1", &got)
				c.SetJSON(ctx, "productId:2", sample{ID: i}, time.Minute)
				c.Delete(ctx, "productId:2")
				c.skipping()
			}
		}(i)
	}
	wg.Wait()

	// Jalankan dengan `go test -race` untuk memastikan tidak ada data race
	// pada skipUntil.
}
