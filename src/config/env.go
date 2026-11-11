package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

var isEnvLoaded = false

var (
	DB_NAME        = getEnv("DB_NAME", "")
	DB_PORT        = getEnv("DB_PORT", "")
	DB_HOST        = getEnv("DB_HOST", "")
	DB_USERNAME    = getEnv("DB_USER", "")
	DB_PASSWORD    = getEnv("DB_PASSWORD", "")
	DB_PARAMS      = getEnv("DB_PARAMS", "sslmode=disable")
	REDIS_ADDR     = getEnv("REDIS_ADDR", "localhost:6379")
	REDIS_PASSWORD = getEnvOptional("REDIS_PASSWORD")
	CACHE_TIMEOUT  = cacheTimeout()

	JWT_SECRET = getEnv("JWT_SECRET", "")
	JWT_EXP    = getEnv("JWT_EXP", "8h")
)

const defaultCacheTimeoutMs = 100

// cacheTimeout membaca CACHE_TIMEOUT_MS (dalam milidetik). Nilai kosong,
// bukan angka, atau <= 0 akan memakai default 100 ms.
func cacheTimeout() time.Duration {
	ms := getEnvAsInt("CACHE_TIMEOUT_MS", defaultCacheTimeoutMs)
	if ms <= 0 {
		ms = defaultCacheTimeoutMs
	}
	return time.Duration(ms) * time.Millisecond
}

func loadEnv() {
	if isEnvLoaded {
		return
	}
	isEnvLoaded = true

	// `go test ./...` menjalankan tiap package di direktorinya sendiri,
	// jadi `.env` di root repo tidak ditemukan kalau hanya memakai CWD.
	// Coba CWD dulu, lalu naik maksimal 5 level.
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(dir, ".env")
		if _, statErr := os.Stat(candidate); statErr == nil {
			if loadErr := godotenv.Load(candidate); loadErr != nil {
				fmt.Printf("gagal membaca %s: %v\n", candidate, loadErr)
			}
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	fmt.Println(".env file is not found, using env from os...")
}

// getEnvOptional untuk env yang memang boleh kosong,
// contoh: REDIS_PASSWORD saat Redis tidak memakai auth.
func getEnvOptional(name string) string {
	loadEnv()
	return os.Getenv(name)
}

func getEnv(name string, fallback string) string {
	loadEnv()
	if value := os.Getenv(name); value != "" {
		return value
	}

	if fallback != "" {
		return fallback
	}

	panic(fmt.Sprintf(`Environment variable not found :: %v`, name))
}

func getEnvAsInt(name string, fallback int) int {
	loadEnv()
	if value := os.Getenv(name); value != "" {
		intValue, err := strconv.Atoi(value)
		if err != nil {
			log.Fatalf("Error converting %v to int", name)
		}
		return intValue
	}

	return fallback
}
