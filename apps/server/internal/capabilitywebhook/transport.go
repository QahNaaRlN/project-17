package capabilitywebhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	DeliveryID, BodyHash, KeyID string
	Body                        []byte
}
type Outcome struct {
	Code                    string
	HTTPStatus              int
	Retryable, Acknowledged bool
	RetryAfter              time.Duration
}
type Sender interface {
	Send(context.Context, Endpoint, Message) Outcome
}
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type Transport struct {
	Catalog  *Catalog
	Resolver Resolver
	Now      func() time.Time
	dial     func(context.Context, string, string) (net.Conn, error)
	roots    *x509.CertPool
}

var errAddressBlocked = errors.New("address blocked")

func Hash(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func signature(key []byte, id, timestamp, deliveryID, hash string) string {
	h := hmac.New(sha256.New, key)
	_, _ = fmt.Fprintf(h, "v1\n%s\n%s\n%s\n%s\n", id, timestamp, deliveryID, hash)
	return "v1=" + hex.EncodeToString(h.Sum(nil))
}

// Verify is also used by the reference durable receiver. Callers must separately
// bind the endpoint/project/environment and persist before acknowledging.
func Verify(key []byte, h http.Header, body []byte, now time.Time) bool {
	for _, name := range []string{"X-CMS-Delivery-Id", "X-CMS-Key-Id", "X-CMS-Timestamp", "X-CMS-Body-Hash", "X-CMS-Signature"} {
		if len(h.Values(name)) != 1 {
			return false
		}
	}
	timestamp := h.Get("X-CMS-Timestamp")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || seconds < now.Add(-300*time.Second).Unix() || seconds > now.Add(300*time.Second).Unix() || h.Get("X-CMS-Body-Hash") != Hash(body) {
		return false
	}
	expected := signature(key, h.Get("X-CMS-Key-Id"), timestamp, h.Get("X-CMS-Delivery-Id"), h.Get("X-CMS-Body-Hash"))
	return hmac.Equal([]byte(expected), []byte(h.Get("X-CMS-Signature")))
}

// Public excludes special-use ranges even where IsGlobalUnicast returns true.
func Public(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Is4In6() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range blocked {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

var blocked = func() []netip.Prefix {
	values := []string{"0.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8"}
	var out []netip.Prefix
	for _, v := range values {
		out = append(out, netip.MustParsePrefix(v))
	}
	return out
}()

func (t *Transport) Send(ctx context.Context, endpoint Endpoint, message Message) Outcome {
	if !endpoint.HasKey(message.KeyID) {
		return Outcome{Code: "KEY_UNAVAILABLE"}
	}
	loopback := t.Catalog != nil && t.Catalog.loopback
	if !ValidURL(endpoint.URL, loopback) {
		return Outcome{Code: "ADDRESS_BLOCKED"}
	}
	if len(message.Body) > 640<<10 || Hash(message.Body) != message.BodyHash {
		return Outcome{Code: "BODY_INVALID"}
	}
	u, _ := url.Parse(endpoint.URL)
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resolver := t.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	var addresses []netip.Addr
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil {
		addresses = []netip.Addr{ip}
	} else {
		var err error
		addresses, err = resolver.LookupNetIP(ctx, "ip", u.Hostname())
		if err != nil {
			return Outcome{Code: "NETWORK_UNCERTAIN", Retryable: true}
		}
	}
	if len(addresses) == 0 {
		return Outcome{Code: "ADDRESS_BLOCKED"}
	}
	for _, ip := range addresses {
		if !(loopback && u.Scheme == "http" && ip.IsLoopback()) && !Public(ip) {
			return Outcome{Code: "ADDRESS_BLOCKED"}
		}
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	// Each request gets a fresh transport: no proxy, cookies, pooled DNS decision,
	// redirect, or inherited CMS credentials can cross this boundary.
	dial := t.dial
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	transport := &http.Transport{MaxResponseHeaderBytes: 32 << 10, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname(), RootCAs: t.roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		var last error
		for _, ip := range addresses {
			conn, err := dial(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if err != nil {
				last = err
				continue
			}
			remote, _, err := net.SplitHostPort(conn.RemoteAddr().String())
			remoteIP, parseErr := netip.ParseAddr(remote)
			if err != nil || parseErr != nil || remoteIP.Unmap() != ip.Unmap() {
				_ = conn.Close()
				return nil, errAddressBlocked
			}
			return conn, nil
		}
		return nil, last
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, bytes.NewReader(message.Body))
	if err != nil {
		return Outcome{Code: "ADDRESS_BLOCKED"}
	}
	stamp := strconv.FormatInt(now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CMS-Delivery-Id", message.DeliveryID)
	req.Header.Set("X-CMS-Key-Id", message.KeyID)
	req.Header.Set("X-CMS-Timestamp", stamp)
	req.Header.Set("X-CMS-Body-Hash", message.BodyHash)
	req.Header.Set("X-CMS-Signature", signature(endpoint.keys[message.KeyID], message.KeyID, stamp, message.DeliveryID, message.BodyHash))
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errAddressBlocked) {
			return Outcome{Code: "ADDRESS_BLOCKED"}
		}
		return Outcome{Code: "NETWORK_UNCERTAIN", Retryable: true}
	}
	defer resp.Body.Close()
	o := Outcome{HTTPStatus: resp.StatusCode}
	if resp.StatusCode/100 != 2 {
		o.Retryable = resp.StatusCode == 408 || resp.StatusCode == 425 || resp.StatusCode == 429 || resp.StatusCode >= 500 && resp.StatusCode <= 599
		o.Code = "HTTP_PERMANENT"
		if o.Retryable {
			o.Code = "HTTP_RETRYABLE"
		}
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			o.RetryAfter = retryAfter(resp.Header.Get("Retry-After"), now())
		}
		return o
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	var ack struct {
		DeliveryID string `json:"deliveryId"`
		BodyHash   string `json:"bodyHash"`
		Status     string `json:"status"`
	}
	if err != nil || len(b) > 4096 || uniqueJSON(json.NewDecoder(bytes.NewReader(b))) != nil || json.Unmarshal(b, &ack) != nil || ack.DeliveryID != message.DeliveryID || ack.BodyHash != message.BodyHash || ack.Status != "stored" {
		o.Code = "ACK_INVALID"
		o.Retryable = true
		return o
	}
	o.Acknowledged = true
	o.Code = "STORED"
	return o
}

func retryAfter(s string, now time.Time) time.Duration {
	var delay time.Duration
	if seconds, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && seconds >= 0 {
		if seconds > 7200 {
			seconds = 7200
		}
		delay = time.Duration(seconds) * time.Second
	} else if date, err := http.ParseTime(s); err == nil {
		delay = date.Sub(now)
	} else {
		return 0
	}
	if delay < 5*time.Second {
		delay = 5 * time.Second
	}
	if delay > 2*time.Hour {
		delay = 2 * time.Hour
	}
	return delay
}
