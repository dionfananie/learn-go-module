package handler

import (
	"encoding/json"
	"errors"
	"learn-go/entities"
	"learn-go/service"
	"learn-go/utility"
	"net/http"
)

type ProductHandler struct {
	service *service.ProductService
}

func NewProductHandler(s *service.ProductService) *ProductHandler {
	return &ProductHandler{s}
}

func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	var product entities.Product

	if err := json.NewDecoder(r.Body).Decode(&product); err != nil {
		utility.JSONError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	err := h.service.Create(r.Context(), &product)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProductNameRequired):
			utility.JSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrProductPriceInvalid):
			utility.JSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrProductStockInvalid):
			utility.JSONError(w, http.StatusBadRequest, err.Error())
		default:
			utility.JSONError(w, http.StatusInternalServerError, "Internal Server Error")
		}
		return
	}

	utility.ResponseJson(w, product)

}
