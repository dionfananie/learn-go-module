package events

import (
	"context"
	"time"
)

const (
	ExchangeStock          = "stock.events"
	RoutingKeyStockChanged = "stock.changed"
	QueueStockChanged      = "stock.changed.worker"

	ExchangeStockDLX     = "stock.events.dlx"
	QueueStockChangedDLQ = "stock.events.dlq"
)

// StockChanged dipublish setiap kali stok produk berubah (Transaction,
// TransferStock). Worker async memakainya untuk proses lanjutan
// (notifikasi, sinkronisasi read-model, dsb).
type StockChanged struct {
	ProductID int       `json:"product_id"`
	UserID    string    `json:"user_id"`
	Action    string    `json:"action"` // subtract | add | transfer_out | transfer_in
	OldStock  int       `json:"old_stock"`
	NewStock  int       `json:"new_stock"`
	AuditID   int64     `json:"audit_id,omitempty"`
	At        time.Time `json:"at"`
}

type Publisher interface {
	PublishStockChanged(ctx context.Context, event StockChanged) error
}
