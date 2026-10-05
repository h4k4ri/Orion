package driver

import (
	"context"

	"github.com/horizon/orion/plugins/messaging/queue"
)

type Driver interface {
	Publish(ctx context.Context, subject string, msg *queue.Message) error
	Subscribe(ctx context.Context, subscriptionID, subject, consumer string, handler queue.MessageHandler) error
	Unsubscribe(ctx context.Context, subscriptionID string) error

	CreateQueue(ctx context.Context, q *queue.Queue) error
	GetQueue(ctx context.Context, name string) (*queue.Queue, error)
	DeleteQueue(ctx context.Context, name string) error
	ListQueues(ctx context.Context) ([]*queue.Queue, error)

	CreateConsumer(ctx context.Context, c *queue.Consumer) error
	GetConsumer(ctx context.Context, id string) (*queue.Consumer, error)
	DeleteConsumer(ctx context.Context, id string) error

	Close() error
}

type Config struct {
	URL          string
	NATSURL      string
	KafkaBrokers []string
}
