package conformance

import (
	"context"
	"testing"

	"github.com/horizon/orion/sdk/go/plugin"
)

func TestMockPluginConformance(t *testing.T) {
	p := plugin.New(plugin.Config{
		ID:      "mock",
		Name:    "Mock",
		Version: "1.0.0",
		Vendor:  "Orion",
	})

	p.Resource("orion.io/storage.volume", "v1").
		Handle("create", mockCreate).
		Handle("delete", mockDelete).
		Register()

	p.Relationship("orion.io/storage.attachment", "source").
		Handle("prepare", mockPrepare).
		Handle("release", mockRelease).
		Register()

	RunConformanceSuite(t, "mock", p)
}

func mockCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	return &plugin.ResourceResponse{
		Success: true,
		Result:  []byte(`{"id": "vol-1"}`),
	}, nil
}

func mockDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	return &plugin.ResourceResponse{
		Success: true,
	}, nil
}

func mockPrepare(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	return &plugin.RelationshipResponse{
		Success: true,
		Result:  []byte(`{"connection": {}}`),
	}, nil
}

func mockRelease(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	return &plugin.RelationshipResponse{
		Success: true,
	}, nil
}

func TestIdempotency(t *testing.T) {
	handler := func(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
		return &plugin.ResourceResponse{
			Success: true,
			Result:  []byte(`{"id": "test-123"}`),
		}, nil
	}

	input := []byte(`{"name": "test"}`)
	if err := ValidateIdempotency(context.Background(), handler, input); err != nil {
		t.Errorf("Idempotency check failed: %v", err)
	}
}
