package plugin

import (
	"context"
	"testing"

	v1 "github.com/horizon/orion/sdk/go/proto/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestExecutorServerInvokesResourceHandler(t *testing.T) {
	p := New(Config{ID: "test", Name: "Test", Version: "v1"})
	p.Resource("orion.io/storage.volume", "v1").
		Handle("create", func(context.Context, *ResourceRequest) (*ResourceResponse, error) {
			return &ResourceResponse{Success: true, Result: []byte(`{"id":"vol-1"}`)}, nil
		}).Register()

	s := NewServer(p, "127.0.0.1:0")
	server := &executorServer{plugin: p, parent: s}
	requestPayload, _ := structpb.NewStruct(map[string]interface{}{"name": "volume"})
	response, err := server.Invoke(context.Background(), &v1.InvokeRequest{
		RequestId: "req-1", Resource: "orion.io/storage.volume", ResourceVersion: "v1", Operation: "create", Payload: requestPayload,
	})
	if err != nil || !response.Success {
		t.Fatalf("invoke failed: err=%v response=%v", err, response)
	}
	if response.Result.AsMap()["id"] != "vol-1" {
		t.Fatalf("unexpected result: %#v", response.Result.AsMap())
	}
}

func TestExecutorServerInvokesRelationshipHandler(t *testing.T) {
	p := New(Config{ID: "test", Name: "Test", Version: "v1"})
	p.Relationship("orion.io/storage.attachment", "source").
		Handle("prepare", func(context.Context, *RelationshipRequest) (*RelationshipResponse, error) {
			return &RelationshipResponse{Success: true, Result: []byte(`{"connection":{"protocol":"mock"}}`)}, nil
		}).Register()

	s := NewServer(p, "127.0.0.1:0")
	server := &executorServer{plugin: p, parent: s}
	response, err := server.Invoke(context.Background(), &v1.InvokeRequest{
		RequestId: "req-2", Resource: "orion.io/storage.attachment", Operation: "source:prepare",
		Metadata: map[string]string{"source_resource_id": "vol-1"},
	})
	if err != nil || !response.Success {
		t.Fatalf("relationship invoke failed: err=%v response=%v", err, response)
	}
}
