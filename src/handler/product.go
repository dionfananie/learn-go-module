package handler

import (
	"encoding/json"
	"errors"
	"learn-go/src/entities"
	"learn-go/src/service"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ProductHandler struct {
	service *service.ProductService
}

func NewProductHandler(s *service.ProductService) *ProductHandler {
	return &ProductHandler{s}
}

func (h *ProductHandler) Create(c *gin.Context) {
	var product entities.Product

	if err := json.NewDecoder(c.Request.Body).Decode(&product); err != nil {
		c.JSON(http.StatusBadRequest, "Invalid JSON")
		return
	}
	ctx := c.Request.Context()
	err := h.service.Create(ctx, &product)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProductNameRequired):
		case errors.Is(err, service.ErrProductPriceInvalid):
		case errors.Is(err, service.ErrProductStockInvalid):
			c.JSON(http.StatusBadRequest, err.Error())
		default:
			c.JSON(http.StatusInternalServerError, "Internal Server Error")
			return
		}
	}

	c.JSON(http.StatusCreated, product)

}

func (h *ProductHandler) GetProductAll(c *gin.Context) {
	product, err := h.service.GetProductAll(c.Request.Context())
	if err != nil {
		log.Print(err.Error())
		c.JSON(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	c.JSON(http.StatusOK, product)

}

func (h *ProductHandler) GetProduct(c *gin.Context) {
	product, err := h.service.GetProduct(c.Request.Context(), c.Param("id"))
	if err != nil {
		log.Print(err.Error())

		switch {
		case errors.Is(err, service.ErrProductNotFound):
			c.JSON(http.StatusNotFound, "Product Not found")
		default:
			c.JSON(http.StatusInternalServerError, "Internal Server Error")

		}
		return
	}
	c.JSON(http.StatusOK, product)

}
