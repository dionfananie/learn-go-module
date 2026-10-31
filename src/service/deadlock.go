package service

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// isDeadlock mendeteksi error deadlock PostgreSQL (SQLSTATE 40P01).
// PostgreSQL membatalkan salah satu transaksi yang saling menunggu lock
// (circular wait) dan melaporkannya lewat error ini.
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}
