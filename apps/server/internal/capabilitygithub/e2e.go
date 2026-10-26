//go:build e2e

package capabilitygithub

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// The e2e build of the §2 scenario reads GitHub facts from a loopback fixture
// (STU-445). Product builds do not compile this file.
func init() {
	if err := configureE2E(os.Getenv("CMS_E2E_GITHUB_URL")); err != nil {
		panic(err)
	}
}

func configureE2E(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return errors.New("CMS_E2E_GITHUB_URL must be http://127.0.0.1:<port>")
	}
	host := u.Host
	apiOrigin = "http://" + host
	e2eClient = func() *http.Client {
		dial := func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != host {
				return nil, errors.New("address blocked")
			}
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		// Keep-alive on loopback: closing each Node.js connection resets sockets on Windows.
		return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: dial},
			CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect blocked") }}
	}
	return nil
}
