package worker

import (
	"context"
	"testing"

	"learn-go/src/message_broker/events"
)

// Handler harus selalu return nil (error => Nack => DLQ). Test ini mengunci
// kontrak itu untuk semua action yang mungkin.
func TestStockChangedHandlerAlwaysReturnsNil(t *testing.T) {
	actions := []string{"subtract", "add", "transfer_out", "transfer_in"}
	for _, action := range actions {
		ev := events.StockChanged{ProductID: 1, Action: action, OldStock: 10, NewStock: 5}
		if err := StockChangedHandler(context.Background(), ev); err != nil {
			t.Fatalf("action %q: handler mengembalikan error %v", action, err)
		}
	}
}
