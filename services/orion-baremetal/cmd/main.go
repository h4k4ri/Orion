package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"google.golang.org/grpc"

	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/node"
	"github.com/horizon/orion/core/runtime"
	pb "github.com/horizon/orion/services/orion-baremetal/proto/v1"
)

var (
	port = flag.Int("port", 15001, "Baremetal service port")
)

type baremetalServer struct {
	pb.UnimplementedBaremetalServiceServer
	executor     runtime.NodePluginExecutor
	nodeRegistry node.NodeRegistry
	providerReg  *inMemoryProviderRegistry
}

func main() {
	flag.Parse()

	registry := node.NewInMemoryRegistry()
	providerReg := &inMemoryProviderRegistry{providers: make(map[string]*domain.Provider)}
	executor := runtime.NewMultiNodePluginExecutor(registry, providerReg)

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterBaremetalServiceServer(s, &baremetalServer{
		executor:     executor,
		nodeRegistry: registry,
		providerReg:  providerReg,
	})

	log.Printf("orion-baremetal listening on :%d", *port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down baremetal service...")
		s.GracefulStop()
	}()

	if err := s.Serve(ln); err != nil {
		log.Fatalf("Serve failed: %v", err)
	}
}

type inMemoryProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]*domain.Provider
}

func (r *inMemoryProviderRegistry) Get(ctx context.Context, providerID string) (*domain.Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerID]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", providerID)
	}
	return p, nil
}

func (r *inMemoryProviderRegistry) List(ctx context.Context) ([]*domain.Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*domain.Provider, 0, len(r.providers))
	for _, p := range r.providers {
		result = append(result, p)
	}
	return result, nil
}

func (s *baremetalServer) CreateNode(ctx context.Context, req *pb.CreateNodeRequest) (*pb.CreateNodeResponse, error) {
	if req.GetName() == "" || req.GetDriver() == "" || req.GetEndpoint() == "" {
		return nil, fmt.Errorf("name, driver and endpoint are required")
	}
	nodeID := req.GetNodeId()
	if nodeID == "" {
		nodeID = fmt.Sprintf("bm-host-%s", req.GetName())
	}
	providerID := fmt.Sprintf("bm-node-%s", req.GetName())
	address, nodePort := splitEndpoint(req.GetEndpoint())
	if address == "" || nodePort == 0 {
		return nil, fmt.Errorf("endpoint must be host:port")
	}
	if err := s.nodeRegistry.Register(ctx, &node.Node{
		ID:      nodeID,
		Name:    req.GetName(),
		Address: address,
		Port:    nodePort,
		Status:  node.NodeStatusOnline,
		Plugins: []string{req.GetDriver()},
	}); err != nil {
		return nil, fmt.Errorf("register node: %w", err)
	}

	p := &domain.Provider{
		ProviderID:  providerID,
		PluginID:    req.GetDriver(),
		Name:        req.GetName(),
		Endpoint:    req.GetEndpoint(),
		NodeID:      nodeID,
		HealthState: domain.HealthStateUnknown,
		Config:      mustMarshal(req.GetConfig()),
		EffectiveCapabilities: mustMarshal(map[string]interface{}{
			"driver":        req.GetDriver(),
			"power_state":   "unknown",
			"boot_device":   req.GetBootDevice(),
			"security_mode": req.GetSecurityMode(),
		}),
	}

	s.providerReg.mu.Lock()
	s.providerReg.providers[providerID] = p
	s.providerReg.mu.Unlock()
	if err := s.executor.AttachToNode(ctx, nodeID, providerID, req.GetEndpoint()); err != nil {
		return nil, fmt.Errorf("attach provider to node: %w", err)
	}

	createPayload := make(map[string]interface{}, len(req.GetConfig())+3)
	for key, value := range req.GetConfig() {
		createPayload[key] = value
	}
	createPayload["name"] = providerID
	if _, ok := createPayload["protocol"]; !ok {
		createPayload["protocol"] = "redfish"
	}
	op, err := s.executor.Execute(ctx, providerID, "create", mustMarshal(createPayload))
	if err != nil {
		return nil, err
	}
	if op.State == domain.OperationStateFailed {
		return nil, fmt.Errorf("provider initialization failed: %s", op.ErrorMessage)
	}

	return &pb.CreateNodeResponse{
		Node: &pb.BaremetalNode{
			Id:           providerID,
			Name:         req.GetName(),
			Driver:       req.GetDriver(),
			PowerState:   pb.PowerState_POWER_STATE_UNKNOWN,
			BootDevice:   req.GetBootDevice(),
			SecurityMode: req.GetSecurityMode(),
			Status:       pb.NodeStatus_NODE_STATUS_ACTIVE,
		},
	}, nil
}

func (s *baremetalServer) GetNode(ctx context.Context, req *pb.GetNodeRequest) (*pb.GetNodeResponse, error) {
	p, err := s.providerReg.Get(ctx, req.NodeId)
	if err != nil {
		return nil, err
	}

	var caps map[string]interface{}
	if err := json.Unmarshal(p.EffectiveCapabilities, &caps); err != nil {
		return nil, fmt.Errorf("invalid provider capabilities: %w", err)
	}

	return &pb.GetNodeResponse{
		Node: &pb.BaremetalNode{
			Id:           p.ProviderID,
			Name:         p.Name,
			Driver:       p.PluginID,
			PowerState:   powerStateFromString(getString(caps["power_state"])),
			BootDevice:   getString(caps["boot_device"]),
			SecurityMode: getString(caps["security_mode"]),
			Status:       nodeStatusFromState(p.HealthState),
		},
	}, nil
}

func (s *baremetalServer) ListNodes(ctx context.Context, req *pb.ListNodesRequest) (*pb.ListNodesResponse, error) {
	providers, err := s.providerReg.List(ctx)
	if err != nil {
		return nil, err
	}

	nodes := make([]*pb.BaremetalNode, 0, len(providers))
	for _, p := range providers {
		var caps map[string]interface{}
		if err := json.Unmarshal(p.EffectiveCapabilities, &caps); err != nil {
			continue
		}

		nodes = append(nodes, &pb.BaremetalNode{
			Id:           p.ProviderID,
			Name:         p.Name,
			Driver:       p.PluginID,
			PowerState:   powerStateFromString(getString(caps["power_state"])),
			BootDevice:   getString(caps["boot_device"]),
			SecurityMode: getString(caps["security_mode"]),
			Status:       nodeStatusFromState(p.HealthState),
		})
	}

	return &pb.ListNodesResponse{Nodes: nodes}, nil
}

func (s *baremetalServer) SetPowerState(ctx context.Context, req *pb.SetPowerStateRequest) (*pb.OperationResponse, error) {
	payload := mustMarshal(map[string]interface{}{
		"id":          req.NodeId,
		"action":      "power",
		"power_state": req.PowerState.String(),
		"node_id":     req.NodeId,
	})

	operation, err := powerOperation(req.PowerState)
	if err != nil {
		return &pb.OperationResponse{Success: false, Error: err.Error()}, nil
	}
	op, err := s.executor.Execute(ctx, req.NodeId, operation, payload)
	if err != nil {
		return &pb.OperationResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.OperationResponse{
		Success:     op.State == domain.OperationStateSucceeded,
		OperationId: op.OperationID,
	}, nil
}

func (s *baremetalServer) SetBootDevice(ctx context.Context, req *pb.SetBootDeviceRequest) (*pb.OperationResponse, error) {
	payload := mustMarshal(map[string]interface{}{
		"id":          req.NodeId,
		"action":      "boot_device",
		"boot_device": req.BootDevice,
		"node_id":     req.NodeId,
	})

	op, err := s.executor.Execute(ctx, req.NodeId, "set_boot_device", payload)
	if err != nil {
		return &pb.OperationResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.OperationResponse{
		Success:     op.State == domain.OperationStateSucceeded,
		OperationId: op.OperationID,
	}, nil
}

func (s *baremetalServer) ProvisionNode(ctx context.Context, req *pb.ProvisionNodeRequest) (*pb.OperationResponse, error) {
	payload := mustMarshal(map[string]interface{}{
		"id":            req.NodeId,
		"action":        "provision",
		"instance_info": req.InstanceInfo,
		"node_id":       req.NodeId,
	})

	op, err := s.executor.Execute(ctx, req.NodeId, "provision", payload)
	if err != nil {
		return &pb.OperationResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.OperationResponse{
		Success:     op.State == domain.OperationStateSucceeded,
		OperationId: op.OperationID,
	}, nil
}

func (s *baremetalServer) DeprovisionNode(ctx context.Context, req *pb.DeprovisionNodeRequest) (*pb.OperationResponse, error) {
	payload := mustMarshal(map[string]interface{}{
		"id":      req.NodeId,
		"action":  "deprovision",
		"node_id": req.NodeId,
	})

	op, err := s.executor.Execute(ctx, req.NodeId, "deprovision", payload)
	if err != nil {
		return &pb.OperationResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.OperationResponse{
		Success:     op.State == domain.OperationStateSucceeded,
		OperationId: op.OperationID,
	}, nil
}

func powerStateFromString(s string) pb.PowerState {
	switch s {
	case "power_on":
		return pb.PowerState_POWER_STATE_ON
	case "power_off":
		return pb.PowerState_POWER_STATE_OFF
	case "reboot":
		return pb.PowerState_POWER_STATE_REBOOT
	default:
		return pb.PowerState_POWER_STATE_UNKNOWN
	}
}

func nodeStatusFromState(hs domain.HealthState) pb.NodeStatus {
	switch hs {
	case domain.HealthStateHealthy:
		return pb.NodeStatus_NODE_STATUS_ACTIVE
	case domain.HealthStateDegraded:
		return pb.NodeStatus_NODE_STATUS_DEGRADED
	case domain.HealthStateUnhealthy:
		return pb.NodeStatus_NODE_STATUS_FAILED
	default:
		return pb.NodeStatus_NODE_STATUS_ENROLLING
	}
}

func getString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func splitEndpoint(endpoint string) (string, int) {
	endpoint = strings.TrimPrefix(strings.TrimSpace(endpoint), "grpc://")
	host, portString, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", 0
	}
	var port int
	if _, err := fmt.Sscanf(portString, "%d", &port); err != nil {
		return "", 0
	}
	return host, port
}

func powerOperation(state pb.PowerState) (string, error) {
	switch state {
	case pb.PowerState_POWER_STATE_ON:
		return "power_on", nil
	case pb.PowerState_POWER_STATE_OFF:
		return "power_off", nil
	case pb.PowerState_POWER_STATE_REBOOT:
		return "reboot", nil
	default:
		return "", fmt.Errorf("unsupported power state: %s", state.String())
	}
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
