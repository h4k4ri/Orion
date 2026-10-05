package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// ClientCredentialsFromEnv enables mTLS when all certificate settings are
// present. Leaving them unset keeps local development on insecure transport.
func ClientCredentialsFromEnv() (credentials.TransportCredentials, error) {
	certFile, keyFile := os.Getenv("ORION_TLS_CERT_FILE"), os.Getenv("ORION_TLS_KEY_FILE")
	if certFile == "" && keyFile == "" {
		return nil, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("ORION_TLS_CERT_FILE and ORION_TLS_KEY_FILE must be set together")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS client certificate: %w", err)
	}
	pool, err := loadRoots(os.Getenv("ORION_TLS_CA_FILE"))
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, RootCAs: pool, ServerName: os.Getenv("ORION_TLS_SERVER_NAME")}), nil
}

func ServerCredentialsFromEnv() (credentials.TransportCredentials, error) {
	certFile, keyFile := os.Getenv("ORION_TLS_CERT_FILE"), os.Getenv("ORION_TLS_KEY_FILE")
	if certFile == "" && keyFile == "" {
		return nil, nil
	}
	if certFile == "" || keyFile == "" || os.Getenv("ORION_TLS_CA_FILE") == "" {
		return nil, fmt.Errorf("mTLS server requires ORION_TLS_CERT_FILE, ORION_TLS_KEY_FILE and ORION_TLS_CA_FILE")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS server certificate: %w", err)
	}
	pool, err := loadRoots(os.Getenv("ORION_TLS_CA_FILE"))
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}), nil
}

func loadRoots(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read TLS CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("TLS CA file contains no certificates")
	}
	return pool, nil
}
