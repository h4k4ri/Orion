package imageclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/image"
	"github.com/horizon/orion/libs/go/kit/retry"
	"github.com/horizon/orion/services/orion-compute/internal/ports"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) GetImage(ctx context.Context, imageID string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/images/"+imageID, nil)
	if err != nil {
		return image.Image{}, err
	}
	httpx.SetRequestIDHeader(ctx, req)

	resp, err := retry.DoHTTP(ctx, c.client, req.Method, req.URL.String(), nil, req.Header)
	if err != nil {
		return image.Image{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return image.Image{}, ports.ErrImageNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return image.Image{}, fmt.Errorf("image lookup failed with status %d", resp.StatusCode)
	}

	var response struct {
		Image image.Image `json:"image"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return image.Image{}, err
	}

	return response.Image, nil
}
