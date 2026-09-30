package main

import (
	"fmt"
	database "learn-go/src/database"
	"learn-go/src/handler"
	"learn-go/src/repository"
	"learn-go/src/service"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
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
	router := gin.Default()
	router.POST("/products", productHandler.Create)
	router.GET("/products/:id", productHandler.GetProduct)
	router.GET("/products", productHandler.GetProductAll)

	fmt.Println("Server running at http://localhost:8080")

	if err := router.Run(":8080"); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
