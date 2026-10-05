package secret

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/horizon/orion/sdk/go/plugin"
)

type Secret struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Value     string            `json:"value"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt string            `json:"created_at,omitempty"`
	UpdatedAt string            `json:"updated_at,omitempty"`
}

type SecretVersion struct {
	Version int    `json:"version"`
	Value   string `json:"value"`
}

type baoClient struct {
	baseURL string
	token   string
	mount   string
	client  *http.Client
}

func newClient() (*baoClient, error) {
	address := strings.TrimRight(os.Getenv("ORION_OPENBAO_ADDR"), "/")
	if address == "" {
		return nil, fmt.Errorf("ORION_OPENBAO_ADDR is required")
	}
	token := os.Getenv("ORION_OPENBAO_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("ORION_OPENBAO_TOKEN is required")
	}
	mount := strings.Trim(os.Getenv("ORION_OPENBAO_MOUNT"), "/")
	if mount == "" {
		mount = "secret"
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if os.Getenv("ORION_OPENBAO_TLS_SKIP_VERIFY") == "true" {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit opt-in for lab deployments
	}
	return &baoClient{baseURL: address, token: token, mount: mount, client: &http.Client{Transport: transport}}, nil
}

func (c *baoClient) do(ctx context.Context, method, path string, requestBody interface{}, responseBody interface{}) error {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", c.token)
	if namespace := os.Getenv("ORION_OPENBAO_NAMESPACE"); namespace != "" {
		req.Header.Set("X-Vault-Namespace", namespace)
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("openbao %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(response)))
	}
	if responseBody != nil && len(response) > 0 {
		if err := json.Unmarshal(response, responseBody); err != nil {
			return fmt.Errorf("decode OpenBao response: %w", err)
		}
	}
	return nil
}

func (c *baoClient) dataPath(name string) (string, error) {
	name, err := normalizeName(name)
	if err != nil {
		return "", err
	}
	return "/v1/" + url.PathEscape(c.mount) + "/data/" + escapedPath(name), nil
}

func (c *baoClient) metadataPath(name string) (string, error) {
	if strings.Trim(name, "/") == "" {
		return "/v1/" + url.PathEscape(c.mount) + "/metadata", nil
	}
	name, err := normalizeName(name)
	if err != nil {
		return "", err
	}
	return "/v1/" + url.PathEscape(c.mount) + "/metadata/" + escapedPath(name), nil
}

func normalizeName(name string) (string, error) {
	name = strings.Trim(name, "/")
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "?#") {
		return "", fmt.Errorf("invalid secret name %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid secret name %q", name)
		}
	}
	return name, nil
}

func escapedPath(name string) string {
	parts := strings.Split(name, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func Create(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name     string            `json:"name"`
		Value    string            `json:"value"`
		Metadata map[string]string `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil || input.Name == "" {
		if err == nil {
			err = fmt.Errorf("name is required")
		}
		return failure("INVALID_INPUT", err), nil
	}
	return write(ctx, input.Name, input.Value, input.Metadata, "")
}

func Read(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID      string `json:"id"`
		Version int    `json:"version,omitempty"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return failure("INVALID_INPUT", err), nil
	}
	c, err := newClient()
	if err != nil {
		return failure("OPENBAO_CONFIG_ERROR", err), nil
	}
	path, err := c.dataPath(input.ID)
	if err != nil {
		return failure("INVALID_INPUT", err), nil
	}
	if input.Version > 0 {
		path += "?version=" + strconv.Itoa(input.Version)
	}
	var response kvReadResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return failure("OPENBAO_READ_FAILED", err), nil
	}
	return success(secretFromKV(input.ID, response.Data.Data, response.Data.Metadata)), nil
}

func Update(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID       string            `json:"id"`
		Value    string            `json:"value"`
		Metadata map[string]string `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return failure("INVALID_INPUT", err), nil
	}
	return write(ctx, input.ID, input.Value, input.Metadata, input.ID)
}

func Delete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return failure("INVALID_INPUT", err), nil
	}
	c, err := newClient()
	if err != nil {
		return failure("OPENBAO_CONFIG_ERROR", err), nil
	}
	path, err := c.metadataPath(input.ID)
	if err != nil {
		return failure("INVALID_INPUT", err), nil
	}
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return failure("OPENBAO_DELETE_FAILED", err), nil
	}
	return success(map[string]string{"id": input.ID, "status": "deleted"}), nil
}

func List(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil && len(req.Payload) > 0 {
		return failure("INVALID_INPUT", err), nil
	}
	c, err := newClient()
	if err != nil {
		return failure("OPENBAO_CONFIG_ERROR", err), nil
	}
	path, err := c.metadataPath(strings.Trim(input.Path, "/"))
	if err != nil {
		return failure("INVALID_INPUT", err), nil
	}
	path += "?list=true"
	var response struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return failure("OPENBAO_LIST_FAILED", err), nil
	}
	result := make([]Secret, 0, len(response.Data.Keys))
	for _, key := range response.Data.Keys {
		name := strings.Trim(strings.Trim(input.Path, "/")+"/"+key, "/")
		result = append(result, Secret{ID: name, Name: name})
	}
	return success(result), nil
}

func write(ctx context.Context, name, value string, metadata map[string]string, resultID string) (*plugin.ResourceResponse, error) {
	c, err := newClient()
	if err != nil {
		return failure("OPENBAO_CONFIG_ERROR", err), nil
	}
	path, err := c.dataPath(name)
	if err != nil {
		return failure("INVALID_INPUT", err), nil
	}
	data := map[string]interface{}{"value": value}
	if metadata != nil {
		data["metadata"] = metadata
	}
	var response kvWriteResponse
	if err := c.do(ctx, http.MethodPost, path, map[string]interface{}{"data": data}, &response); err != nil {
		return failure("OPENBAO_WRITE_FAILED", err), nil
	}
	if resultID == "" {
		resultID = name
	}
	return success(secretFromKV(resultID, data, response.Data.Metadata)), nil
}

type kvReadResponse struct {
	Data struct {
		Data     map[string]interface{} `json:"data"`
		Metadata kvMetadata             `json:"metadata"`
	} `json:"data"`
}

type kvWriteResponse struct {
	Data struct {
		Metadata kvMetadata `json:"metadata"`
	} `json:"data"`
}

type kvMetadata struct {
	Version     int    `json:"version"`
	CreatedTime string `json:"created_time"`
	UpdatedTime string `json:"updated_time"`
}

func secretFromKV(id string, data map[string]interface{}, metadata kvMetadata) Secret {
	value, _ := data["value"].(string)
	secretMetadata := map[string]string{}
	if raw, ok := data["metadata"].(map[string]interface{}); ok {
		for key, item := range raw {
			if text, ok := item.(string); ok {
				secretMetadata[key] = text
			}
		}
	} else if raw, ok := data["metadata"].(map[string]string); ok {
		for key, value := range raw {
			secretMetadata[key] = value
		}
	}
	return Secret{ID: id, Name: id, Value: value, Metadata: secretMetadata, CreatedAt: metadata.CreatedTime, UpdatedAt: metadata.UpdatedTime}
}

func success(value interface{}) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: true, Result: marshal(value)}
}
func failure(code string, err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: code, Message: err.Error()}}
}
func marshal(value interface{}) []byte { data, _ := json.Marshal(value); return data }
