package retry

import (
	"context"
	"math/rand/v2"
	"time"
)

type Policy struct {
	MaxAttempts int
	Initial     time.Duration
	MaxDelay    time.Duration
}

func DefaultPolicy() Policy {
	return Policy{MaxAttempts: 5, Initial: time.Second, MaxDelay: 30 * time.Second}
}

// Do applies the shared exponential-backoff policy. The callback decides
// whether an error is retryable, keeping transport and domain classification
// outside this package.
func Do(ctx context.Context, policy Policy, operation func(context.Context) error, retryable func(error) bool) error {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}
	if policy.Initial <= 0 {
		policy.Initial = time.Second
	}
	if policy.MaxDelay <= 0 {
		policy.MaxDelay = 30 * time.Second
	}
	var err error
	for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
		if err = operation(ctx); err == nil {
			return nil
		}
		if !retryable(err) || attempt == policy.MaxAttempts-1 {
			return err
		}
		delay := policy.Initial * time.Duration(1<<attempt)
		if delay > policy.MaxDelay {
			delay = policy.MaxDelay
		}
		jitter := time.Duration(rand.Int64N(maxInt64(int64(delay/5), 1)))
		timer := time.NewTimer(delay - delay/10 + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
