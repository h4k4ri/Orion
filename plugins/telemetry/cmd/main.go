package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"

	orionmetric "github.com/horizon/orion/plugins/telemetry/metric"
	"github.com/horizon/orion/sdk/go/plugin"
)

var (
	_store  = newTelemetryStore()
	_meter  otelmetric.Meter
	_reg    *promclient.Registry
	_server *http.Server
)

func main() {
	if stateFile := strings.TrimSpace(os.Getenv("ORION_TELEMETRY_STATE_FILE")); stateFile != "" {
		_store = newTelemetryStoreWithFile(stateFile)
		if err := _store.load(); err != nil {
			log.Fatalf("failed to load telemetry state: %v", err)
		}
	}
	initTelemetry()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(_reg, promhttp.HandlerOpts{}))
	metricsPort := getEnvOr("ORION_TELEMETRY_HTTP_PORT", "50081")
	_server = &http.Server{Addr: ":" + metricsPort, Handler: mux}
	go func() {
		log.Printf("Telemetry metrics endpoint: :%s/metrics", metricsPort)
		if err := _server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	p := plugin.New(plugin.Config{ID: "telemetry-plugin", Name: "Orion Telemetry", Version: "1.0.0", Vendor: "Orion"})
	p.Resource("orion.io/telemetry.metrics", "v1").
		Handle("create_metric", handleCreateMetric).
		Handle("record_metric", handleRecordMetric).
		Handle("get_metric", handleGetMetric).
		Handle("list_metrics", handleListMetrics).
		Handle("query_metrics", handleQueryMetrics).
		Handle("create_alert", handleCreateAlert).
		Handle("get_alert", handleGetAlert).
		Handle("list_alerts", handleListAlerts).
		Handle("create_dashboard", handleCreateDashboard).
		Handle("get_dashboard", handleGetDashboard).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50080"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := p.Serve(ctx, endpoint); err != nil {
		log.Fatalf("telemetry plugin stopped: %v", err)
	}
}

func initTelemetry() {
	_reg = promclient.NewRegistry()
	_reg.MustRegister(promclient.NewGoCollector())
	_reg.MustRegister(promclient.NewProcessCollector(promclient.ProcessCollectorOpts{}))
	_reg.MustRegister(&metricCollector{store: _store})

	exporter, err := otelprom.New(otelprom.WithRegisterer(_reg))
	if err != nil {
		log.Fatalf("failed to create prometheus exporter: %v", err)
	}
	res, err := resource.New(context.Background(), resource.WithAttributes(
		semconv.ServiceName("orion-telemetry"), semconv.ServiceVersion("1.0.0"),
	))
	if err != nil {
		log.Fatalf("failed to create resource: %v", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter), sdkmetric.WithResource(res))
	otel.SetMeterProvider(meterProvider)
	_meter = meterProvider.Meter("orion-telemetry")
	log.Println("Telemetry initialized with Prometheus exporter")
}

func handleCreateMetric(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name        string            `json:"name"`
		Description string            `json:"description,omitempty"`
		Unit        string            `json:"unit,omitempty"`
		Type        string            `json:"type"`
		Labels      map[string]string `json:"labels,omitempty"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.Name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	if input.Type == "" {
		input.Type = string(orionmetric.MetricTypeGauge)
	}
	if input.Type != string(orionmetric.MetricTypeGauge) && input.Type != string(orionmetric.MetricTypeCounter) {
		return errorResponse(fmt.Errorf("unsupported metric type: %s", input.Type)), nil
	}
	m := &orionmetric.Metric{Name: input.Name, Description: input.Description, Unit: input.Unit,
		Type: orionmetric.MetricType(input.Type), Labels: input.Labels, Timestamp: time.Now().UTC()}
	if err := _store.createMetric(m); err != nil {
		return errorResponse(err), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(m)}, nil
}

func handleRecordMetric(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name      string            `json:"name"`
		Value     float64           `json:"value"`
		Labels    map[string]string `json:"labels,omitempty"`
		Timestamp int64             `json:"timestamp,omitempty"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.Name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	at := time.Now().UTC()
	if input.Timestamp != 0 {
		at = time.Unix(input.Timestamp, 0).UTC()
	}
	if err := _store.record(input.Name, input.Value, input.Labels, at); err != nil {
		return errorResponse(err), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(map[string]interface{}{
		"name": input.Name, "value": input.Value, "timestamp": at,
	})}, nil
}

func handleGetMetric(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	name := getString(input, "name")
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	m, ok := _store.getMetric(name)
	if !ok {
		return errorResponse(fmt.Errorf("metric not found: %s", name)), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(m)}, nil
}

func handleListMetrics(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	items := _store.listMetrics()
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(map[string]interface{}{"items": items, "total": len(items)})}, nil
}

func handleQueryMetrics(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Expr  string `json:"expr"`
		Start int64  `json:"start"`
		End   int64  `json:"end"`
		Step  int64  `json:"step"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	var start, end time.Time
	if input.Start != 0 {
		start = time.Unix(input.Start, 0).UTC()
	}
	if input.End != 0 {
		end = time.Unix(input.End, 0).UTC()
	}
	series := _store.query(strings.TrimSpace(input.Expr), start, end)
	result := &orionmetric.QueryResult{Series: series, Total: len(series), Page: 1, PageSize: 100}
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(result)}, nil
}

func handleCreateAlert(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name        string            `json:"name"`
		Description string            `json:"description,omitempty"`
		Severity    string            `json:"severity"`
		Metric      string            `json:"metric,omitempty"`
		Condition   string            `json:"condition"`
		Threshold   float64           `json:"threshold"`
		Duration    string            `json:"duration"`
		Labels      map[string]string `json:"labels,omitempty"`
		Annotations map[string]string `json:"annotations,omitempty"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.Name == "" || input.Condition == "" {
		return errorResponse(fmt.Errorf("name and condition are required")), nil
	}
	duration := time.Minute
	if input.Duration != "" {
		var err error
		duration, err = time.ParseDuration(input.Duration)
		if err != nil {
			return errorResponse(fmt.Errorf("invalid duration: %w", err)), nil
		}
	}
	metricName := input.Metric
	if metricName == "" {
		fields := strings.Fields(input.Condition)
		if len(fields) > 0 {
			metricName = fields[0]
		}
	}
	alert := &orionmetric.Alert{ID: fmt.Sprintf("alert-%d", time.Now().UnixNano()), Name: input.Name,
		Description: input.Description, Severity: orionmetric.AlertSeverity(input.Severity), Metric: metricName,
		Condition: input.Condition, Threshold: input.Threshold, Duration: duration,
		State: orionmetric.AlertStateInactive, Labels: input.Labels, Annotations: input.Annotations}
	if err := _store.createAlert(alert); err != nil {
		return errorResponse(err), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(alert)}, nil
}

func handleGetAlert(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}
	alert, ok := _store.getAlert(id)
	if !ok {
		return errorResponse(fmt.Errorf("alert not found: %s", id)), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(alert)}, nil
}

func handleListAlerts(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	items := _store.listAlerts()
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(map[string]interface{}{"items": items, "total": len(items)})}, nil
}

func handleCreateDashboard(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input orionmetric.Dashboard
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.Name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	if input.ID == "" {
		input.ID = fmt.Sprintf("dash-%d", time.Now().UnixNano())
	}
	_store.saveDashboard(input)
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(input)}, nil
}

func handleGetDashboard(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}
	dashboard, ok := _store.getDashboard(id)
	if !ok {
		return errorResponse(fmt.Errorf("dashboard not found: %s", id)), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(dashboard)}, nil
}

func getString(m map[string]interface{}, key string) string {
	if value, ok := m[key].(string); ok {
		return value
	}
	return ""
}
func getEnvOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "OPERATION_FAILED", Message: err.Error()}}
}
func mustMarshal(value interface{}) []byte { b, _ := json.Marshal(value); return b }
