package nodeagent

import (
	"context"
	"net"
	"sync"
	"testing"

	nodev1 "github.com/horizon/orion/gen/go/node/v1"
	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/image"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/services/orion-compute/internal/ports"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

const bufSize = 1024 * 1024

var (
	mockServer *mockNodeAgentServer
	lis        *bufconn.Listener
	mu         sync.Mutex
)

type mockNodeAgentServer struct {
	nodev1.UnimplementedNodeAgentServiceServer
	capturedImage *nodev1.ImageSpec
}

func (m *mockNodeAgentServer) BuildInstance(ctx context.Context, req *nodev1.BuildInstanceRequest) (*nodev1.BuildInstanceResponse, error) {
	mu.Lock()
	m.capturedImage = req.Spec.Image
	mu.Unlock()
	return &nodev1.BuildInstanceResponse{
		DomainName: "orion-srv-1",
		Status:     "active",
		DiskPath:   "/var/lib/orion/node-agent/srv-1.qcow2",
	}, nil
}

func (m *mockNodeAgentServer) DestroyInstance(ctx context.Context, req *nodev1.DestroyInstanceRequest) (*nodev1.DestroyInstanceResponse, error) {
	return &nodev1.DestroyInstanceResponse{Success: true}, nil
}

func (m *mockNodeAgentServer) AttachVolume(ctx context.Context, req *nodev1.AttachVolumeRequest) (*nodev1.AttachVolumeResponse, error) {
	return &nodev1.AttachVolumeResponse{Success: true}, nil
}

func (m *mockNodeAgentServer) DetachVolume(ctx context.Context, req *nodev1.DetachVolumeRequest) (*nodev1.DetachVolumeResponse, error) {
	return &nodev1.DetachVolumeResponse{Success: true}, nil
}

func (m *mockNodeAgentServer) ObserveInstance(ctx context.Context, req *nodev1.ObserveInstanceRequest) (*nodev1.ObserveInstanceResponse, error) {
	return &nodev1.ObserveInstanceResponse{
		Status:        "running",
		DomainName:    "orion-srv-1",
		ObservedState: "running",
	}, nil
}

func (m *mockNodeAgentServer) RegisterNode(ctx context.Context, req *nodev1.RegisterNodeRequest) (*nodev1.RegisterNodeResponse, error) {
	return &nodev1.RegisterNodeResponse{Accepted: true}, nil
}

func (m *mockNodeAgentServer) Heartbeat(ctx context.Context, req *nodev1.HeartbeatRequest) (*nodev1.HeartbeatResponse, error) {
	return &nodev1.HeartbeatResponse{Ok: true}, nil
}

func (m *mockNodeAgentServer) GetInventory(ctx context.Context, req *nodev1.GetInventoryRequest) (*nodev1.GetInventoryResponse, error) {
	return &nodev1.GetInventoryResponse{}, nil
}

func (m *mockNodeAgentServer) Health(ctx context.Context, req *emptypb.Empty) (*nodev1.HealthResponse, error) {
	return &nodev1.HealthResponse{Healthy: true}, nil
}

func setupGRPCTestServer(t *testing.T) (*grpc.Server, *bufconn.Listener) {
	mockServer = &mockNodeAgentServer{}
	lis = bufconn.Listen(bufSize)
	s := grpc.NewServer()
	nodev1.RegisterNodeAgentServiceServer(s, mockServer)
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Logf("Server exited with error: %v", err)
		}
	}()
	return s, lis
}

type hostDirectoryStub struct {
	record ports.HostRecord
}

func (s hostDirectoryStub) GetHost(context.Context, string) (ports.HostRecord, error) {
	return s.record, nil
}

func TestBuildServerSendsImageDescriptor(t *testing.T) {
	t.Parallel()

	s, lis := setupGRPCTestServer(t)
	defer s.Stop()

	client := New("", "http://127.0.0.1:8085", hostDirectoryStub{
		record: ports.HostRecord{
			HostID:           "host-1",
			NodeAgentGRPCURL: "bufnet",
		},
	})

	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithInsecure(),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()

	client.conns["bufnet"] = conn

	_, err = client.BuildServer(
		context.Background(),
		compute.Server{ID: "srv-1", HostID: "host-1", Name: "srv-a"},
		image.Image{
			ID:             "img-1",
			Path:           "/srv/orion/shared/image/cirros.qcow2",
			ChecksumSHA256: "abc123",
			SizeBytes:      4096,
		},
		[]networkkit.Port{{ID: "port-1", MACAddress: "fa:16:3e:00:00:01"}},
		1,
		512,
		10,
	)
	if err != nil {
		t.Fatalf("BuildServer returned error: %v", err)
	}

	mu.Lock()
	capturedImage := mockServer.capturedImage
	mu.Unlock()

	if capturedImage == nil {
		t.Fatal("image was not captured by mock server")
	}
	if capturedImage.Id.Value != "img-1" {
		t.Fatalf("unexpected image id: %s", capturedImage.Id.Value)
	}
	if capturedImage.SourcePath != "/srv/orion/shared/image/cirros.qcow2" {
		t.Fatalf("unexpected source path: %s", capturedImage.SourcePath)
	}
	if capturedImage.ChecksumSha256 != "abc123" {
		t.Fatalf("unexpected checksum: %s", capturedImage.ChecksumSha256)
	}
	if capturedImage.SourceUrl != "http://127.0.0.1:8085/v1/images/img-1/file" {
		t.Fatalf("unexpected source url: %s", capturedImage.SourceUrl)
	}
}
