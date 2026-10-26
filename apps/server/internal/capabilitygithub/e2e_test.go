//go:build e2e

package capabilitygithub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// STU-445: only a loopback http origin is accepted, and the client reaches it.
func TestSTU445E2EFixtureOrigin(t *testing.T) {
	for _, bad := range []string{"https://127.0.0.1:1", "http://localhost:1", "http://127.0.0.1", "http://127.0.0.1:1/x", "http://127.0.0.1:1?q=1", "http://u@127.0.0.1:1", "%"} {
		if configureE2E(bad) == nil {
			t.Fatal("accepted", bad)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":42,"full_name":"local/store"}`))
	}))
	defer server.Close()
	if err := configureE2E(server.URL); err != nil {
		t.Fatal(err)
	}
	defer func() { apiOrigin, e2eClient = "https://api.github.com", nil }()
	s := session{client: NewClient().client, repo: Repository{Token: "t"}}
	var v repoInfo
	if err := s.get(context.Background(), "/repos/local/store", &v); err != nil || v.FullName != "local/store" {
		t.Fatal(v, err)
	}
	if _, err := s.client.Get("http://127.0.0.2:1/"); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatal("other hosts", err)
	}
}
