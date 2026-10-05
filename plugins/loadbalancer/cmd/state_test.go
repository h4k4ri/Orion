package main

import (
	"path/filepath"
	"testing"
)

func TestListenerStateRoundTrip(t *testing.T) {
	oldStateFile := stateFile
	oldListeners := listeners
	t.Cleanup(func() { stateFile = oldStateFile; listeners = oldListeners })
	stateFile = filepath.Join(t.TempDir(), "loadbalancer.json")
	listeners = map[string]Listener{
		"public": {
			Name:           "public",
			TLS:            true,
			TLSCertificate: "/etc/orion/public.pem",
			Members:        []Member{{Name: "api", IP: "10.0.0.10", Port: 8080}},
			L7Rules:        []L7Rule{{Name: "api", Path: "/api", BackendName: "public-l7-api"}},
		},
	}
	if err := persistListeners(); err != nil {
		t.Fatal(err)
	}
	listeners = make(map[string]Listener)
	if err := loadListeners(); err != nil {
		t.Fatal(err)
	}
	listener, ok := listeners["public"]
	if !ok || !listener.TLS || listener.TLSCertificate == "" || len(listener.Members) != 1 || len(listener.L7Rules) != 1 {
		t.Fatalf("listener state was not restored: %#v", listeners)
	}
}
