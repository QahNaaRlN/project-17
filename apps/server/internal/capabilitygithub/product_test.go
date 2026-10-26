//go:build !e2e

package capabilitygithub

import (
	"net/http"
	"testing"
)

// STU-445/459: a product build has no fixture: the client is pinned to GitHub.
func TestSTU445ProductBuildPinsGitHub(t *testing.T) {
	if apiOrigin != "https://api.github.com" || e2eClient != nil {
		t.Fatal("product build can reach another GitHub origin")
	}
	transport := NewClient().client.Transport.(*http.Transport)
	if transport.TLSClientConfig.ServerName != "api.github.com" || transport.Proxy != nil {
		t.Fatal(transport)
	}
}
