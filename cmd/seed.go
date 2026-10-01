package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"

	database "learn-go/src/database"
	"learn-go/src/entities"
	"learn-go/src/repository"
)

const seedCount = 250

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

func main() {
	db, err := database.Connect()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	repo := repository.NewProductRepository(db)
	ctx := context.Background()
	r := rand.New(rand.NewSource(42)) // seed tetap, biar hasil reproducible

	var success, failed int

	for i := 0; i < seedCount; i++ {
		product := randomProduct(r)
		if err := repo.Create(ctx, &product); err != nil {
			log.Printf("gagal seed produk #%d (%s): %v", i+1, product.Name, err)
			failed++
			continue
		}
		success++
	}

	fmt.Printf("Seeding selesai. Berhasil: %d, Gagal: %d\n", success, failed)
}
