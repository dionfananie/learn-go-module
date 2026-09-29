package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	entities "learn-go/Entities"
	utility "learn-go/Utils"
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

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("server stopped: %v", err)
	}

}

func createProduct(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var product entities.Product
	err := json.NewDecoder(r.Body).Decode(&product)

	if err != nil {
		utility.JSONError(w, http.StatusBadRequest, "JSON invalid, check again")

		return
	}
	if product.Name == "" {
		utility.JSONError(w, http.StatusBadRequest, "Name must be filled")
		return
	}
	if product.Price <= 0 {
		utility.JSONError(w, http.StatusBadRequest, "Price must be filled")
		return
	}
	if product.Stock <= 0 {
		utility.JSONError(w, http.StatusBadRequest, "Stock must be filled")
		return
	}
	result, err := db.Exec(
		"INSERT INTO products (name, price, stock) VALUES (?, ?, ?)", product.Name, product.Price, product.Stock,
	)
	if err != nil {
		utility.JSONError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	product.ID, err = result.LastInsertId()
	if err != nil {
		utility.JSONError(w, http.StatusInternalServerError, "Internal Server Error")
		log.Printf("Success storing to DB but error when read ID: %v", err)

		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(product)
}
