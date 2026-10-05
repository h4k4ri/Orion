package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/horizon/orion/plugins/messaging/driver"
	"github.com/horizon/orion/plugins/messaging/queue"
	"github.com/horizon/orion/sdk/go/plugin"
)

var (
	_drv       driver.Driver
	_subs      = sync.Map{}
	_queues    = sync.Map{}
	_consumers = sync.Map{}
)

func main() {
	backend := os.Getenv("ORION_MESSAGING_BACKEND")
	if backend == "" {
		backend = "nats"
	}

	var err error
	_drv, err = driver.NewDriver(backend, driver.Config{
		NATSURL:      os.Getenv("NATS_URL"),
		KafkaBrokers: getEnvList("KAFKA_BROKERS"),
	})
	if err != nil {
		log.Fatalf("Failed to create messaging driver: %v", err)
	}

	p := plugin.New(plugin.Config{ID: "messaging-plugin", Name: "Orion Messaging", Version: "1.0.0", Vendor: "Orion"})
	p.Resource("orion.io/messaging.queue", "v1").
		Handle("publish", handlePublish).
		Handle("subscribe", handleSubscribe).
		Handle("receive", handleReceive).
		Handle("unsubscribe", handleUnsubscribe).
		Handle("create_queue", handleCreateQueue).
		Handle("get_queue", handleGetQueue).
		Handle("delete_queue", handleDeleteQueue).
		Handle("list_queues", handleListQueues).
		Handle("create_consumer", handleCreateConsumer).
		Handle("get_consumer", handleGetConsumer).
		Handle("delete_consumer", handleDeleteConsumer).
		Handle("list_consumers", handleListConsumers).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50085"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer _drv.Close()
	if err := p.Serve(ctx, endpoint); err != nil {
		log.Fatalf("messaging plugin stopped: %v", err)
	}
}

func handlePublish(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Subject string            `json:"subject"`
		Data    []byte            `json:"data,omitempty"`
		Headers map[string]string `json:"headers,omitempty"`
		ID      string            `json:"id,omitempty"`
	}

	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	if input.Subject == "" {
		return errorResponse(fmt.Errorf("subject is required")), nil
	}
	if input.ID == "" {
		input.ID = uuid.New().String()
	}

	msg := &queue.Message{
		ID:      input.ID,
		Subject: input.Subject,
		Data:    input.Data,
		Headers: input.Headers,
	}

	if err := _drv.Publish(ctx, input.Subject, msg); err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"status": "published"}),
	}, nil
}

func handleSubscribe(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Subject   string `json:"subject"`
		Consumer  string `json:"consumer"`
		QueueName string `json:"queueName,omitempty"`
	}

	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	if input.Subject == "" {
		return errorResponse(fmt.Errorf("subject is required")), nil
	}

	subID := uuid.New().String()
	delivery := make(chan *queue.Message, 256)
	consumerID := input.Consumer
	_consumers.Range(func(key, value interface{}) bool {
		consumer := value.(*queue.Consumer)
		if consumer.ID == input.Consumer || consumer.Name == input.Consumer {
			consumerID = consumer.ID
			return false
		}
		return true
	})

	handler := func(msg *queue.Message) error {
		select {
		case delivery <- msg:
			return nil
		default:
			return fmt.Errorf("subscription buffer is full")
		}
	}

	if err := _drv.Subscribe(context.Background(), subID, input.Subject, consumerID, handler); err != nil {
		return errorResponse(err), nil
	}

	_sub := &queue.Subscription{
		ID:        subID,
		Consumer:  consumerID,
		Subject:   input.Subject,
		QueueName: input.QueueName,
		Handler:   handler,
		Messages:  delivery,
	}
	_subs.Store(subID, _sub)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"subscriptionId": subID}),
	}, nil
}

func handleUnsubscribe(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	subID := getString(input, "subscriptionId")
	if subID == "" {
		return errorResponse(fmt.Errorf("subscriptionId is required")), nil
	}

	if _, ok := _subs.Load(subID); !ok {
		return errorResponse(fmt.Errorf("subscription not found: %s", subID)), nil
	}
	if err := _drv.Unsubscribe(ctx, subID); err != nil {
		return errorResponse(err), nil
	}

	_subs.Delete(subID)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"status": "unsubscribed"}),
	}, nil
}

func handleReceive(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		SubscriptionID string `json:"subscriptionId"`
		Max            int    `json:"max"`
		TimeoutMS      int    `json:"timeoutMs"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.SubscriptionID == "" {
		return errorResponse(fmt.Errorf("subscriptionId is required")), nil
	}
	if input.Max <= 0 || input.Max > 1000 {
		input.Max = 1
	}
	if input.TimeoutMS < 0 || input.TimeoutMS > 60000 {
		return errorResponse(fmt.Errorf("timeoutMs must be between 0 and 60000")), nil
	}
	value, ok := _subs.Load(input.SubscriptionID)
	if !ok {
		return errorResponse(fmt.Errorf("subscription not found: %s", input.SubscriptionID)), nil
	}
	sub := value.(*queue.Subscription)
	messages := make([]*queue.Message, 0, input.Max)
	if input.TimeoutMS > 0 {
		timer := time.NewTimer(time.Duration(input.TimeoutMS) * time.Millisecond)
		defer timer.Stop()
		select {
		case msg := <-sub.Messages:
			messages = append(messages, msg)
		case <-timer.C:
		}
	} else {
		select {
		case msg := <-sub.Messages:
			messages = append(messages, msg)
		default:
		}
	}
	for len(messages) < input.Max {
		select {
		case msg := <-sub.Messages:
			messages = append(messages, msg)
		default:
			return &plugin.ResourceResponse{Success: true, Result: mustMarshal(map[string]interface{}{"messages": messages, "total": len(messages)})}, nil
		}
	}
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(map[string]interface{}{"messages": messages, "total": len(messages)})}, nil
}

func handleCreateQueue(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name       string `json:"name"`
		Kind       string `json:"kind"`
		Durable    bool   `json:"durable"`
		AutoDelete bool   `json:"autoDelete"`
	}

	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	if input.Name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	if input.Kind == "" {
		input.Kind = "stream"
	}

	q := &queue.Queue{
		Name:       input.Name,
		Kind:       queue.QueueKind(input.Kind),
		Durable:    input.Durable,
		AutoDelete: input.AutoDelete,
	}

	if err := _drv.CreateQueue(ctx, q); err != nil {
		return errorResponse(err), nil
	}

	_queues.Store(q.Name, q)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(q),
	}, nil
}

func handleGetQueue(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name := getString(input, "name")
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	q, err := _drv.GetQueue(ctx, name)
	if err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(q),
	}, nil
}

func handleDeleteQueue(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name := getString(input, "name")
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	if err := _drv.DeleteQueue(ctx, name); err != nil {
		return errorResponse(err), nil
	}

	_queues.Delete(name)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"status": "deleted"}),
	}, nil
}

func handleListQueues(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	queues, err := _drv.ListQueues(ctx)
	if err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"items": queues, "total": len(queues)}),
	}, nil
}

func handleCreateConsumer(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name      string `json:"name"`
		QueueName string `json:"queueName"`
	}

	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	if input.Name == "" || input.QueueName == "" {
		return errorResponse(fmt.Errorf("name and queueName are required")), nil
	}

	c := &queue.Consumer{
		ID:        uuid.New().String(),
		Name:      input.Name,
		QueueName: input.QueueName,
		CreatedAt: time.Now().UTC(),
	}

	if err := _drv.CreateConsumer(ctx, c); err != nil {
		return errorResponse(err), nil
	}

	_consumers.Store(c.ID, c)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(c),
	}, nil
}

func handleGetConsumer(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}
	c, err := _drv.GetConsumer(ctx, id)
	if err != nil {
		return errorResponse(err), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(c)}, nil
}

func handleDeleteConsumer(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}
	if err := _drv.DeleteConsumer(ctx, id); err != nil {
		return errorResponse(err), nil
	}
	_consumers.Delete(id)
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(map[string]string{"status": "deleted"})}, nil
}

func handleListConsumers(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var items []*queue.Consumer
	_consumers.Range(func(key, value interface{}) bool {
		items = append(items, value.(*queue.Consumer))
		return true
	})

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"items": items, "total": len(items)}),
	}, nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getEnvList(key string) []string {
	val := os.Getenv(key)
	if val == "" {
		return nil
	}
	var result []string
	for _, s := range split(val, ",") {
		result = append(result, s)
	}
	return result
}

func split(s, sep string) []string {
	var result []string
	for i := 0; i < len(s); {
		j := index(s, sep, i)
		if j < 0 {
			result = append(result, s[i:])
			break
		}
		result = append(result, s[i:j])
		i = j + len(sep)
	}
	return result
}

func index(s, substr string, start int) int {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{
		Success: false,
		Error:   &plugin.Error{Code: "OPERATION_FAILED", Message: err.Error()},
	}
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
