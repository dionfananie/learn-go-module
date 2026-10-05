package repository

import (
	"context"
	"database/sql"
)

type AuditRepository struct {
	db *sql.DB
}

func NewAuditRepository(db *sql.DB) *AuditRepository {
	return &AuditRepository{db}
}

func (r *AuditRepository) Create(ctx context.Context, tx *sql.Tx, productId int, userId string, action string) (int64, error) {
	var auditID int64
	err := tx.QueryRowContext(ctx, "INSERT INTO products_audit_logs (product_id, user_id, action) VALUES($1, $2, $3) RETURNING id", productId, userId, action).Scan(&auditID)
	if err != nil {
		return 0, err
	}
	return auditID, nil
}
