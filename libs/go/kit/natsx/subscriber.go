package natsx

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type MessageHandler func(msg *nats.Msg) error

type CommandHandler func(subject string, data []byte, headers map[string][]string) error

type Subscriber struct {
	js         jetstream.JetStream
	conn       *nats.Conn
	workerPool int
}

func NewSubscriber(c *Client, workerPool int) *Subscriber {
	return &Subscriber{
		js:         c.js,
		conn:       c.conn,
		workerPool: workerPool,
	}
}

type SubscribeOptions struct {
	Durable    string
	QueueGroup string
	Stream     string
}

type SubscribeOption func(*SubscribeOptions)

func WithDurable(name string) SubscribeOption {
	return func(o *SubscribeOptions) {
		o.Durable = name
	}
}

func WithQueueGroup(group string) SubscribeOption {
	return func(o *SubscribeOptions) {
		o.QueueGroup = group
	}
}

func WithStream(stream string) SubscribeOption {
	return func(o *SubscribeOptions) {
		o.Stream = stream
	}
}

func (s *Subscriber) Subscribe(ctx context.Context, subject string, handler MessageHandler, opts ...SubscribeOption) error {
	options := &SubscribeOptions{}
	for _, opt := range opts {
		opt(options)
	}

	consumerCfg := jetstream.ConsumerConfig{
		Durable:        options.Durable,
		DeliverSubject: options.QueueGroup,
		FilterSubject:  subject,
		DeliverPolicy:  jetstream.DeliverAllPolicy,
		AckPolicy:      jetstream.AckExplicitPolicy,
		AckWait:        30 * time.Second,
		MaxDeliver:     3,
	}

	consumer, err := s.js.CreateOrUpdateConsumer(ctx, options.Stream, consumerCfg)
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-msgs.Messages():
			if !ok {
				if err := msgs.Error(); err != nil {
					return fmt.Errorf("consumer error: %w", err)
				}
				return nil
			}
			natsMsg := &nats.Msg{
				Subject: msg.Subject(),
				Data:    msg.Data(),
				Header:  msg.Headers(),
				Reply:   msg.Reply(),
			}
			err := handler(natsMsg)
			if err != nil {
				if nakErr := msg.Nak(); nakErr != nil {
					return fmt.Errorf("nak failed: %w", nakErr)
				}
				continue
			}
			if err := msg.Ack(); err != nil {
				return fmt.Errorf("ack failed: %w", err)
			}
		}
	}
}

func (s *Subscriber) Consume(ctx context.Context, subject string, handler MessageHandler, opts ...SubscribeOption) error {
	options := &SubscribeOptions{}
	for _, opt := range opts {
		opt(options)
	}

	consumerCfg := jetstream.ConsumerConfig{
		Durable:       options.Durable,
		FilterSubject: subject,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    3,
	}

	consumer, err := s.js.CreateOrUpdateConsumer(ctx, options.Stream, consumerCfg)
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	_, err = consumer.Consume(func(msg jetstream.Msg) {
		natsMsg := &nats.Msg{
			Subject: msg.Subject(),
			Data:    msg.Data(),
			Header:  msg.Headers(),
			Reply:   msg.Reply(),
		}
		err := handler(natsMsg)
		if err != nil {
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}

	<-ctx.Done()
	return ctx.Err()
}

func (s *Subscriber) SubscribeCommand(ctx context.Context, subject string, handler CommandHandler, opts ...SubscribeOption) error {
	options := &SubscribeOptions{}
	for _, opt := range opts {
		opt(options)
	}

	consumerCfg := jetstream.ConsumerConfig{
		Durable:        options.Durable,
		DeliverSubject: options.QueueGroup,
		FilterSubject:  subject,
		DeliverPolicy:  jetstream.DeliverAllPolicy,
		AckPolicy:      jetstream.AckExplicitPolicy,
		AckWait:        30 * time.Second,
		MaxDeliver:     3,
	}

	consumer, err := s.js.CreateOrUpdateConsumer(ctx, options.Stream, consumerCfg)
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			msg, err := consumer.Next(jetstream.FetchContext(ctx))
			if err != nil {
				continue
			}
			headers := make(map[string][]string)
			for k, v := range msg.Headers() {
				headers[k] = v
			}
			err = handler(msg.Subject(), msg.Data(), headers)
			if err != nil {
				if nakErr := msg.Nak(); nakErr != nil {
					return fmt.Errorf("nak failed: %w", nakErr)
				}
				continue
			}
			if err := msg.Ack(); err != nil {
				return fmt.Errorf("ack failed: %w", err)
			}
		}
	}
}
