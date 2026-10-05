package driver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/horizon/orion/plugins/messaging/queue"
)

type NATSDriver struct {
	conn            *nats.Conn
	js              nats.JetStreamContext
	mu              sync.RWMutex
	subs            map[string]*nats.Subscription
	handlers        map[string]queue.MessageHandler
	consumers       map[string]queue.Consumer
	consumerStreams map[string]string
	cancels         map[string]context.CancelFunc
}

func NewNATSDriver(cfg Config) (Driver, error) {
	url := cfg.NATSURL
	if url == "" {
		url = nats.DefaultURL
	}

	conn, err := nats.Connect(url,
		nats.Name("orion-messaging"),
		nats.Timeout(10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	return &NATSDriver{
		conn:            conn,
		js:              js,
		subs:            make(map[string]*nats.Subscription),
		handlers:        make(map[string]queue.MessageHandler),
		consumers:       make(map[string]queue.Consumer),
		consumerStreams: make(map[string]string),
		cancels:         make(map[string]context.CancelFunc),
	}, nil
}

func (d *NATSDriver) Publish(ctx context.Context, subject string, msg *queue.Message) error {
	var data []byte
	if msg.Data != nil {
		data = msg.Data
	}

	natsMsg := &nats.Msg{Subject: subject, Data: data, Header: make(nats.Header)}
	for k, v := range msg.Headers {
		natsMsg.Header.Set(k, v)
	}
	if msg.ID != "" {
		natsMsg.Header.Set(nats.MsgIdHdr, msg.ID)
	}
	_, err := d.js.PublishMsg(natsMsg, nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("failed to publish: %w", err)
	}

	return nil
}

func (d *NATSDriver) Subscribe(ctx context.Context, subscriptionID, subject, consumer string, handler queue.MessageHandler) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if subscriptionID == "" {
		return fmt.Errorf("subscription id is required")
	}
	var options []nats.SubOpt
	if consumer != "" {
		options = append(options, nats.Durable(consumer), nats.ManualAck())
	}
	sub, err := d.js.Subscribe(subject, func(msg *nats.Msg) {
		m := &queue.Message{
			ID:        msg.Header.Get(nats.MsgIdHdr),
			Subject:   msg.Subject,
			Data:      msg.Data,
			Headers:   marshalHeader(msg.Header),
			Timestamp: time.Now(),
		}

		if err := handler(m); err != nil {
			msg.Nak()
		} else {
			msg.Ack()
		}
	}, options...)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	d.subs[subscriptionID] = sub
	d.handlers[subscriptionID] = handler
	d.cancels[subscriptionID] = func() {}

	return nil
}

func (d *NATSDriver) Unsubscribe(ctx context.Context, subscriptionID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	sub, ok := d.subs[subscriptionID]
	if !ok {
		return fmt.Errorf("subscription not found: %s", subscriptionID)
	}

	sub.Unsubscribe()
	delete(d.subs, subscriptionID)
	delete(d.handlers, subscriptionID)
	if cancel := d.cancels[subscriptionID]; cancel != nil {
		cancel()
	}
	delete(d.cancels, subscriptionID)

	return nil
}

func (d *NATSDriver) CreateQueue(ctx context.Context, q *queue.Queue) error {
	cfg := &nats.StreamConfig{
		Name:      q.Name,
		Subjects:  []string{q.Name},
		Retention: nats.LimitsPolicy,
		Storage:   nats.FileStorage,
	}

	if q.Kind == queue.QueueKindStream {
		cfg.Retention = nats.InterestPolicy
	}

	_, err := d.js.AddStream(cfg)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}

	return nil
}

func (d *NATSDriver) GetQueue(ctx context.Context, name string) (*queue.Queue, error) {
	si, err := d.js.StreamInfo(name)
	if err != nil {
		return nil, fmt.Errorf("stream not found: %w", err)
	}

	return &queue.Queue{
		Name:      si.Config.Name,
		Durable:   qDurable(si.Config.Retention),
		Messages:  int64(si.State.Msgs),
		BytesUsed: int64(si.State.Bytes),
		CreatedAt: si.Created,
	}, nil
}

func (d *NATSDriver) DeleteQueue(ctx context.Context, name string) error {
	err := d.js.DeleteStream(name)
	if err != nil {
		return fmt.Errorf("failed to delete stream: %w", err)
	}
	return nil
}

func (d *NATSDriver) ListQueues(ctx context.Context) ([]*queue.Queue, error) {
	names := d.js.StreamNames()
	queues := make([]*queue.Queue, 0)
	for name := range names {
		q, err := d.GetQueue(ctx, name)
		if err != nil {
			continue
		}
		queues = append(queues, q)
	}

	return queues, nil
}

func qDurable(retention nats.RetentionPolicy) bool {
	return retention == nats.LimitsPolicy || retention == nats.InterestPolicy
}

func (d *NATSDriver) CreateConsumer(ctx context.Context, c *queue.Consumer) error {
	if c.ID == "" {
		c.ID = c.Name
	}
	cfg := &nats.ConsumerConfig{
		Name:          c.ID,
		Durable:       c.ID,
		AckPolicy:     nats.AckExplicitPolicy,
		FilterSubject: c.QueueName,
	}

	_, err := d.js.AddConsumer(c.QueueName, cfg)
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}
	d.mu.Lock()
	d.consumers[c.ID] = *c
	d.consumerStreams[c.ID] = c.QueueName
	d.mu.Unlock()

	return nil
}

func (d *NATSDriver) GetConsumer(ctx context.Context, id string) (*queue.Consumer, error) {
	d.mu.RLock()
	c, ok := d.consumers[id]
	stream := d.consumerStreams[id]
	d.mu.RUnlock()
	if ok {
		return &c, nil
	}
	if stream == "" {
		return nil, fmt.Errorf("consumer not found: %s", id)
	}
	info, err := d.js.ConsumerInfo(stream, id)
	if err != nil {
		return nil, fmt.Errorf("consumer not found: %s: %w", id, err)
	}
	return &queue.Consumer{ID: id, Name: info.Name, QueueName: stream, PendingCount: int(info.NumPending), AckCount: int(info.AckFloor.Consumer), CreatedAt: info.Created}, nil
}

func (d *NATSDriver) DeleteConsumer(ctx context.Context, id string) error {
	d.mu.Lock()
	stream := d.consumerStreams[id]
	delete(d.consumers, id)
	delete(d.consumerStreams, id)
	d.mu.Unlock()
	if stream == "" {
		return fmt.Errorf("consumer not found: %s", id)
	}
	if err := d.js.DeleteConsumer(stream, id); err != nil {
		return fmt.Errorf("failed to delete consumer: %w", err)
	}
	return nil
}

func (d *NATSDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, sub := range d.subs {
		sub.Unsubscribe()
	}
	d.subs = nil
	d.handlers = nil
	d.cancels = nil

	if d.conn != nil {
		d.conn.Close()
	}

	return nil
}

func parseQueueKind(kind string) nats.StorageType {
	switch kind {
	case "memory":
		return nats.MemoryStorage
	default:
		return nats.FileStorage
	}
}

func marshalHeader(h nats.Header) map[string]string {
	result := make(map[string]string)
	for k, v := range h {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	return result
}
