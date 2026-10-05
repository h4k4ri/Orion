package retry

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Config struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay    time.Duration
	Multiplier  float64
	Jitter      bool
}

var DefaultConfig = Config{
	MaxAttempts:  3,
	InitialDelay: 100 * time.Millisecond,
	MaxDelay:     5 * time.Second,
	Multiplier:  2.0,
	Jitter:       true,
}

func Do(ctx context.Context, cfg Config, fn func() error) error {
	var attempt int
	var delay time.Duration

	if cfg.InitialDelay > 0 {
		delay = cfg.InitialDelay
	}

	for {
		err := fn()
		if err == nil {
			return nil
		}

		if !isRetryable(err) {
			return err
		}

		attempt++
		if attempt >= cfg.MaxAttempts {
			return fmt.Errorf("retry exhausted after %d attempts: %w", attempt, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		delay = nextDelay(delay, cfg)
	}
}

func isRetryable(err error) bool {
	s, ok := status.FromError(err)
	if !ok {
		return false
	}

	switch s.Code() {
	case codes.Unavailable,
		codes.ResourceExhausted,
		codes.Aborted,
		codes.Internal:
		return true
	default:
		return false
	}
}

func nextDelay(current time.Duration, cfg Config) time.Duration {
	delay := time.Duration(float64(current) * cfg.Multiplier)
	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}

	if cfg.Jitter {
		jitter := time.Duration(float64(delay) * (0.5 + rand.Float64()*0.5))
		delay = delay/2 + jitter
	}

	return delay
}
