package secrets

import (
	"fmt"
	"os"
	"strings"
)

type Provider interface {
	Get(ref string) (string, error)
}

type EnvProvider struct{}

func (EnvProvider) Get(ref string) (string, error) {
	name := strings.TrimPrefix(ref, "env://")
	if name == ref || name == "" {
		return "", fmt.Errorf("invalid env secret reference")
	}
	value, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("secret %s is not set", name)
	}
	return value, nil
}

type FileProvider struct{}

func (FileProvider) Get(ref string) (string, error) {
	path := strings.TrimPrefix(ref, "file://")
	if path == ref || path == "" {
		return "", fmt.Errorf("invalid file secret reference")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func Resolve(ref string) (string, error) {
	switch {
	case strings.HasPrefix(ref, "env://"):
		return (EnvProvider{}).Get(ref)
	case strings.HasPrefix(ref, "file://"):
		return (FileProvider{}).Get(ref)
	default:
		return "", fmt.Errorf("unsupported secret reference %q", ref)
	}
}
