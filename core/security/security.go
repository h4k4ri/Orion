package security

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type TLSConfig struct {
	CertFile string
	KeyFile  string
	CAFile   string
}

type mTLSConfig struct {
	ServerCert tls.Certificate
	ClientCAs *x509.CertPool
}

func NewServerCredentials(cfg TLSConfig) (grpc.ServerOption, error) {
	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return grpc.Creds(insecure.NewCredentials()), nil
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server cert: %w", err)
	}

	var caCertPool *x509.CertPool
	if cfg.CAFile != "" {
		caCertPool = x509.NewCertPool()
		ca, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}
		if !caCertPool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:   caCertPool,
		ClientAuth:  tls.NoClientCert,
	}

	if caCertPool != nil {
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return grpc.Creds(credentials.NewTLS(tlsCfg)), nil
}

type ClientCredentials struct {
	mu       sync.RWMutex
	configs  map[string]*TLSConfig
	creds    map[string]credentials.TransportCredentials
}

func NewClientCredentials() *ClientCredentials {
	return &ClientCredentials{
		configs: make(map[string]*TLSConfig),
		creds:   make(map[string]credentials.TransportCredentials),
	}
}

func (c *ClientCredentials) AddPlugin(name string, cfg TLSConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configs[name] = &cfg
}

func (c *ClientCredentials) GetCredentials(pluginName string) (credentials.TransportCredentials, error) {
	c.mu.RLock()
	cfg, ok := c.configs[pluginName]
	c.mu.RUnlock()

	if !ok {
		return insecure.NewCredentials(), nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if cached, ok := c.creds[pluginName]; ok {
		return cached, nil
	}

	if cfg.CertFile == "" || cfg.KeyFile == "" {
		c.creds[pluginName] = insecure.NewCredentials()
		return c.creds[pluginName], nil
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client cert: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ServerName:   pluginName,
	}

	if cfg.CAFile != "" {
		tlsCfg.RootCAs = x509.NewCertPool()
		ca, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}
		if !tlsCfg.RootCAs.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
	}

	c.creds[pluginName] = credentials.NewTLS(tlsCfg)
	return c.creds[pluginName], nil
}

type RBAC struct {
	mu       sync.RWMutex
	policies map[string]*Policy
}

type Policy struct {
	Resources []string
	Actions   []string
	Effect    string
}

const (
	EffectAllow = "allow"
	EffectDeny  = "deny"
)

func NewRBAC() *RBAC {
	return &RBAC{
		policies: make(map[string]*Policy),
	}
}

func (r *RBAC) AddPolicy(role, resource, action, effect string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s:%s:%s", role, resource, action)
	r.policies[key] = &Policy{
		Resources: []string{resource},
		Actions:   []string{action},
		Effect:    effect,
	}
}

func (r *RBAC) IsAllowed(role, resource, action string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := fmt.Sprintf("%s:%s:%s", role, resource, action)
	if policy, ok := r.policies[key]; ok {
		return policy.Effect == EffectAllow
	}

	wildcardKey := fmt.Sprintf("%s:%s:%s", role, resource, "*")
	if policy, ok := r.policies[wildcardKey]; ok {
		return policy.Effect == EffectAllow
	}

	return false
}

type AuditLog struct {
	mu      sync.RWMutex
	entries []*AuditEntry
}

type AuditEntry struct {
	Timestamp time.Time
	Actor     string
	Action    string
	Resource  string
	Result    string
	Details   map[string]string
}

func NewAuditLog() *AuditLog {
	return &AuditLog{
		entries: make([]*AuditEntry, 0),
	}
}

func (a *AuditLog) Log(ctx context.Context, actor, action, resource, result string, details map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.entries = append(a.entries, &AuditEntry{
		Timestamp: time.Now(),
		Actor:    actor,
		Action:   action,
		Resource: resource,
		Result:   result,
		Details:  details,
	})
}

func (a *AuditLog) GetEntries(limit int) []*AuditEntry {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if limit <= 0 || limit > len(a.entries) {
		limit = len(a.entries)
	}

	result := make([]*AuditEntry, limit)
	copy(result, a.entries[len(a.entries)-limit:])
	return result
}

type RateLimiter struct {
	mu       sync.RWMutex
	limits   map[string]*Limit
	requests map[string][]time.Time
}

type Limit struct {
	MaxRequests int
	Window      time.Duration
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		limits:   make(map[string]*Limit),
		requests: make(map[string][]time.Time),
	}
}

func (r *RateLimiter) SetLimit(name string, max int, window time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limits[name] = &Limit{MaxRequests: max, Window: window}
}

func (r *RateLimiter) Allow(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	limit, ok := r.limits[name]
	if !ok {
		return true
	}

	now := time.Now()
	windowStart := now.Add(-limit.Window)

	requests := r.requests[name]
	var valid []time.Time
	for _, t := range requests {
		if t.After(windowStart) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= limit.MaxRequests {
		r.requests[name] = valid
		return false
	}

	r.requests[name] = append(valid, now)
	return true
}
