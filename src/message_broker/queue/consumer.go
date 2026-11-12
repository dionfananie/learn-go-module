package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"learn-go/src/message_broker/events"
)

// Handler memproses satu event. Mengembalikan error bila gagal (akan di-Nack).
type Handler func(ctx context.Context, ev events.StockChanged) error

// Consumer membaca event dari queue dan meneruskannya ke handler.
type Consumer struct {
	url     string
	handler Handler
}

func NewConsumer(url string, handler Handler) *Consumer {
	return &Consumer{url: url, handler: handler}
}

// Run memblokir selama ctx aktif. Kalau koneksi putus, ia reconnect otomatis.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		err := c.consume(ctx)
		if ctx.Err() != nil {
			return nil // shutdown normal
		}
		log.Printf("[queue] konsumen terputus: %v; reconnect 3 detik", err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}
}

func (c *Consumer) consume(ctx context.Context) error {
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	defer ch.Close()

	if err := c.declare(ch); err != nil {
		return err
	}

	// Qos=1: satu pesan per worker dalam proses, menjaga urutan. Naikkan
	// nilainya untuk paralelisme (handler harus aman konkuren).
	if err := ch.Qos(1, 0, false); err != nil {
		return fmt.Errorf("qos: %w", err)
	}

	msgs, err := ch.Consume(
		events.QueueStockChanged, // queue
		"",                       // consumer tag (biar broker yang tentukan)
		false,                    // auto-ack: kita ack manual setelah proses
		false,                    // exclusive
		false,                    // no-local
		false,                    // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-msgs:
			if !ok {
				return fmt.Errorf("channel konsumen tertutup")
			}
			c.process(d)
		}
	}
}

// declare membuat exchange, queue, binding, dan dead-letter queue.
func (c *Consumer) declare(ch *amqp.Channel) error {
	for _, name := range []string{events.ExchangeStock, events.ExchangeStockDLX} {
		if err := ch.ExchangeDeclare(name, "topic", true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare exchange %s: %w", name, err)
		}
	}

	// Queue kerja: pesan yang gagal (Nack requeue=false) dialihkan ke DLX.
	_, err := ch.QueueDeclare(
		events.QueueStockChanged,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		amqp.Table{
			"x-dead-letter-exchange":    events.ExchangeStockDLX,
			"x-dead-letter-routing-key": events.RoutingKeyStockChanged,
		},
	)
	if err != nil {
		return fmt.Errorf("declare queue: %w", err)
	}
	if err := ch.QueueBind(events.QueueStockChanged, events.RoutingKeyStockChanged, events.ExchangeStock, false, nil); err != nil {
		return fmt.Errorf("bind queue: %w", err)
	}

	// Dead-letter queue: tempat pesan gagal untuk inspeksi/debug.
	if _, err := ch.QueueDeclare(events.QueueStockChangedDLQ, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlq: %w", err)
	}
	if err := ch.QueueBind(events.QueueStockChangedDLQ, events.RoutingKeyStockChanged, events.ExchangeStockDLX, false, nil); err != nil {
		return fmt.Errorf("bind dlq: %w", err)
	}
	return nil
}

func (c *Consumer) process(d amqp.Delivery) {
	var ev events.StockChanged
	if err := json.Unmarshal(d.Body, &ev); err != nil {
		// Payload rusak permanen → tolak (masuk DLQ).
		log.Printf("[queue] payload invalid, tolak: %v", err)
		_ = d.Nack(false, false)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.handler(ctx, ev); err != nil {
		log.Printf("[queue] handler gagal (product %d): %v; masuk DLQ", ev.ProductID, err)
		_ = d.Nack(false, false) // requeue=false → DLQ, hindari loop tanpa henti
		return
	}

	_ = d.Ack(false)
}
