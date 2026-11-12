package queue

import (
	"context"
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"learn-go/src/message_broker/events"
)

// Publisher mengirim event ke RabbitMQ (implementasi events.Publisher).
type Publisher struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewPublisher membuka koneksi + channel, lalu mendeklarasikan exchange.
func NewPublisher(url string) (*Publisher, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}

	err = ch.ExchangeDeclare(
		events.ExchangeStock, // name
		"topic",              // type
		true,                 // durable
		false,                // auto-deleted
		false,                // internal
		false,                // no-wait
		nil,                  // args
	)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("declare exchange: %w", err)
	}

	return &Publisher{conn: conn, ch: ch}, nil
}

// PublishStockChanged mengirim event stok berubah.
func (p *Publisher) PublishStockChanged(ctx context.Context, ev events.StockChanged) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return p.ch.PublishWithContext(ctx,
		events.ExchangeStock,          // exchange
		events.RoutingKeyStockChanged, // routing key
		false,                         // mandatory
		false,                         // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // pesan selamat dari restart broker
			Body:         body,
			Timestamp:    ev.At,
		},
	)
}

// Close menutup channel lalu koneksi.
func (p *Publisher) Close() error {
	if p.ch != nil {
		_ = p.ch.Close()
	}
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}
