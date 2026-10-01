package handler

import (
	"errors"
	"fmt"
	"learn-go/src/entities"
	"learn-go/src/service"
	"learn-go/src/utility"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type UserHandler struct {
	service *service.UserService
}

func NewUserHandler(s *service.UserService) *UserHandler {
	return &UserHandler{s}
}
func (u *UserHandler) Register(c *gin.Context) {
	var user entities.User

	if err := c.ShouldBindJSON(&user); err != nil {
		fmt.Printf("Error &w", err.Error())
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, gin.H{"error": utility.BindMessage(ve)})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Request Body is invalid JSON"})
		return
	}
	accessToken, err := u.service.Register(c.Request.Context(), &user)
	if err != nil {
		fmt.Printf("Error &w", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "User succesfully registered",
		"data": gin.H{
			"user_id":      user.ID,
			"name":         user.Name,
			"created_at":   user.CreatedAt,
			"access_token": accessToken,
		},
	})

}
