package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestPackagedBuildsNeverSelfUpdate(t *testing.T) {
	old := packaged
	packaged = "AUR"
	defer func() { packaged = old }()
	// A client that fails every request proves the refusal happens before any network use.
	client := &http.Client{Transport: failTransport{t}}
	err := selfUpdate(client)
	if err == nil || !strings.Contains(err.Error(), "package manager") {
		t.Fatalf("packaged build: want package-manager error, got %v", err)
	}
}

type failTransport struct{ t *testing.T }

func (f failTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Fatalf("unexpected network request to %s", r.URL)
	return nil, nil
}
