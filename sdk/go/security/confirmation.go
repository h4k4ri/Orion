package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type ConfirmationToken struct {
	Token     string
	ExpiresAt time.Time
	Used      bool
}

type ConfirmationStore interface {
	Store(ctx context.Context, token *ConfirmationToken) error
	Get(ctx context.Context, token string) (*ConfirmationToken, error)
	MarkUsed(ctx context.Context, token string) error
}

type InMemoryConfirmationStore struct {
	mu     sync.RWMutex
	tokens map[string]*ConfirmationToken
}

func NewInMemoryConfirmationStore() *InMemoryConfirmationStore {
	return &InMemoryConfirmationStore{
		tokens: make(map[string]*ConfirmationToken),
	}
}

func (s *InMemoryConfirmationStore) Store(ctx context.Context, token *ConfirmationToken) error {
	if token == nil || token.Token == "" {
		return fmt.Errorf("confirmation token is required")
	}
	if token.ExpiresAt.IsZero() || !token.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("confirmation token must have a future expiry")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tokens[token.Token]; exists {
		return fmt.Errorf("confirmation token already exists")
	}
	copy := *token
	s.tokens[token.Token] = &copy
	return nil
}

func (s *InMemoryConfirmationStore) Get(ctx context.Context, token string) (*ConfirmationToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tokens[token]
	if !ok {
		return nil, fmt.Errorf("confirmation token not found")
	}
	if t.Used {
		return nil, fmt.Errorf("confirmation token already used")
	}
	if time.Now().After(t.ExpiresAt) {
		return nil, fmt.Errorf("confirmation token expired")
	}
	copy := *t
	return &copy, nil
}

func (s *InMemoryConfirmationStore) MarkUsed(ctx context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[token]
	if !ok {
		return fmt.Errorf("confirmation token not found")
	}
	if t.Used {
		return fmt.Errorf("confirmation token already used")
	}
	if time.Now().After(t.ExpiresAt) {
		return fmt.Errorf("confirmation token expired")
	}
	t.Used = true
	return nil
}

func GenerateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate secure token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

func GenerateConfirmationCode(length int) (string, error) {
	if length < 6 || length > 10 {
		length = 8
	}

	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate confirmation code: %w", err)
	}

	code := binary.BigEndian.Uint32(bytes) % uint32(pow10(length))
	return fmt.Sprintf("%0*d", length, code), nil
}

func pow10(n int) int {
	result := 1
	for i := 0; i < n; i++ {
		result *= 10
	}
	return result
}

type DestructiveOperation struct {
	ID          string
	Description string
	ResourceID  string
	ResourceKind string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Confirmed   bool
	ConfirmedAt *time.Time
	ConfirmedBy string
}

type DestructiveOperationStore interface {
	Store(ctx context.Context, op *DestructiveOperation) error
	Get(ctx context.Context, id string) (*DestructiveOperation, error)
	Confirm(ctx context.Context, id string, by string) error
	Cancel(ctx context.Context, id string) error
	Expire(ctx context.Context, id string) error
}

func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func ConstantTimeCompareByte(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

type IDGenerator struct {
	prefix string
	seq    uint64
}

func NewIDGenerator(prefix string) *IDGenerator {
	return &IDGenerator{
		prefix: prefix,
		seq:    0,
	}
}

func (g *IDGenerator) Next() string {
	seq := atomic.AddUint64(&g.seq, 1)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, seq)

	encoded := make([]byte, base64.URLEncoding.EncodedLen(len(buf)))
	base64.URLEncoding.Encode(encoded, buf)

	return fmt.Sprintf("%s-%s", g.prefix, string(encoded[:8]))
}

type RequestIDGenerator struct{}

func (g RequestIDGenerator) NewRequestID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return fmt.Sprintf("req-%x", bytes)
}

type CorrelationIDKey struct{}

func NewCorrelationID(ctx context.Context) context.Context {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return context.WithValue(ctx, CorrelationIDKey{}, fmt.Sprintf("corr-%x", bytes))
}

func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(CorrelationIDKey{}).(string); ok {
		return id
	}
	return ""
}
