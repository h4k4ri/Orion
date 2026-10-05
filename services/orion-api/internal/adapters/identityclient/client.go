package identityclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/retry"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *Client) Validate(ctx context.Context, token string) (authn.Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/auth/tokens", nil)
	if err != nil {
		return authn.Token{}, err
	}

	req.Header.Set("X-Subject-Token", token)
	httpx.SetRequestIDHeader(ctx, req)

	resp, err := retry.DoHTTP(ctx, c.client, req.Method, req.URL.String(), nil, req.Header)
	if err != nil {
		return authn.Token{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return authn.Token{}, fmt.Errorf("identity validate failed with status %d", resp.StatusCode)
	}

	var payload struct {
		Token authn.Token `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return authn.Token{}, err
	}

	return payload.Token, nil
}
