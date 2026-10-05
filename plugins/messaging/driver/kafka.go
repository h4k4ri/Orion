package driver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/horizon/orion/plugins/messaging/queue"
)

type KafkaDriver struct {
	brokers   []string
	conn      *kafka.Conn
	writers   map[string]*kafka.Writer
	readers   map[string]*kafka.Reader
	consumers map[string]queue.Consumer
	cancels   map[string]context.CancelFunc
	mu        sync.RWMutex
}

func NewKafkaDriver(cfg Config) (Driver, error) {
	if len(cfg.KafkaBrokers) == 0 {
		return nil, fmt.Errorf("Kafka brokers required")
	}

	conn, err := kafka.Dial("tcp", cfg.KafkaBrokers[0])
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Kafka: %w", err)
	}

	return &KafkaDriver{
		brokers:   cfg.KafkaBrokers,
		conn:      conn,
		writers:   make(map[string]*kafka.Writer),
		readers:   make(map[string]*kafka.Reader),
		consumers: make(map[string]queue.Consumer),
		cancels:   make(map[string]context.CancelFunc),
	}, nil
}

func (d *KafkaDriver) Publish(ctx context.Context, topic string, msg *queue.Message) error {
	d.mu.Lock()
	writer, ok := d.writers[topic]
	if !ok {
		writer = &kafka.Writer{
			Addr:         kafka.TCP(d.brokers...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: kafka.RequireOne,
		}
		d.writers[topic] = writer
	}
	d.mu.Unlock()

	kafkaMsg := kafka.Message{
		Key:   []byte(msg.ID),
		Value: msg.Data,
		Time:  msg.Timestamp,
	}
	if len(kafkaMsg.Key) == 0 {
		kafkaMsg.Key = []byte(msg.Subject)
	}
	if kafkaMsg.Time.IsZero() {
		kafkaMsg.Time = time.Now().UTC()
	}

	if msg.Headers != nil {
		kafkaMsg.Headers = make([]kafka.Header, 0, len(msg.Headers))
		for k, v := range msg.Headers {
			kafkaMsg.Headers = append(kafkaMsg.Headers, kafka.Header{Key: k, Value: []byte(v)})
		}
	}

	err := writer.WriteMessages(ctx, kafkaMsg)
	if err != nil {
		return fmt.Errorf("failed to publish: %w", err)
	}

	return nil
}

func (d *KafkaDriver) Subscribe(ctx context.Context, subscriptionID, topic, consumer string, handler queue.MessageHandler) error {
	if subscriptionID == "" {
		return fmt.Errorf("subscription id is required")
	}
	if consumer == "" {
		consumer = subscriptionID
	}
	readCtx, cancel := context.WithCancel(context.Background())
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  d.brokers,
		Topic:    topic,
		GroupID:  consumer,
		MinBytes: 1,
		MaxBytes: 10e6,
	})

	d.mu.Lock()
	d.readers[subscriptionID] = reader
	d.cancels[subscriptionID] = cancel
	d.mu.Unlock()

	go func() {
		for {
			msg, err := reader.ReadMessage(readCtx)
			if err != nil {
				break
			}

			headers := make(map[string]string)
			for _, h := range msg.Headers {
				headers[h.Key] = string(h.Value)
			}

			qmsg := &queue.Message{
				ID:        string(msg.Key),
				Subject:   topic,
				Data:      msg.Value,
				Headers:   headers,
				Timestamp: msg.Time,
			}

			if err := handler(qmsg); err != nil {
				// NACK - message will be redelivered
				continue
			}
		}
	}()

	return nil
}

func (d *KafkaDriver) Unsubscribe(ctx context.Context, subscriptionID string) error {
	d.mu.Lock()
	reader, ok := d.readers[subscriptionID]
	if !ok {
		d.mu.Unlock()
		return fmt.Errorf("reader not found: %s", subscriptionID)
	}
	cancel := d.cancels[subscriptionID]
	delete(d.readers, subscriptionID)
	delete(d.cancels, subscriptionID)
	d.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	reader.Close()

	return nil
}

func (d *KafkaDriver) CreateQueue(ctx context.Context, q *queue.Queue) error {
	topic := &kafka.TopicConfig{
		Topic:             q.Name,
		NumPartitions:     3,
		ReplicationFactor: 1,
	}

	err := d.conn.CreateTopics(*topic)
	if err != nil {
		return fmt.Errorf("failed to create topic: %w", err)
	}

	return nil
}

func (d *KafkaDriver) GetQueue(ctx context.Context, name string) (*queue.Queue, error) {
	partitions, err := d.conn.ReadPartitions(name)
	if err != nil {
		return nil, fmt.Errorf("topic not found: %w", err)
	}

	return &queue.Queue{
		Name:      name,
		Kind:      queue.QueueKindTopic,
		Consumers: len(partitions),
		Messages:  0,
	}, nil
}

func (d *KafkaDriver) DeleteQueue(ctx context.Context, name string) error {
	err := d.conn.DeleteTopics(name)
	if err != nil {
		return fmt.Errorf("failed to delete topic: %w", err)
	}
	return nil
}

func (d *KafkaDriver) ListQueues(ctx context.Context) ([]*queue.Queue, error) {
	controller, err := d.conn.Controller()
	if err != nil {
		return nil, fmt.Errorf("failed to get controller: %w", err)
	}

	conn, err := kafka.Dial("tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to controller: %w", err)
	}
	defer conn.Close()

	partitions, err := conn.ReadPartitions()
	if err != nil {
		return nil, fmt.Errorf("failed to read topics: %w", err)
	}

	queues := make([]*queue.Queue, 0)
	seen := make(map[string]struct{})
	for _, t := range partitions {
		if _, ok := seen[t.Topic]; ok {
			continue
		}
		seen[t.Topic] = struct{}{}
		queues = append(queues, &queue.Queue{
			Name: t.Topic,
			Kind: queue.QueueKindTopic,
		})
	}

	return queues, nil
}

func (d *KafkaDriver) CreateConsumer(ctx context.Context, c *queue.Consumer) error {
	if c.ID == "" {
		c.ID = c.Name
	}
	if c.Name == "" {
		c.Name = c.ID
	}
	d.mu.Lock()
	d.consumers[c.ID] = *c
	d.mu.Unlock()
	return nil
}

func (d *KafkaDriver) GetConsumer(ctx context.Context, id string) (*queue.Consumer, error) {
	d.mu.RLock()
	c, ok := d.consumers[id]
	d.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("consumer not found: %s", id)
	}
	return &c, nil
}

func (d *KafkaDriver) DeleteConsumer(ctx context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.consumers[id]; !ok {
		return fmt.Errorf("consumer not found: %s", id)
	}
	delete(d.consumers, id)
	return nil
}

func (d *KafkaDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, w := range d.writers {
		w.Close()
	}
	for _, r := range d.readers {
		r.Close()
	}
	for _, cancel := range d.cancels {
		cancel()
	}
	if d.conn != nil {
		d.conn.Close()
	}
	return nil
}
