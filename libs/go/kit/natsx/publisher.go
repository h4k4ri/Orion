package natsx

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type PublishOption func(*PublishOptions)

type PublishOptions struct {
	MsgID   string
	Timeout time.Duration
}

func WithMsgID(msgID string) PublishOption {
	return func(o *PublishOptions) {
		o.MsgID = msgID
	}
}

func WithTimeout(timeout time.Duration) PublishOption {
	return func(o *PublishOptions) {
		o.Timeout = timeout
	}
}

type Publisher struct {
	js   jetstream.JetStream
	conn *nats.Conn
}

func NewPublisher(c *Client) *Publisher {
	return &Publisher{
		js:   c.js,
		conn: c.conn,
	}
}

func (p *Publisher) Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	options := &PublishOptions{Timeout: 10 * time.Second}
	for _, opt := range opts {
		opt(options)
	}
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}

	msg := &nats.Msg{Subject: subject, Data: data, Header: InjectContext(ctx, nil)}
	_, err := p.js.PublishMsg(ctx, msg, jetstream.WithMsgID(options.MsgID))
	if err != nil {
		return fmt.Errorf("jetstream publish %s: %w", subject, err)
	}

	return nil
}

func (p *Publisher) PublishWithHeaders(ctx context.Context, subject string, data []byte, headers nats.Header, opts ...PublishOption) error {
	options := &PublishOptions{Timeout: 10 * time.Second}
	for _, opt := range opts {
		opt(options)
	}
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}

	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  InjectContext(ctx, headers),
	}

	_, err := p.js.PublishMsg(ctx, msg, jetstream.WithMsgID(options.MsgID))
	if err != nil {
		return fmt.Errorf("jetstream publish with headers %s: %w", subject, err)
	}

	return nil
}
