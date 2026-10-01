package handler

import (
	"errors"
	"learn-go/src/entities"
	"learn-go/src/service"
	"net/http"

	"github.com/gin-gonic/gin"
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := u.service.Register(c.Request.Context(), &user)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUserNameRequired),
			errors.Is(err, service.ErrUserPasswordRequired):
			c.JSON(http.StatusBadRequest, err.Error())
		default:
			c.JSON(http.StatusInternalServerError, err.Error())
		}
		return
	}
	c.JSON(http.StatusCreated, user)
}
