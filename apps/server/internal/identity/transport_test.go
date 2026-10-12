package identity

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// STU-080: discovery, JWKS, token requests and redirects cannot downgrade to arbitrary HTTP.
func TestProviderURLBoundary(t *testing.T) {
	for _, raw := range []string{"\x00", "http://remote.example/keys", "ftp://localhost/token", "https://user:secret@issuer.example/keys"} {
		if endpointURL(raw, true) {
			t.Fatal("unsafe provider URL")
		}
	}
	if !endpointURL("https://issuer.example/authorize?prompt=login", false) {
		t.Fatal("valid endpoint query rejected")
	}
	u, _ := url.Parse("http://remote.example/keys")
	if _, err := (transport{}).RoundTrip(&http.Request{URL: u}); !errors.Is(err, ErrConfig) {
		t.Fatal(err)
	}
}
