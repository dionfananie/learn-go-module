package repository

import (
	"context"
	"database/sql"

	"learn-go/src/entities"
)

type AuditRepository struct {
	db *sql.DB
}

func NewAuditRepository(db *sql.DB) *AuditRepository {
	return &AuditRepository{db}
}
func (r *AuditRepository) Create(ctx context.Context, tx *sql.Tx, productId int, userId string, action string, oldStock int, newStock int) (int64, error) {
	var auditID int64
	err := tx.QueryRowContext(ctx,
		"INSERT INTO products_audit_logs (product_id, user_id, action, old_stock, new_stock) VALUES($1, $2, $3, $4, $5) RETURNING id",
		productId, userId, action, oldStock, newStock).Scan(&auditID)
	if err != nil {
		return 0, err
	}
	return auditID, nil
}

// GetByProductID mengambil riwayat perubahan stok suatu produk, terbaru dulu.
// Query ini adalah pemakai composite index (product_id, created_at):
// kolom pertama (product_id) dipakai di WHERE, kolom kedua (created_at) dipakai di ORDER BY.
func (r *AuditRepository) GetByProductID(ctx context.Context, productId int) ([]entities.AuditLog, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, product_id, user_id, action, old_stock, new_stock, created_at
		FROM products_audit_logs
		WHERE product_id = $1
		ORDER BY created_at DESC`, productId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]entities.AuditLog, 0)
	for rows.Next() {
		var l entities.AuditLog
		if err := rows.Scan(&l.ID, &l.ProductID, &l.UserID, &l.Action, &l.OldStock, &l.NewStock, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}
