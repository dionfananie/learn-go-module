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
	http.HandleFunc("POST /products", func(w http.ResponseWriter, r *http.Request) {
		createProduct(db, w, r)
	})
	http.HandleFunc("GET /products/{id}", func(w http.ResponseWriter, r *http.Request) {
		getProduct(db, w, r)
	})
	http.HandleFunc("GET /products", func(w http.ResponseWriter, r *http.Request) {
		getProductAll(db, w, r)
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
	if product.Stock < 0 {
		utility.JSONError(w, http.StatusBadRequest, "Stock can't be negative")
		return
	}
	result, err := db.Exec(
		"INSERT INTO products (name, price, stock) VALUES (?, ?, ?)", product.Name, product.Price, product.Stock,
	)
	if err != nil {
		log.Printf("Insert product failed %v", err)
		utility.JSONError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	product.ID, err = result.LastInsertId()
	if err != nil {
		utility.JSONError(w, http.StatusInternalServerError, "Internal Server Error")
		log.Printf("Success storing to DB but error when read ID: %v", err)

		return
	}
	utility.ResponseJson(w, product)
}

func getProduct(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var product entities.Product

	id := r.PathValue("id")
	err := db.QueryRow(
		"SELECT id, name, price, stock FROM products WHERE id = ?", id).Scan(&product.ID, &product.Name, &product.Price, &product.Stock)

	if err == sql.ErrNoRows {
		log.Printf("Error fetching product %v", err)
		utility.JSONError(w, http.StatusNotFound, "Product Not Found")
		return
	}

	if err != nil {
		log.Printf("Error fetching product %v", err)
		utility.JSONError(w, http.StatusInternalServerError, "Product Not Found")
		return
	}

	utility.ResponseJson(w, product)

}

func getProductAll(db *sql.DB, w http.ResponseWriter, r *http.Request) {

	rows, err := db.Query(
		"SELECT id, name, price, stock FROM products ORDER BY id")
	if err != nil {
		log.Printf("Error fetching all products %v", err)
		utility.JSONError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	defer rows.Close()

	products := make([]entities.Product, 0)

	for rows.Next() {
		var product entities.Product
		if err := rows.Scan(
			&product.ID,
			&product.Name,
			&product.Price,
			&product.Stock,
		); err != nil {
			log.Printf("Error Read Product %v", err)
			utility.JSONError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Error Iterate Product %v", err)
		utility.JSONError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	utility.ResponseJson(w, products)
}
