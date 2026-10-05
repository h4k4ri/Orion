package natsx

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Client struct {
	conn           *nats.Conn
	js             jetstream.JetStream
	url            string
	maxRetries     int
	initialBackoff time.Duration
	user           string
	pass           string
}

type ClientOption func(*Client)

func WithRetry(maxRetries int, initialBackoff time.Duration) ClientOption {
	return func(c *Client) {
		c.maxRetries = maxRetries
		c.initialBackoff = initialBackoff
	}
}

func WithAuth(user, pass string) ClientOption {
	return func(c *Client) {
		c.user = user
		c.pass = pass
	}
}

func New(url string, opts ...ClientOption) (*Client, error) {
	c := &Client{url: url, maxRetries: -1, initialBackoff: 2 * time.Second}
	for _, opt := range opts {
		opt(c)
	}

	connectOpts := []nats.Option{
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(c.maxRetries),
		nats.ReconnectWait(c.initialBackoff),
		nats.Timeout(5 * time.Second),
	}
	if c.user != "" {
		connectOpts = append(connectOpts, nats.UserInfo(c.user, c.pass))
	}
	conn, err := nats.Connect(url, connectOpts...)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("jetstream new: %w", err)
	}

	c.conn = conn
	c.js = js
	return c, nil
}

func (c *Client) Close() {
	if c.conn != nil && !c.conn.IsClosed() {
		c.conn.Close()
	}
}

func (c *Client) Conn() *nats.Conn {
	return c.conn
}

func (c *Client) JS() jetstream.JetStream {
	return c.js
}

func (c *Client) Publish(ctx context.Context, subject string, data []byte, msgID string) error {
	_, err := c.js.Publish(ctx, subject, data, jetstream.WithMsgID(msgID))
	if err != nil {
		return fmt.Errorf("jetstream publish %s: %w", subject, err)
	}

	return nil
}
