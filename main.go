package main

import (
	"fmt"
	"learn-go/src/cache"
	"learn-go/src/config"
	database "learn-go/src/database"
	"learn-go/src/handler"
	"learn-go/src/middleware"
	"learn-go/src/repository"
	"learn-go/src/service"
	"log"
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	db, err := database.Connect()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	fmt.Println("DB Connected!")
	rdb, err := database.ConnectRedis()
	if err != nil {
		log.Printf("Redis is unavailable, check it later: %v", err)
	}
	defer rdb.Close()
	fmt.Println("Cache Connected!")
	router := gin.Default()
	router.GET("/", func(c *gin.Context) {
		c.String(200, "Running Go")
		return
	})

	// register user
	userRepo := repository.NewUserRepository(db)
	userService := service.NewUserService(userRepo)
	userHandler := handler.NewUserHandler(userService)
	router.POST("/register", userHandler.Register)
	router.POST("/login", userHandler.Login)

	productRepo := repository.NewProductRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	cacheClient := cache.New(rdb, config.CACHE_TIMEOUT)
	productService := service.NewProductService(productRepo, auditRepo, cacheClient)
	productHandler := handler.NewProductHandler(productService)

	router.GET("/products/:id", productHandler.GetProduct)
	router.GET("/products", productHandler.GetProductAll)
	router.GET("/products/:id/audit-logs", productHandler.GetAuditLogs)

	protected := router.Group("/")
	protected.Use(middleware.AuthMiddleware())

	protected.POST("/products", productHandler.Create)
	protected.DELETE("/products/:id", productHandler.DeleteProduct)
	protected.PATCH("/products/:id/update-stock", productHandler.Transaction)
	protected.POST("/products/transfer-stock", productHandler.TransferStock)

	addr := os.Getenv("APP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	fmt.Printf("Server running at http://localhost%s\n", addr)

	if err := router.Run(addr); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
