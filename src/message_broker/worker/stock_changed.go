package worker

import (
	"context"
	"log"

	"learn-go/src/message_broker/events"
)

// StockChangedHandler adalah proses async yang dikerjakan worker.
// Dipanggil untuk setiap event stok berubah yang dikonsumsi dari queue.
// Penting: harus idempoten (bisa dipanggil ulang) karena RabbitMQ
// menjamin at-least-once delivery.
func StockChangedHandler(ctx context.Context, ev events.StockChanged) error {
	switch ev.Action {
	case "subtract", "transfer_out":
		if ev.NewStock == 0 {
			log.Printf("[worker] WARNING: produk %d HABIS", ev.ProductID)
		} else if ev.NewStock < 10 {
			log.Printf("[worker] peringatan: produk %d stok menipis (%d)", ev.ProductID, ev.NewStock)
		}
	}

	log.Printf("[worker] memproses stock_changed: product=%d action=%s %d -> %d",
		ev.ProductID, ev.Action, ev.OldStock, ev.NewStock)
	return nil
}
