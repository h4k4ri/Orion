package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/horizon/orion/sdk/go/plugin"
)

var haproxyConfigTemplate = `
global
    daemon
    maxconn {{.MaxConn}}
    stats socket /var/run/haproxy.sock mode 600 level admin
    log stdout format raw local0

defaults
    mode {{.Mode}}
    timeout connect {{.TimeoutConnect}}ms
    timeout client {{.TimeoutClient}}ms
    timeout server {{.TimeoutServer}}ms

{{range .Listeners}}
frontend {{.Name}}
    bind {{.Address}}:{{.Port}}
    {{if .TLS}}ssl crt {{.TLSCertificate}}{{end}}
    mode {{.Mode}}
{{if gt .RateLimitRPS 0}}    stick-table type ip size 100k expire 10m store http_req_rate(10s)
    http-request track-sc0 src
    http-request deny deny_status 429 if { sc0_http_req_rate(0) gt {{.RateLimitRPS}} }
{{end}}{{range .L7Rules}}    acl {{.Name}} path_beg {{.Path}}
{{if .Host}}    acl {{.Name}}_host hdr(host) -i {{.Host}}
    use_backend {{.BackendName}} if {{.Name}} {{.Name}}_host
{{else}}    use_backend {{.BackendName}} if {{.Name}}
{{end}}{{end}}
    default_backend {{.Name}}-default
{{end}}
{{range .Backends}}
backend {{.Name}}
    mode {{.Mode}}
    balance {{.Algorithm}}
    {{range .Members}}server {{.Name}} {{.IP}}:{{.Port}} weight {{.Weight}} {{if .HealthCheck}}check{{end}}
    {{end}}
{{end}}
`

type Listener struct {
	Name           string
	Address        string
	Port           int
	Mode           string
	Algorithm      string
	Members        []Member
	TLS            bool
	TLSCertificate string
	L7Rules        []L7Rule
	RateLimitRPS   int
}

type L7Rule struct {
	Name          string
	Path          string
	Host          string
	BackendMember string
	BackendName   string
}
type Backend struct {
	Name      string
	Mode      string
	Algorithm string
	Members   []Member
}

type Member struct {
	Name        string
	IP          string
	Port        int
	Weight      int
	HealthCheck bool
}

type HAProxyConfig struct {
	MaxConn        int
	Mode           string
	TimeoutConnect int
	TimeoutClient  int
	TimeoutServer  int
	Listeners      []Listener
	Backends       []Backend
}

func main() {
	if err := loadListeners(); err != nil {
		log.Fatalf("failed to load load-balancer state: %v", err)
	}
	p := plugin.New(plugin.Config{
		ID:      "loadbalancer-plugin",
		Name:    "LoadBalancer",
		Version: "1.0.0",
		Vendor:  "HAProxy",
	})

	p.Resource("orion.io/loadbalancer.lb", "v1").
		Handle("create", handleLBCreate).
		Handle("delete", handleLBDelete).
		Handle("get", handleLBGet).
		Handle("list", handleLBList).
		Handle("add_listener", handleAddListener).
		Handle("remove_listener", handleRemoveListener).
		Handle("add_member", handleAddMember).
		Handle("remove_member", handleRemoveMember).
		Handle("set_algorithm", handleSetAlgorithm).
		Handle("enable_member", handleEnableMember).
		Handle("disable_member", handleDisableMember).
		Handle("set_tls", handleSetTLS).
		Handle("add_l7_rule", handleAddL7Rule).
		Handle("remove_l7_rule", handleRemoveL7Rule).
		AddCapability("health_check", true).
		AddCapability("sslTermination", true).
		AddCapability("rate_limiting", true).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50064"
	}
	log.Printf("LoadBalancer plugin starting on %s...", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

var listeners = make(map[string]Listener)
var listenersMu sync.RWMutex
var configFile = "/etc/haproxy/haproxy.cfg"
var stateFile = os.Getenv("ORION_LOADBALANCER_STATE_FILE")

func loadListeners() error {
	if strings.TrimSpace(stateFile) == "" {
		return nil
	}
	payload, err := os.ReadFile(stateFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var loaded map[string]Listener
	if err := json.Unmarshal(payload, &loaded); err != nil {
		return fmt.Errorf("decode listener state: %w", err)
	}
	listenersMu.Lock()
	for name, listener := range loaded {
		listeners[name] = listener
	}
	listenersMu.Unlock()
	return nil
}

func persistListeners() error {
	if strings.TrimSpace(stateFile) == "" {
		return nil
	}
	listenersMu.RLock()
	loaded := make(map[string]Listener, len(listeners))
	for name, listener := range listeners {
		listener.Members = append([]Member(nil), listener.Members...)
		listener.L7Rules = append([]L7Rule(nil), listener.L7Rules...)
		loaded[name] = listener
	}
	listenersMu.RUnlock()
	payload, err := json.MarshalIndent(loaded, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(stateFile)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(directory, ".orion-lb-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, stateFile)
}

func handleLBCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	mode, _ := input["mode"].(string)
	algorithm, _ := input["algorithm"].(string)
	tls, _ := input["tls"].(bool)
	tlsCertificate, _ := input["tls_certificate"].(string)
	rateLimit, _ := input["rate_limit_rps"].(float64)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	if !validHAProxyToken(name) {
		return errorResponse(fmt.Errorf("name contains invalid HAProxy characters")), nil
	}
	if tls && tlsCertificate == "" {
		return errorResponse(fmt.Errorf("tls_certificate is required when tls is enabled")), nil
	}
	if mode == "" {
		mode = "http"
	}
	if algorithm == "" {
		algorithm = "roundrobin"
	}

	listenersMu.Lock()
	listeners[name] = Listener{
		Name:      name,
		Mode:      mode,
		Algorithm: algorithm,
		Members:   []Member{},
		TLS:       tls, TLSCertificate: tlsCertificate, RateLimitRPS: int(rateLimit),
	}
	listenersMu.Unlock()

	if err := reloadHAProxy(); err != nil {
		return errorResponse(fmt.Errorf("failed to reload haproxy: %w", err)), nil
	}

	log.Printf("LoadBalancer: created LB %s (mode=%s, algorithm=%s)", name, mode, algorithm)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":        name,
			"name":      name,
			"mode":      mode,
			"algorithm": algorithm,
			"status":    "active",
		}),
	}, nil
}

func handleLBDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	listenersMu.Lock()
	delete(listeners, name)
	listenersMu.Unlock()
	if err := reloadHAProxy(); err != nil {
		return errorResponse(fmt.Errorf("failed to reload haproxy: %w", err)), nil
	}

	log.Printf("LoadBalancer: deleted LB %s", name)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleSetTLS(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name        string `json:"name"`
		Enabled     bool   `json:"enabled"`
		Certificate string `json:"certificate"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.Name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	listenersMu.Lock()
	listener, ok := listeners[input.Name]
	if !ok {
		listenersMu.Unlock()
		return errorResponse(fmt.Errorf("loadbalancer %s not found", input.Name)), nil
	}
	if input.Enabled && input.Certificate == "" {
		listenersMu.Unlock()
		return errorResponse(fmt.Errorf("certificate is required when TLS is enabled")), nil
	}
	listener.TLS, listener.TLSCertificate = input.Enabled, input.Certificate
	listeners[input.Name] = listener
	listenersMu.Unlock()
	if err := reloadHAProxy(); err != nil {
		return errorResponse(err), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: marshal(listener)}, nil
}

func handleAddL7Rule(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		LBName        string `json:"lb_name"`
		Name          string `json:"name"`
		Path          string `json:"path"`
		Host          string `json:"host"`
		BackendMember string `json:"backend_member"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.LBName == "" || input.Name == "" || input.Path == "" {
		return errorResponse(fmt.Errorf("lb_name, name and path are required")), nil
	}
	if !validHAProxyToken(input.Name) || !strings.HasPrefix(input.Path, "/") || strings.ContainsAny(input.Path, "\r\n") || strings.ContainsAny(input.Host, "\r\n") {
		return errorResponse(fmt.Errorf("invalid L7 rule name, path or host")), nil
	}
	listenersMu.Lock()
	listener, ok := listeners[input.LBName]
	if !ok {
		listenersMu.Unlock()
		return errorResponse(fmt.Errorf("loadbalancer %s not found", input.LBName)), nil
	}
	for _, rule := range listener.L7Rules {
		if rule.Name == input.Name {
			listenersMu.Unlock()
			return errorResponse(fmt.Errorf("L7 rule already exists: %s", input.Name)), nil
		}
	}
	if input.BackendMember != "" {
		found := false
		for _, member := range listener.Members {
			if member.Name == input.BackendMember {
				found = true
				break
			}
		}
		if !found {
			listenersMu.Unlock()
			return errorResponse(fmt.Errorf("backend member not found: %s", input.BackendMember)), nil
		}
	}
	rule := L7Rule{Name: input.Name, Path: input.Path, Host: input.Host, BackendMember: input.BackendMember, BackendName: input.LBName + "-l7-" + input.Name}
	listener.L7Rules = append(listener.L7Rules, rule)
	listeners[input.LBName] = listener
	listenersMu.Unlock()
	if err := reloadHAProxy(); err != nil {
		return errorResponse(err), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: marshal(rule)}, nil
}

func handleRemoveL7Rule(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		LBName string `json:"lb_name"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.LBName == "" || input.Name == "" {
		return errorResponse(fmt.Errorf("lb_name and name are required")), nil
	}
	listenersMu.Lock()
	listener, ok := listeners[input.LBName]
	if !ok {
		listenersMu.Unlock()
		return errorResponse(fmt.Errorf("loadbalancer %s not found", input.LBName)), nil
	}
	filtered := listener.L7Rules[:0]
	for _, rule := range listener.L7Rules {
		if rule.Name != input.Name {
			filtered = append(filtered, rule)
		}
	}
	listener.L7Rules = filtered
	listeners[input.LBName] = listener
	listenersMu.Unlock()
	if err := reloadHAProxy(); err != nil {
		return errorResponse(err), nil
	}
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleLBGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	listenersMu.RLock()
	l, ok := listeners[name]
	listenersMu.RUnlock()
	if !ok {
		return errorResponse(fmt.Errorf("loadbalancer %s not found", name)), nil
	}

	log.Printf("LoadBalancer: got LB %s", name)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(l),
	}, nil
}

func handleLBList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	listenersMu.RLock()
	defer listenersMu.RUnlock()
	result := make([]map[string]interface{}, 0, len(listeners))
	for _, l := range listeners {
		result = append(result, map[string]interface{}{
			"name":      l.Name,
			"mode":      l.Mode,
			"algorithm": l.Algorithm,
			"members":   len(l.Members),
		})
	}

	log.Printf("LoadBalancer: listed %d LBs", len(result))
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"loadbalancers": result}),
	}, nil
}

func handleAddListener(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	address, _ := input["address"].(string)
	port, _ := input["port"].(float64)
	mode, _ := input["mode"].(string)
	algorithm, _ := input["algorithm"].(string)
	tls, _ := input["tls"].(bool)
	tlsCertificate, _ := input["tls_certificate"].(string)
	rateLimit, _ := input["rate_limit_rps"].(float64)

	if name == "" || address == "" {
		return errorResponse(fmt.Errorf("name and address are required")), nil
	}
	if port == 0 {
		return errorResponse(fmt.Errorf("port is required")), nil
	}
	if port < 1 || port > 65535 {
		return errorResponse(fmt.Errorf("invalid listener port")), nil
	}
	if !validHAProxyToken(name) || (tls && tlsCertificate == "") {
		return errorResponse(fmt.Errorf("invalid listener name or TLS certificate")), nil
	}
	if mode == "" {
		mode = "http"
	}
	if algorithm == "" {
		algorithm = "roundrobin"
	}

	listenersMu.Lock()
	l, ok := listeners[name]
	if !ok {
		l = Listener{Name: name, Mode: mode, Algorithm: algorithm, Members: []Member{}}
	}

	l.Address = address
	l.Port = int(port)
	l.Mode = mode
	l.Algorithm = algorithm
	l.TLS, l.TLSCertificate, l.RateLimitRPS = tls, tlsCertificate, int(rateLimit)
	listeners[name] = l
	listenersMu.Unlock()

	if err := reloadHAProxy(); err != nil {
		return errorResponse(fmt.Errorf("failed to reload haproxy: %w", err)), nil
	}

	log.Printf("LoadBalancer: added listener to %s (%s:%d)", name, address, int(port))

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "address": address, "port": int(port)}),
	}, nil
}

func handleRemoveListener(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	listenersMu.Lock()
	l, ok := listeners[name]
	if !ok {
		listenersMu.Unlock()
		return errorResponse(fmt.Errorf("loadbalancer %s not found", name)), nil
	}

	l.Address = ""
	l.Port = 0
	listeners[name] = l
	listenersMu.Unlock()

	if err := reloadHAProxy(); err != nil {
		return errorResponse(fmt.Errorf("failed to reload haproxy: %w", err)), nil
	}

	log.Printf("LoadBalancer: removed listener from %s", name)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleAddMember(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	lbName, _ := input["lb_name"].(string)
	memberName, _ := input["member_name"].(string)
	ip, _ := input["ip"].(string)
	port, _ := input["port"].(float64)
	weight, _ := input["weight"].(float64)

	if lbName == "" || memberName == "" || ip == "" {
		return errorResponse(fmt.Errorf("lb_name, member_name, and ip are required")), nil
	}
	if port == 0 {
		return errorResponse(fmt.Errorf("port is required")), nil
	}
	if port < 1 || port > 65535 || net.ParseIP(ip) == nil || !validHAProxyToken(memberName) {
		return errorResponse(fmt.Errorf("invalid member name, ip or port")), nil
	}
	if weight == 0 {
		weight = 1
	}
	if weight < 1 {
		return errorResponse(fmt.Errorf("weight must be positive")), nil
	}

	listenersMu.Lock()
	l, ok := listeners[lbName]
	if !ok {
		listenersMu.Unlock()
		return errorResponse(fmt.Errorf("loadbalancer %s not found", lbName)), nil
	}

	l.Members = append(l.Members, Member{
		Name:        memberName,
		IP:          ip,
		Port:        int(port),
		Weight:      int(weight),
		HealthCheck: true,
	})
	listeners[lbName] = l
	listenersMu.Unlock()

	if err := reloadHAProxy(); err != nil {
		return errorResponse(fmt.Errorf("failed to reload haproxy: %w", err)), nil
	}

	log.Printf("LoadBalancer: added member %s (%s:%d) to %s", memberName, ip, int(port), lbName)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"lb_name": lbName, "member_name": memberName, "ip": ip, "port": int(port),
		}),
	}, nil
}

func handleRemoveMember(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	lbName, _ := input["lb_name"].(string)
	memberName, _ := input["member_name"].(string)

	if lbName == "" || memberName == "" {
		return errorResponse(fmt.Errorf("lb_name and member_name are required")), nil
	}

	listenersMu.Lock()
	l, ok := listeners[lbName]
	if !ok {
		listenersMu.Unlock()
		return errorResponse(fmt.Errorf("loadbalancer %s not found", lbName)), nil
	}

	for i, m := range l.Members {
		if m.Name == memberName {
			l.Members = append(l.Members[:i], l.Members[i+1:]...)
			break
		}
	}
	listeners[lbName] = l
	listenersMu.Unlock()

	if err := reloadHAProxy(); err != nil {
		return errorResponse(fmt.Errorf("failed to reload haproxy: %w", err)), nil
	}

	log.Printf("LoadBalancer: removed member %s from %s", memberName, lbName)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleSetAlgorithm(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	algorithm, _ := input["algorithm"].(string)

	if name == "" || algorithm == "" {
		return errorResponse(fmt.Errorf("name and algorithm are required")), nil
	}

	listenersMu.Lock()
	l, ok := listeners[name]
	if !ok {
		listenersMu.Unlock()
		return errorResponse(fmt.Errorf("loadbalancer %s not found", name)), nil
	}

	l.Algorithm = algorithm
	listeners[name] = l
	listenersMu.Unlock()

	if err := reloadHAProxy(); err != nil {
		return errorResponse(fmt.Errorf("failed to reload haproxy: %w", err)), nil
	}

	log.Printf("LoadBalancer: set algorithm %s for %s", algorithm, name)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "algorithm": algorithm}),
	}, nil
}

func handleEnableMember(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	lbName, _ := input["lb_name"].(string)
	memberName, _ := input["member_name"].(string)

	if lbName == "" || memberName == "" {
		return errorResponse(fmt.Errorf("lb_name and member_name are required")), nil
	}

	cmd := exec.CommandContext(ctx, "docker", "exec", "haproxy", "socat",
		"/var/run/haproxy.sock", fmt.Sprintf("set server %s/%s state ready", lbName, memberName))
	cmd.Run()

	log.Printf("LoadBalancer: enabled member %s/%s", lbName, memberName)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"lb_name": lbName, "member_name": memberName, "status": "enabled"}),
	}, nil
}

func handleDisableMember(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	lbName, _ := input["lb_name"].(string)
	memberName, _ := input["member_name"].(string)

	if lbName == "" || memberName == "" {
		return errorResponse(fmt.Errorf("lb_name and member_name are required")), nil
	}

	cmd := exec.CommandContext(ctx, "docker", "exec", "haproxy", "socat",
		"/var/run/haproxy.sock", fmt.Sprintf("set server %s/%s state maint", lbName, memberName))
	cmd.Run()

	log.Printf("LoadBalancer: disabled member %s/%s", lbName, memberName)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"lb_name": lbName, "member_name": memberName, "status": "disabled"}),
	}, nil
}

func reloadHAProxy() error {
	if err := persistListeners(); err != nil {
		return fmt.Errorf("persist load-balancer state: %w", err)
	}
	config, err := generateConfig()
	if err != nil {
		return err
	}

	if err := os.WriteFile(configFile, []byte(config), 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	cmd := exec.CommandContext(context.Background(), "haproxy", "-f", configFile, "-c")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("HAProxy config check failed: %s", string(output))
		return fmt.Errorf("config check failed: %w", err)
	}

	cmd = exec.CommandContext(context.Background(), "systemctl", "reload", "haproxy")
	cmd.Run()

	return nil
}

func generateConfig() (string, error) {
	listenersMu.RLock()
	defer listenersMu.RUnlock()
	cfg := HAProxyConfig{
		MaxConn:        4096,
		Mode:           "http",
		TimeoutConnect: 5000,
		TimeoutClient:  30000,
		TimeoutServer:  30000,
		Listeners:      make([]Listener, 0, len(listeners)),
		Backends:       make([]Backend, 0),
	}

	for _, l := range listeners {
		if l.Port > 0 {
			cfg.Backends = append(cfg.Backends, Backend{Name: l.Name + "-default", Mode: l.Mode, Algorithm: l.Algorithm, Members: append([]Member(nil), l.Members...)})
			for _, rule := range l.L7Rules {
				members := l.Members
				if rule.BackendMember != "" {
					members = nil
					for _, member := range l.Members {
						if member.Name == rule.BackendMember {
							members = []Member{member}
							break
						}
					}
				}
				cfg.Backends = append(cfg.Backends, Backend{Name: rule.BackendName, Mode: l.Mode, Algorithm: l.Algorithm, Members: members})
			}
			cfg.Listeners = append(cfg.Listeners, l)
		}
	}

	tmpl, err := template.New("haproxy").Parse(haproxyConfigTemplate)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	if err := tmpl.Execute(&out, cfg); err != nil {
		return "", err
	}

	return out.String(), nil
}

func validHAProxyToken(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "LB_ERROR", Message: err.Error()}}
}

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
