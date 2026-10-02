package handler

import (
	"context"
	"errors"
	"fmt"
	"learn-go/src/entities"
	"learn-go/src/service"
	"learn-go/src/utility"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type ProductHandler struct {
	service *service.ProductService
}

func NewProductHandler(s *service.ProductService) *ProductHandler {
	return &ProductHandler{s}
}

func (h *ProductHandler) Create(c *gin.Context) {
	var product entities.Product

	if err := c.ShouldBindJSON(&product); err != nil {
		fmt.Printf("[Error]- Create Product %v\n", err.Error())
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, gin.H{"error": utility.BindMessage(ve)})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Request Body is invalid JSON"})
		return
	}
	ctx := c.Request.Context()
	userId := c.GetString("userId")
	err := h.service.Create(ctx, &product, userId)
	if err != nil {
		fmt.Printf("[Error]- Create Product %v\n", err.Error())
		switch {
		case errors.Is(err, service.ErrProductNameRequired),
			errors.Is(err, service.ErrProductPriceInvalid),
			errors.Is(err, service.ErrProductStockInvalid):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})

		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": product})

}

func (h *ProductHandler) GetProductAll(c *gin.Context) {
	product, err := h.service.GetProductAll(c.Request.Context())
	if err != nil {
		log.Print(err.Error())
		fmt.Printf("[Error]- Get Product All %v\n", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})

		return
	}
	c.JSON(http.StatusOK, gin.H{"data": product})

}

func (h *ProductHandler) GetProduct(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id"})
		return
	}
	product, err := h.service.GetProduct(c.Request.Context(), id)
	if err != nil {
		log.Print(err.Error())
		fmt.Printf("[Error]- Get Product  %v\n", err.Error())
		switch {
		case errors.Is(err, service.ErrProductNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})

		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})

		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": product})
}

func (h *ProductHandler) DeleteProduct(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		fmt.Printf("[Error]- Delete Product  %v\n", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)

	userId := c.GetString("userId")
	err = h.service.DeleteProduct(c.Request.Context(), id, userId)
	if err != nil {
		fmt.Printf("[Error]- Delete Product  %v\n", err.Error())
		switch {
		case errors.Is(err, service.ErrProductNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})

		case errors.Is(err, service.ErrProductNotAuthorized):
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})

		case errors.Is(err, context.DeadlineExceeded):
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "Database operation timed out"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})

		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Product successfully deleted", "data": id})

}
