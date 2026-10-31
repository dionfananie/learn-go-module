package database

import (
	"context"
	"learn-go/src/config"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConnectRedis selalu mengembalikan client (tidak pernah nil) walaupun Ping
// gagal, supaya pemanggil bisa tetap jalan dengan cache yang dianggap miss.
func ConnectRedis() (*redis.Client, error) {

	rdb := redis.NewClient(&redis.Options{
		Addr:                  config.REDIS_ADDR,
		Password:              config.REDIS_PASSWORD,
		DB:                    0,
		MaxRetries:            -1,   // -1 = retry command dimatikan (0 justru berarti 3)
		ContextTimeoutEnabled: true, // ctx request benar-benar membatasi read/write

		DialTimeout:  500 * time.Millisecond,
		ReadTimeout:  config.CACHE_TIMEOUT,
		WriteTimeout: config.CACHE_TIMEOUT,
		PoolTimeout:  500 * time.Millisecond,

		// Default-nya 5 kali percobaan dial (~400ms backoff + timeout per attempt).
		DialerRetries: 1, // satu percobaan saja, gagal ya gagal
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return rdb, err
	}

	return rdb, nil
}
