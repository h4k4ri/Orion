package natsx

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
)

type headerCarrier nats.Header

func (c headerCarrier) Get(key string) string { return nats.Header(c).Get(key) }
func (c headerCarrier) Set(key, value string) { nats.Header(c).Set(key, value) }
func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}

// InjectContext puts W3C traceparent/baggage into NATS headers.
func InjectContext(ctx context.Context, headers nats.Header) nats.Header {
	if headers == nil {
		headers = make(nats.Header)
	}
	otel.GetTextMapPropagator().Inject(ctx, headerCarrier(headers))
	return headers
}

// ExtractContext restores the parent span from NATS headers.
func ExtractContext(ctx context.Context, headers nats.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, headerCarrier(headers))
}
