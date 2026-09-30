package main

import (
	"fmt"
	database "learn-go/src/database"
	"learn-go/src/handler"
	"learn-go/src/repository"
	"learn-go/src/service"
	"log"
	"net/http"
)

func main() {
	db, err := database.Connect()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	fmt.Println("DB Connected!")

	// create table if not exist
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS products(
        id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
        name TEXT NOT NULL,
        price INTEGER NOT NULL CHECK (price > 0),
        stock INTEGER NOT NULL CHECK (stock >= 0)
    )`)

	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Go running...")
	})

	productRepo := repository.NewProductRepository(db)
	productService := service.NewProductService(productRepo)
	productHandler := handler.NewProductHandler(productService)

	http.HandleFunc("POST /products", productHandler.Create)
	http.HandleFunc("GET /products/{id}", productHandler.GetProduct)
	http.HandleFunc("GET /products", productHandler.GetProductAll)

	fmt.Println("Server running at http://localhost:8080")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
