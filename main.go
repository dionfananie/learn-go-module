package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	entities "learn-go/Entities"
	"log"
	"net/http"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "./app.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("DB Connected!")

	// create table if not exist
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS products(
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        price INTEGER NOT NULL,
        stock INTEGER NOT NULL
    )`)

	if err != nil {
		log.Fatal(err)
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Go running...")
	})
	http.HandleFunc("POST /product", func(w http.ResponseWriter, r *http.Request) {
		createProduct(db, w, r)
	})

	fmt.Println("Server running at http://localhost:8080")
	http.ListenAndServe(":8080", nil)

}

func createProduct(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var product entities.Product
	err := json.NewDecoder(r.Body).Decode(&product)

	if err != nil {
		http.Error(w, "JSON invalid, check again", http.StatusBadRequest)
		return
	}
	if product.Name == "" {
		http.Error(w, "Name must be filled", http.StatusBadRequest)
		return
	}
	if product.Price <= 0 {
		http.Error(w, "Price must be filled", http.StatusBadRequest)
		return
	}
	result, err := db.Exec(
		"INSERT INTO products (name, price, stock) VALUES (?, ?, ?)", product.Name, product.Price, product.Stock,
	)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	product.ID, err = result.LastInsertId()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Success storing to DB but error when read ID: %v", err)

		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(product)
}
