package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type Config struct {
	CaCertFile     string
	ServerCertFile string
	ServerKeyFile  string
	ClientCertFile string
	ClientKeyFile  string
	MinVersion     uint16
}

func LoadServerConfig(cfg Config) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		MinVersion: cfg.MinVersion,
	}

	if cfg.MinVersion == 0 {
		tlsCfg.MinVersion = tls.VersionTLS12
	}

	if cfg.CaCertFile != "" {
		caCert, err := os.ReadFile(cfg.CaCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}

		tlsCfg.ClientCAs = caCertPool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	if cfg.ServerCertFile != "" && cfg.ServerKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ServerCertFile, cfg.ServerKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load server cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}

func LoadClientConfig(cfg Config) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		MinVersion: cfg.MinVersion,
	}

	if cfg.MinVersion == 0 {
		tlsCfg.MinVersion = tls.VersionTLS12
	}

	if cfg.CaCertFile != "" {
		caCert, err := os.ReadFile(cfg.CaCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
		tlsCfg.RootCAs = caCertPool
	}

	if cfg.ClientCertFile != "" && cfg.ClientKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}

func LoadTLSConfig(cfg *TLSConfig) (credentials.TransportCredentials, error) {
	if cfg == nil {
		return nil, fmt.Errorf("TLS config is nil")
	}

	if cfg.CaCert == "" && cfg.ClientCert == "" {
		return insecure.NewCredentials(), nil
	}

	var certPool *x509.CertPool
	if cfg.CaCert != "" {
		caCert, err := os.ReadFile(cfg.CaCert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}
		certPool = x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
	}

	tlsCfg := &tls.Config{
		RootCAs: certPool,
	}

	if cfg.ClientCert != "" && cfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCert, cfg.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return credentials.NewTLS(tlsCfg), nil
}

type TLSConfig struct {
	CaCert     string
	ClientCert string
	ClientKey  string
}
