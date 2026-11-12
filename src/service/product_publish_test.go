package service

import (
	"context"
	"testing"

	"learn-go/src/message_broker/events"
)

// fakePublisher merekam event untuk test tanpa RabbitMQ.
type fakePublisher struct {
	events []events.StockChanged
}

func (f *fakePublisher) PublishStockChanged(ctx context.Context, ev events.StockChanged) error {
	f.events = append(f.events, ev)
	return nil
}

func TestPublishStockChanged(t *testing.T) {
	f := &fakePublisher{}
	s := &ProductService{publisher: f}

	ev := events.StockChanged{ProductID: 1, Action: "subtract", OldStock: 10, NewStock: 9}
	s.publishStockChanged(ev)

	if len(f.events) != 1 {
		t.Fatalf("ingin 1 event terkirim, dapat %d", len(f.events))
	}
	if f.events[0].ProductID != 1 {
		t.Fatalf("ProductID = %d, ingin 1", f.events[0].ProductID)
	}
}

func TestPublishStockChangedNilPublisherNoPanic(t *testing.T) {
	s := &ProductService{publisher: nil}
	s.publishStockChanged(events.StockChanged{ProductID: 1})
}
