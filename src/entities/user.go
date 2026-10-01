package entities

import "time"

type User struct {
	ID          string    `json:"id"`
	Name        string    `json:"name" binding:"required,min=3,max=30"`
	PhoneNumber string    `json:"phone_number" binding:"required,min=10,max=16"`
	Password    string    `json:"password" binding:"required,min=8,max=20"`
	CreatedAt   time.Time `json:"createdAt"`
}

type UserResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	AccessToken string    `json:"access_token"`
	CreatedAt   time.Time `json:"created_at"`
}
