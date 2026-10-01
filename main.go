package main

import (
	"fmt"
	database "learn-go/src/database"
	"learn-go/src/handler"
	"learn-go/src/repository"
	"learn-go/src/service"
	"log"

	"github.com/gin-gonic/gin"
)

func main() {
	db, err := database.Connect()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	fmt.Println("DB Connected!")

	productRepo := repository.NewProductRepository(db)
	productService := service.NewProductService(productRepo)
	productHandler := handler.NewProductHandler(productService)

	router := gin.Default()
	router.GET("/", func(c *gin.Context) {
		c.String(200, "Running Go")
		return
	})
	router.POST("/products", productHandler.Create)
	router.GET("/products/:id", productHandler.GetProduct)
	router.GET("/products", productHandler.GetProductAll)

	// register user
	userRepo := repository.NewUserRepository(db)
	userService := service.NewUserService(userRepo)
	userHandler := handler.NewUserHandler(userService)
	router.POST("/register", userHandler.Register)
	router.POST("/login", userHandler.Login)
	fmt.Println("Server running at http://localhost:8080")

	if err := router.Run(":8080"); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
