package main

import (
	"context"
	"errors"
	"fmt"
	"learn-go/src/cache"
	"learn-go/src/config"
	database "learn-go/src/database"
	"learn-go/src/handler"
	"learn-go/src/message_broker/queue"
	"learn-go/src/message_broker/worker"
	"learn-go/src/middleware"
	"learn-go/src/repository"
	"learn-go/src/service"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	publisher, err := queue.NewPublisher(config.RABBITMQ_URL)
	if err != nil {
		log.Fatalf("RabbitMQ unavailable: %v", err)
	}
	defer publisher.Close()
	fmt.Println("RabbitMQ Connected!")

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
	productService := service.NewProductService(productRepo, auditRepo, cacheClient, publisher)
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

	// Worker async: consume event stok berubah di goroutine sendiri.
	consumer := queue.NewConsumer(config.RABBITMQ_URL, worker.StockChangedHandler)
	workerCtx, stopWorker := context.WithCancel(context.Background())
	go func() {
		if err := consumer.Run(workerCtx); err != nil {
			log.Printf("[worker] berhenti: %v", err)
		}
	}()

	addr := os.Getenv("APP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: router}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server stopped: %v", err)
		}
	}()
	fmt.Printf("Server running at http://localhost%s\n", addr)

	// Graceful shutdown: tangkap SIGINT/SIGTERM, hentikan HTTP lalu worker.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	fmt.Println("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}
	stopWorker() // batalkan consumer.Run
}
