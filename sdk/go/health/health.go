package health

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Status int

const (
	StatusUnknown Status = iota
	StatusHealthy
	StatusUnhealthy
	StatusDegraded
)

func (s Status) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusUnhealthy:
		return "unhealthy"
	case StatusDegraded:
		return "degraded"
	default:
		return "unknown"
	}
}

type Checker interface {
	Check(ctx context.Context) error
}

type healthCheck struct {
	name    string
	checker Checker
	timeout time.Duration
}

type Server struct {
	mu      sync.RWMutex
	checks  map[string]healthCheck
	status  Status
	details map[string]string
}

func NewServer() *Server {
	return &Server{
		checks:  make(map[string]healthCheck),
		status:  StatusUnknown,
		details: make(map[string]string),
	}
}

func (s *Server) Register(name string, checker Checker, timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.checks[name] = healthCheck{
		name:    name,
		checker: checker,
		timeout: timeout,
	}
}

func (s *Server) Check(ctx context.Context) (Status, map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.checks) == 0 {
		return StatusUnknown, nil, fmt.Errorf("no checks registered")
	}

	results := make(map[string]string)
	allHealthy := true
	anyUnhealthy := false

	for _, hc := range s.checks {
		checkCtx := ctx
		if hc.timeout > 0 {
			var cancel context.CancelFunc
			checkCtx, cancel = context.WithTimeout(ctx, hc.timeout)
			defer cancel()
		}

		err := hc.checker.Check(checkCtx)
		if err != nil {
			results[hc.name] = fmt.Sprintf("unhealthy: %v", err)
			allHealthy = false
			anyUnhealthy = true
		} else {
			results[hc.name] = "healthy"
		}
	}

	s.details = results

	if allHealthy {
		s.status = StatusHealthy
		return StatusHealthy, results, nil
	}

	if anyUnhealthy {
		s.status = StatusUnhealthy
		return StatusUnhealthy, results, fmt.Errorf("one or more checks failed")
	}

	s.status = StatusDegraded
	return StatusDegraded, results, nil
}

func (s *Server) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Server) Details() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	details := make(map[string]string)
	for k, v := range s.details {
		details[k] = v
	}
	return details
}

type GRPCServer struct {
	*Server
}

func NewGRPCServer() *GRPCServer {
	return &GRPCServer{Server: NewServer()}
}

func (s *GRPCServer) Register(name string, checker Checker) {
	s.Server.Register(name, checker, 5*time.Second)
}

type memoryChecker struct {
	status Status
	err    error
}

func (mc *memoryChecker) Check(ctx context.Context) error {
	return mc.err
}

func MemoryChecker(status Status) Checker {
	return &memoryChecker{status: status}
}
