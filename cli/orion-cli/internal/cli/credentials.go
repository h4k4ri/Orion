package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type savedCredentials struct {
	Token     string `json:"token"`
	ProjectID string `json:"project_id,omitempty"`
}

func credentialsPath() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(cfgDir, "orion", "credentials.json")
	return p, nil
}

func loadCredentials() (savedCredentials, error) {
	var creds savedCredentials
	p, err := credentialsPath()
	if err != nil {
		return creds, err
	}
	f, err := os.Open(p)
	if err != nil {
		return creds, err
	}
	defer f.Close()
	return creds, json.NewDecoder(f).Decode(&creds)
}

func saveCredentials(creds savedCredentials) error {
	p, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(creds)
}
