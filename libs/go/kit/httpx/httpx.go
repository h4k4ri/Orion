package httpx

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/horizon/orion/libs/go/kit/apierror"
	"github.com/horizon/orion/libs/go/kit/ids"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type contextKey string

const requestIDKey contextKey = "request_id"

func DecodeJSON(r *http.Request, into any) error {
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(into)
}

func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = ids.New("req")
		}

		w.Header().Set("X-Request-Id", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		if span := trace.SpanFromContext(ctx); span.IsRecording() {
			span.SetAttributes(attribute.String("orion.request_id", requestID))
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}

	return ""
}

func RequestIDFromContextOrNew(ctx context.Context) string {
	if requestID := RequestIDFromContext(ctx); requestID != "" {
		return requestID
	}
	return ids.New("req")
}

// WithRequestID lets trusted adapters bind a durable idempotency key to the
// request identity used by downstream stores.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

func SetRequestIDHeader(ctx context.Context, req *http.Request) {
	if requestID := RequestIDFromContext(ctx); requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}
}

func Method(next http.HandlerFunc, wanted string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != wanted {
			apierror.Write(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}

		next(w, r)
	}
}
