package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDoRetriesWithBoundedAttempts(t *testing.T) {
	attempts := 0
	err := Do(context.Background(), Policy{MaxAttempts: 3, Initial: time.Nanosecond, MaxDelay: time.Nanosecond}, func(context.Context) error { attempts++; return errors.New("temporary") }, func(error) bool { return true })
	if err == nil || attempts != 3 {
		t.Fatalf("expected three attempts, got %d (%v)", attempts, err)
	}
}
