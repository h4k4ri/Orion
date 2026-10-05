package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/horizon/orion/core/node"
	commonv1 "github.com/horizon/orion/gen/go/common/v1"
	nodev1 "github.com/horizon/orion/gen/go/node/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	nodeID      = flag.String("node-id", "", "Unique node identifier")
	nodeName    = flag.String("node-name", "", "Human-readable node name")
	address     = flag.String("address", "", "Node address (IP or hostname)")
	coreAddress = flag.String("core", "localhost:8080", "Orion Core address")
	port        = flag.Int("port", 9090, "Agent gRPC port")
	plugins     = flag.String("plugins", "", "Comma-separated list of plugins")
)

type agentServer struct {
	nodev1.UnimplementedNodeAgentServiceServer
	nodeID   string
	nodeName string
	address  string
	port     int
	registry node.NodeRegistry
}

func main() {
	flag.Parse()
	if *nodeID == "" || *address == "" {
		log.Fatal("--node-id and --address are required")
	}

	registry := node.NewInMemoryRegistry()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	localNode := &node.Node{
		ID: *nodeID, Name: *nodeName, Address: *address, Port: *port,
		Status: node.NodeStatusOnline, Plugins: parsePlugins(*plugins),
		Capabilities: map[string]interface{}{"compute": true, "storage": true, "network": true},
	}
	if err := registry.Register(ctx, localNode); err != nil {
		log.Fatalf("register local node: %v", err)
	}
	go runHeartbeat(ctx, registry, *nodeID)
	if err := registerWithCore(ctx, *nodeID, *coreAddress); err != nil {
		log.Printf("warning: could not register with core: %v", err)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	nodev1.RegisterNodeAgentServiceServer(grpcServer, &agentServer{
		nodeID: *nodeID, nodeName: *nodeName, address: *address, port: *port, registry: registry,
	})
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()
	log.Printf("node agent %s listening on :%d", *nodeID, *port)
	if err := grpcServer.Serve(ln); err != nil && ctx.Err() == nil {
		log.Fatalf("serve failed: %v", err)
	}
}

func runHeartbeat(ctx context.Context, registry node.NodeRegistry, id string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := registry.Heartbeat(ctx, id); err != nil {
				log.Printf("heartbeat failed: %v", err)
			}
		}
	}
}

func registerWithCore(ctx context.Context, id, coreAddress string) error {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(dialCtx, coreAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return err
	}
	defer conn.Close()
	client := nodev1.NewNodeAgentServiceClient(conn)
	_, err = client.RegisterNode(ctx, &nodev1.RegisterNodeRequest{Node: &nodev1.Node{
		Id:           &commonv1.NodeID{Value: id},
		Status:       nodev1.NodeStatus_NODE_STATUS_ONLINE,
		RegisteredAt: timestamppb.Now(),
	}})
	return err
}

func (s *agentServer) RegisterNode(ctx context.Context, req *nodev1.RegisterNodeRequest) (*nodev1.RegisterNodeResponse, error) {
	return &nodev1.RegisterNodeResponse{NodeId: &commonv1.NodeID{Value: s.nodeID}, Accepted: true, Message: "node agent ready"}, nil
}

func (s *agentServer) Heartbeat(ctx context.Context, req *nodev1.HeartbeatRequest) (*nodev1.HeartbeatResponse, error) {
	if err := s.registry.Heartbeat(ctx, s.nodeID); err != nil {
		return nil, err
	}
	return &nodev1.HeartbeatResponse{Ok: true}, nil
}

func (s *agentServer) GetInventory(ctx context.Context, req *nodev1.GetInventoryRequest) (*nodev1.GetInventoryResponse, error) {
	return &nodev1.GetInventoryResponse{Resources: &nodev1.NodeResources{}, Traits: &nodev1.NodeTraits{}}, nil
}

func (s *agentServer) Health(ctx context.Context, req *emptypb.Empty) (*nodev1.HealthResponse, error) {
	return &nodev1.HealthResponse{Healthy: true, Version: "1.0.0", LibvirtStatus: "unknown"}, nil
}

func parsePlugins(value string) []string {
	var result []string
	for _, item := range splitString(value, ",") {
		item = trimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func splitString(value, separator string) []string {
	if value == "" {
		return nil
	}
	var result []string
	for start := 0; ; {
		index := -1
		for i := start; i+len(separator) <= len(value); i++ {
			if value[i:i+len(separator)] == separator {
				index = i
				break
			}
		}
		if index < 0 {
			return append(result, value[start:])
		}
		result = append(result, value[start:index])
		start = index + len(separator)
	}
}

func trimSpace(value string) string {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t') {
		end--
	}
	return value[start:end]
}
