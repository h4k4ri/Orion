// Package httpapi contains a small, dependency-free HTTP client shared by
// Orion plugins that integrate with HTTP backends.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTPClient: http.DefaultClient}
}

func (c *Client) Do(ctx context.Context, method, path string, input, output interface{}) error {
	if c == nil || c.BaseURL == "" {
		return fmt.Errorf("backend API endpoint is required")
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid backend API endpoint: %w", err)
	}
	relative, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("invalid backend API path: %w", err)
	}
	endpoint := base.ResolveReference(relative)
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode backend request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return fmt.Errorf("create backend request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("X-Auth-Token", c.Token)
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("backend API request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read backend API response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("backend API returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if output != nil && len(data) > 0 {
		if err := json.Unmarshal(data, output); err != nil {
			return fmt.Errorf("decode backend API response: %w", err)
		}
	}
	return nil
}

func (c *Client) Get(ctx context.Context, path string, output interface{}) error {
	return c.Do(ctx, http.MethodGet, path, nil, output)
}

func (c *Client) Post(ctx context.Context, path string, input, output interface{}) error {
	return c.Do(ctx, http.MethodPost, path, input, output)
}

func (c *Client) Put(ctx context.Context, path string, input, output interface{}) error {
	return c.Do(ctx, http.MethodPut, path, input, output)
}

func (c *Client) Patch(ctx context.Context, path string, input, output interface{}) error {
	return c.Do(ctx, http.MethodPatch, path, input, output)
}

func (c *Client) Delete(ctx context.Context, path string) error {
	return c.Do(ctx, http.MethodDelete, path, nil, nil)
}
