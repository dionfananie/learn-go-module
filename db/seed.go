package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand"

	database "learn-go/src/database"
	"learn-go/src/entities"
	"learn-go/src/helpers/password"
	"learn-go/src/repository"
)

const seedCount = 250

// Semua produk hasil seed dimiliki user ini supaya kolom created_by (FK users.id)
// terisi, sehingga produk seed bisa ikut di-delete/diuji lewat API.
const (
	seedUserName     = "seed_user"
	seedUserPhone    = "080000000001"
	seedUserPassword = "SeedUser12345" // boleh dipakai login utk cek manual
)

var (
	productNames = []string{
		"Kopi Arabika", "Teh Hijau", "Gula Aren", "Susu UHT", "Minyak Goreng",
		"Beras Premium", "Mie Instan", "Sabun Mandi", "Sikat Gigi", "Air Mineral",
		"Kecap Manis", "Saus Sambal", "Tepung Terigu", "Telur Ayam", "Ikan Kaleng",
		"Kopi Instan", "Coklat Batang", "Biskuit", "Keripik Kentang", "Permen Karet",
		"Deterjen Bubuk", "Pewangi Pakaian", "Shampo", "Pasta Gigi", "Tisu Wajah",
	}
	variants = []string{
		"250g", "500g", "1kg", "2kg", "5kg", "1L", "2L", "600ml", "330ml", "Sachet",
		"Kemasan Kecil", "Kemasan Besar", "Refill", "Botol", "Pouch", "Kaleng",
	}
)

func randomProduct(r *rand.Rand) entities.Product {
	name := fmt.Sprintf("%s %s", productNames[r.Intn(len(productNames))], variants[r.Intn(len(variants))])
	price := float64((r.Intn(200) + 1) * 500) // kelipatan 500, antara 500 - 100000
	stock := int32(r.Intn(500))               // 0 - 499

	return entities.Product{
		Name:  name,
		Price: price,
		Stock: stock,
	}
}

// ensureSeedUser mengembalikan id user pemilik produk seed.
// Kalau belum ada, dibuat dulu — aman dipanggil berulang kali (idempotent).
func ensureSeedUser(ctx context.Context, db *sql.DB, userRepo *repository.UsersRepository) (string, error) {
	var id string
	err := db.QueryRowContext(ctx, "SELECT id FROM users WHERE name = $1", seedUserName).Scan(&id)
	if err == nil {
		return id, nil // sudah ada, pakai ulang
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	hashed, err := password.Hash(seedUserPassword)
	if err != nil {
		return "", err
	}
	resp, err := userRepo.RegisterUser(ctx, &entities.User{
		Name:        seedUserName,
		PhoneNumber: seedUserPhone,
		Password:    hashed,
	})
	if err != nil {
		return "", err
	}
	log.Printf("seed user dibuat: %s", seedUserName)
	return resp.ID, nil
}

func main() {
	db, err := database.Connect()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	repo := repository.NewProductRepository(db)
	userRepo := repository.NewUserRepository(db)
	ctx := context.Background()
	r := rand.New(rand.NewSource(250)) // seed tetap, biar hasil reproducible

	ownerID, err := ensureSeedUser(ctx, db, userRepo)
	if err != nil {
		log.Fatalf("gagal menyiapkan seed user: %v", err)
	}

	var success, failed int

	for i := 0; i < seedCount; i++ {
		product := randomProduct(r)
		if err := repo.Create(ctx, &product, ownerID); err != nil {
			log.Printf("gagal seed produk #%d (%s): %v", i+1, product.Name, err)
			failed++
			continue
		}
		success++
	}

	fmt.Printf("Seeding selesai. Berhasil: %d, Gagal: %d (pemilik: %s)\n", success, failed, seedUserName)
}
