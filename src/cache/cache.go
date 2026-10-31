package cache

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// failureCooldown adalah lama Redis dilewati setelah satu operasi gagal.
// Tujuannya agar Redis yang sedang down tidak membuat setiap request
// membayar timeout cache.
const failureCooldown = 5 * time.Second

// Client membungkus Redis untuk kebutuhan cache. Sifatnya best-effort:
// setiap operasi punya batas waktu sendiri dan tidak pernah mengembalikan
// error ke pemanggil, sehingga Redis mati hanya berarti cache miss.
type Client struct {
	rdb     *redis.Client
	timeout time.Duration

	// skipUntil (unix nano) > sekarang berarti Redis sedang dilewati.
	// 0 artinya tidak ada cooldown aktif.
	skipUntil atomic.Int64
}

func New(rdb *redis.Client, timeout time.Duration) *Client {
	return &Client{rdb: rdb, timeout: timeout}
}

// skipping melaporkan apakah Redis sedang dalam masa cooldown.
func (c *Client) skipping() bool {
	return time.Now().UnixNano() < c.skipUntil.Load()
}

// markFailure mengaktifkan cooldown setelah Redis gagal. Log hanya ditulis
// saat cooldown baru dimulai supaya tidak spam selama Redis down.
func (c *Client) markFailure(err error) {
	now := time.Now()
	if now.UnixNano() >= c.skipUntil.Load() {
		log.Printf("[cache] Redis failed (%v), skipping cache for %s", err, failureCooldown)
	}
	c.skipUntil.Store(now.Add(failureCooldown).UnixNano())
}

// GetJSON mengisi dst dari cache dan mengembalikan true jika hit.
func (c *Client) GetJSON(ctx context.Context, key string, dst any) bool {
	if c.skipping() {
		return false
	}

	cctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	raw, err := c.rdb.Get(cctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			log.Printf("[cache] MISS %s", key)
		} else {
			// Redis bermasalah: anggap miss agar request tetap jalan.
			c.markFailure(err)
		}
		return false
	}

	if err := json.Unmarshal(raw, dst); err != nil {
		log.Printf("[cache] HIT %s error invalid JSON: %v", key, err)
		return false
	}

	log.Printf("[cache] HIT %s", key)
	return true
}

// SetJSON menyimpan val sebagai JSON dengan TTL tertentu.
func (c *Client) SetJSON(ctx context.Context, key string, val any, ttl time.Duration) {
	if c.skipping() {
		return
	}

	raw, err := json.Marshal(val)
	if err != nil {
		log.Printf("[cache] marshal failed %s: %v", key, err)
		return
	}

	cctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if err := c.rdb.Set(cctx, key, raw, ttl).Err(); err != nil {
		c.markFailure(err)
	}
}

// Delete menghapus satu atau beberapa key. Panggil setelah operasi tulis
// berhasil (khususnya setelah tx.Commit).
func (c *Client) Delete(ctx context.Context, keys ...string) {
	if len(keys) == 0 || c.skipping() {
		return
	}

	cctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if err := c.rdb.Del(cctx, keys...).Err(); err != nil {
		c.markFailure(err)
	}
}
