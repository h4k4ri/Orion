package observability

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Runtime struct {
	service        string
	metricsHandler http.Handler
	traceProvider  *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	metrics        *Metrics
}

// Metrics is the cross-service metric contract. Services can record their
// domain events without inventing metric names or label conventions.
type Metrics struct {
	HTTPDuration      metric.Float64Histogram
	GRPCDuration      metric.Float64Histogram
	NATSPublish       metric.Int64Counter
	NATSConsume       metric.Int64Counter
	OperationDuration metric.Float64Histogram
	NodeReconcile     metric.Int64Counter
	LibvirtErrors     metric.Int64Counter
}

func newMetrics(meter metric.Meter) (*Metrics, error) {
	httpDuration, err := meter.Float64Histogram("orion_http_request_duration_seconds", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	grpcDuration, err := meter.Float64Histogram("orion_grpc_request_duration_seconds", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	natsPublish, err := meter.Int64Counter("orion_nats_publish_total")
	if err != nil {
		return nil, err
	}
	natsConsume, err := meter.Int64Counter("orion_nats_consume_total")
	if err != nil {
		return nil, err
	}
	operationDuration, err := meter.Float64Histogram("orion_operation_duration_seconds", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	nodeReconcile, err := meter.Int64Counter("orion_node_reconcile_total")
	if err != nil {
		return nil, err
	}
	libvirtErrors, err := meter.Int64Counter("orion_libvirt_error_total")
	if err != nil {
		return nil, err
	}
	return &Metrics{HTTPDuration: httpDuration, GRPCDuration: grpcDuration, NATSPublish: natsPublish, NATSConsume: natsConsume, OperationDuration: operationDuration, NodeReconcile: nodeReconcile, LibvirtErrors: libvirtErrors}, nil
}

func New(ctx context.Context, service string) (*Runtime, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"",
			attribute.String("service.name", service),
			attribute.String("service.version", "0.1.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	registry := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return nil, err
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(res),
	)
	metrics, err := newMetrics(meterProvider.Meter("orion"))
	if err != nil {
		return nil, err
	}

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
	)

	if hasOTLPEndpoint() {
		traceExporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, err
		}
		traceProvider = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExporter),
			sdktrace.WithResource(res),
		)
	}

	otel.SetTracerProvider(traceProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return &Runtime{
		service:        service,
		metricsHandler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		traceProvider:  traceProvider,
		meterProvider:  meterProvider,
		metrics:        metrics,
	}, nil
}

func (r *Runtime) Metrics() *Metrics { return r.metrics }

func (r *Runtime) MetricsHandler() http.Handler {
	return r.metricsHandler
}

func (r *Runtime) WrapHTTP(handler http.Handler) http.Handler {
	return otelhttp.NewHandler(
		handler,
		r.service,
		otelhttp.WithSpanNameFormatter(func(_ string, req *http.Request) string {
			if req.Pattern != "" {
				return req.Method + " " + req.Pattern
			}
			return req.Method + " " + req.URL.Path
		}),
	)
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if err := r.meterProvider.Shutdown(ctx); err != nil {
		return err
	}
	return r.traceProvider.Shutdown(ctx)
}

func hasOTLPEndpoint() bool {
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}
