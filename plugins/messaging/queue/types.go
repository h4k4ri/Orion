package queue

import "time"

type Message struct {
	ID        string            `json:"id"`
	Subject   string            `json:"subject"`
	Data      []byte            `json:"data,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	ExpiresAt *time.Time        `json:"expiresAt,omitempty"`
}

type Queue struct {
	Name       string    `json:"name"`
	Kind       QueueKind `json:"kind"`
	Durable    bool      `json:"durable"`
	AutoDelete bool      `json:"autoDelete"`
	Consumers  int       `json:"consumers"`
	Messages   int64     `json:"messages"`
	BytesUsed  int64     `json:"bytesUsed"`
	CreatedAt  time.Time `json:"createdAt"`
}

type QueueKind string

const (
	QueueKindStream QueueKind = "stream"
	QueueKindQueue  QueueKind = "queue"
	QueueKindTopic  QueueKind = "topic"
)

type Consumer struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	QueueName    string     `json:"queueName"`
	PendingCount int        `json:"pendingCount"`
	AckCount     int        `json:"ackCount"`
	Redelivered  int        `json:"redelivered"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastActivity *time.Time `json:"lastActivity,omitempty"`
}

type Subscription struct {
	ID        string         `json:"id"`
	Consumer  string         `json:"consumer"`
	Subject   string         `json:"subject"`
	QueueName string         `json:"queueName,omitempty"`
	Handler   MessageHandler `json:"-"`
	Messages  chan *Message  `json:"-"`
}

type MessageHandler func(msg *Message) error

type StreamConfig struct {
	Name      string        `json:"name"`
	Subjects  []string      `json:"subjects"`
	Retention string        `json:"retention"`
	MaxBytes  int64         `json:"maxBytes"`
	MaxAge    time.Duration `json:"maxAge"`
	Storage   string        `json:"storage"`
	Replicas  int           `json:"replicas"`
}

type KafkaTopic struct {
	Name              string            `json:"name"`
	Partitions        int               `json:"partitions"`
	ReplicationFactor int               `json:"replicationFactor"`
	RetentionBytes    int64             `json:"retentionBytes"`
	RetentionMs       int64             `json:"retentionMs"`
	Config            map[string]string `json:"config,omitempty"`
}
