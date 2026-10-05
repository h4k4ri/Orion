package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorInfo  `json:"error,omitempty"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PaginatedResponse struct {
	Items      interface{} `json:"items"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"pageSize"`
	HasMore    bool        `json:"hasMore"`
}

type HandlerFunc func(ctx context.Context, w http.ResponseWriter, r *http.Request) error

type ErrorHandlerFunc func(ctx context.Context, err error) (int, *ErrorInfo)

func WrapHandler(h HandlerFunc, errHandler ErrorHandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if err := h(ctx, w, r); err != nil {
			code := http.StatusInternalServerError
			errInfo := &ErrorInfo{Code: "INTERNAL_ERROR", Message: err.Error()}

			if errHandler != nil {
				code, errInfo = errHandler(ctx, err)
			}

			WriteError(w, code, errInfo.Code, errInfo.Message)
		}
	}
}

func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
	})
}

func WriteSuccess(w http.ResponseWriter, status int, data interface{}) {
	WriteJSON(w, status, Response{
		Success: true,
		Data:    data,
	})
}

func WritePaginated(w http.ResponseWriter, items interface{}, total, page, pageSize int) {
	hasMore := (page * pageSize) < total
	WriteJSON(w, http.StatusOK, PaginatedResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		HasMore:  hasMore,
	})
}

func ParseJSONBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	return io.ReadAll(r.Body)
}

func GetPathParam(r *http.Request, name string) string {
	return chi.URLParam(r, name)
}

func GetQueryParam(r *http.Request, name string) string {
	return r.URL.Query().Get(name)
}

func GetQueryInt(r *http.Request, name string, defaultVal int) int {
	if val := r.URL.Query().Get(name); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func GetQueryBool(r *http.Request, name string, defaultVal bool) bool {
	if val := r.URL.Query().Get(name); val != "" {
		if val == "true" || val == "1" || val == "yes" {
			return true
		}
		if val == "false" || val == "0" || val == "no" {
			return false
		}
	}
	return defaultVal
}

type Middleware func(http.Handler) http.Handler

func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

func RecoveryMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					WriteError(w, http.StatusInternalServerError, "PANIC", "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func RequestIDMiddleware(requestIDHeader string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := r.Header.Get(requestIDHeader)
			if reqID == "" {
				// Generate one if not provided
				ctx := context.WithValue(r.Context(), "request_id", generateRequestID())
				w.Header().Set(requestIDHeader, getRequestIDFromContext(ctx))
				r = r.WithContext(ctx)
			} else {
				ctx := context.WithValue(r.Context(), "request_id", reqID)
				w.Header().Set(requestIDHeader, reqID)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func ContentTypeJSON() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodDelete {
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					WriteError(w, http.StatusUnsupportedMediaType, "INVALID_CONTENT_TYPE", "Content-Type must be application/json")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

type contextKey string

const requestIDKey contextKey = "request_id"

func generateRequestID() string {
	// Simple implementation - in production use uuid or similar
	return "req-" + randomString(16)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}

func getRequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}
