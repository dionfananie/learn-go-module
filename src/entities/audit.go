package entities

import "time"

type AuditLog struct {
	ID        int64     `json:"id"`
	ProductID int       `json:"product_id"`
	UserID    string    `json:"user_id"`
	Action    string    `json:"action"`
	OldStock  int       `json:"old_stock"`
	NewStock  int       `json:"new_stock"`
	CreatedAt time.Time `json:"created_at"`
}
