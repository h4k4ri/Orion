package retry

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"
)

// DoHTTP retries transport failures while rebuilding the request body for each
// attempt. HTTP status codes are returned to the caller so each adapter can
// preserve its domain-specific error mapping.
func DoHTTP(ctx context.Context, client *http.Client, method, url string, body []byte, headers http.Header) (*http.Response, error) {
	policy := Policy{MaxAttempts: 3, Initial: 100 * time.Millisecond, MaxDelay: time.Second}
	var response *http.Response
	err := Do(ctx, policy, func(callCtx context.Context) error {
		req, err := http.NewRequestWithContext(callCtx, method, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header = headers.Clone()
		response, err = client.Do(req)
		return err
	}, func(err error) bool {
		return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}
