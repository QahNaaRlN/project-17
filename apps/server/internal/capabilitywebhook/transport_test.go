package capabilitywebhook

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

var testProject = uuid.MustParse("36b984a7-95a0-450a-b887-3cde9d722742")

func TestSignatureGoldenVector(t *testing.T) {
	body := []byte(`{"event":"golden"}`)
	hash := Hash(body)
	if hash != "sha256:1000135f9acd10ca8b475df42a6f35753314700f512e8116908865e6a56c9e72" || signature([]byte(strings.Repeat("k", 32)), "key1", "1792233600", testProject.String(), hash) != "v1=e00af5db60f28e8753da205e97be3a63ff53729cee18afebd50ef2aa6a6829d8" {
		t.Fatal("cross-language signature vector")
	}
}

func configJSON(url string) []byte {
	b, _ := json.Marshal(map[string]any{"endpoints": []any{map[string]any{"endpointId": "receiver", "projectId": testProject, "environment": "staging", "url": url, "activeKeyId": "key_1", "keys": map[string]string{"key_1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))}}}})
	return b
}
func TestConfig(t *testing.T) {
	b := configJSON("https://receiver.example/context")
	c, err := Parse(b, false)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := c.Find(testProject, "staging", "receiver")
	if !ok || !e.HasKey("key_1") || e.ActiveKeyID() != "key_1" || !strings.HasPrefix(e.Fingerprint, "sha256:") {
		t.Fatal(e)
	}
	public, _ := json.Marshal(e)
	if strings.Contains(string(public), "keys") || strings.Contains(string(public), "activeKey") || !strings.Contains(c.String(), "redacted") || !strings.Contains(e.String(), "redacted") {
		t.Fatal("secret projection")
	}
	if len(c.Endpoints(uuid.New(), "staging")) != 0 || len(c.All()) != 1 {
		t.Fatal("scope")
	}
	var absent *Catalog
	if len(absent.All()) != 0 || len(absent.Endpoints(testProject, "staging")) != 0 {
		t.Fatal("nil catalog")
	}
	for _, bad := range []string{
		`{}`, `null`, `{"endpoints":null}`, `{"endpoints":[]}`, `{"endpoints":[],"unknown":true}`, `{"endpoints":[],"endpoints":[]}`, `{"endpoints":[]} {}`,
		strings.Replace(string(b), `"key_1":`, `"key_1":"a","key_1":`, 1),
		strings.Replace(string(b), `"activeKeyId":"key_1"`, `"activeKeyId":"missing"`, 1),
		strings.Replace(string(b), `"environment":"staging"`, `"environment":""`, 1),
		strings.Replace(string(b), `"projectId":"`+testProject.String()+`"`, `"projectId":"bad"`, 1),
		strings.Replace(string(b), base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), "bad", 1),
		strings.Replace(string(b), `"endpointId":"receiver"`, `"endpointId":"bad/value"`, 1),
		strings.Repeat(" ", (1<<20)+1),
	} {
		if _, err := Parse([]byte(bad), false); err != ErrConfig {
			t.Fatalf("accepted %s", bad[:min(len(bad), 80)])
		}
	}
	var root map[string][]map[string]any
	_ = json.Unmarshal(b, &root)
	root["endpoints"] = append(root["endpoints"], root["endpoints"][0])
	duplicate, _ := json.Marshal(root)
	if _, err := Parse(duplicate, false); err != ErrConfig {
		t.Fatal("duplicate endpoint")
	}
	file := filepath.Join(t.TempDir(), "private.json")
	if err = os.WriteFile(file, b, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, flag string
		valid      bool
	}{{"", "", true}, {file, "false", true}, {file, "true", true}, {file, "invalid", false}, {file + "missing", "", false}} {
		_, err := FromEnv(func(name string) string {
			if strings.HasSuffix(name, "CONFIG_FILE") {
				return tc.path
			}
			return tc.flag
		})
		if (err == nil) != tc.valid {
			t.Fatal(tc, err)
		}
		if err != nil && strings.Contains(err.Error(), file) {
			t.Fatal("private path exposed")
		}
	}
}
func TestURLAndAddressBoundary(t *testing.T) {
	for _, url := range []string{"http://receiver.example/path", "https://127.0.0.1/", "https://[::1]/", "https://a.example:444/", "https://user:pass@a.example/", "https://a.example/?key=x", "https://a.example/#x", "https://a.example/?", "https://a.example./", "https://localhost/", "https://a.example/\\x", ""} {
		if ValidURL(url, false) {
			t.Fatal(url)
		}
	}
	if !ValidURL("https://receiver.example:443/path", false) || !ValidURL("http://127.0.0.1:18000/context", true) || !ValidURL("http://[::1]:18000/context", true) || ValidURL("http://localhost:18000", true) {
		t.Fatal("url policy")
	}
	for _, ip := range []string{"0.0.0.0", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.1.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "::", "::1", "::ffff:8.8.8.8", "64:ff9b::808:808", "2001:db8::1", "2002:808:808::1", "fc00::1", "fe80::1", "ff02::1"} {
		if Public(netip.MustParseAddr(ip)) {
			t.Fatal(ip)
		}
	}
	if !Public(netip.MustParseAddr("8.8.8.8")) || !Public(netip.MustParseAddr("2606:4700:4700::1111")) || Public(netip.Addr{}) {
		t.Fatal("public boundary")
	}
}
func TestSignedDeliveryAndReceipts(t *testing.T) {
	var mu sync.Mutex
	var mode string
	var first []byte
	var stamps []string
	key := []byte(strings.Repeat("k", 32))
	now := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		b, _ := io.ReadAll(r.Body)
		if !Verify(key, r.Header, b, now) || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("signature or isolation")
		}
		stamps = append(stamps, r.Header.Get("X-CMS-Timestamp"))
		if first == nil {
			first = b
		} else if string(first) != string(b) {
			t.Error("mutable body")
		}
		switch mode {
		case "redirect":
			w.Header().Set("Location", "http://127.0.0.1:1/leak")
			w.WriteHeader(302)
		case "retry":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(503)
		case "permanent":
			w.WriteHeader(401)
		case "missing":
			w.WriteHeader(204)
		case "large":
			_, _ = w.Write([]byte(strings.Repeat("a", 4097)))
		case "wrong":
			_, _ = w.Write([]byte(`{"deliveryId":"wrong","bodyHash":"wrong","status":"stored"}`))
		case "duplicate":
			_, _ = w.Write([]byte(`{"status":"stored","status":"stored"}`))
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{"deliveryId": r.Header.Get("X-CMS-Delivery-Id"), "bodyHash": r.Header.Get("X-CMS-Body-Hash"), "status": "stored", "ignored": "not a CMS command"})
		}
	}))
	defer server.Close()
	c, err := Parse(configJSON(server.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := c.Find(testProject, "staging", "receiver")
	transport := &Transport{Catalog: c, Now: func() time.Time { mu.Lock(); defer mu.Unlock(); return now }}
	msg := Message{DeliveryID: uuid.NewString(), KeyID: "key_1", Body: []byte(`{"immutable":true}`)}
	msg.BodyHash = Hash(msg.Body)
	for _, tc := range []struct {
		mode, code string
		retry, ack bool
	}{{"", "STORED", false, true}, {"redirect", "HTTP_PERMANENT", false, false}, {"retry", "HTTP_RETRYABLE", true, false}, {"permanent", "HTTP_PERMANENT", false, false}, {"missing", "ACK_INVALID", true, false}, {"large", "ACK_INVALID", true, false}, {"wrong", "ACK_INVALID", true, false}, {"duplicate", "ACK_INVALID", true, false}} {
		mu.Lock()
		mode = tc.mode
		mu.Unlock()
		o := transport.Send(context.Background(), e, msg)
		if o.Code != tc.code || o.Retryable != tc.retry || o.Acknowledged != tc.ack {
			t.Fatal(tc, o)
		}
		if tc.mode == "retry" && o.RetryAfter != 2*time.Minute {
			t.Fatal(o)
		}
		mu.Lock()
		now = now.Add(time.Second)
		mu.Unlock()
	}
	mu.Lock()
	sameStamp := stamps[0] == stamps[1]
	mu.Unlock()
	if sameStamp {
		t.Fatal("timestamp not refreshed")
	}
	msg.KeyID = "missing"
	if transport.Send(context.Background(), e, msg).Code != "KEY_UNAVAILABLE" {
		t.Fatal("key fallback")
	}
	msg.KeyID = "key_1"
	msg.BodyHash = "wrong"
	if transport.Send(context.Background(), e, msg).Code != "BODY_INVALID" {
		t.Fatal("invalid body")
	}
	msg.BodyHash = Hash(msg.Body)
	if (&Transport{}).Send(context.Background(), e, msg).Code != "ADDRESS_BLOCKED" {
		t.Fatal("loopback default")
	}
	server.Close()
	if transport.Send(context.Background(), e, msg).Code != "NETWORK_UNCERTAIN" {
		t.Fatal("network")
	}
}

type fakeResolver struct {
	addresses []netip.Addr
	err       error
}

func (r fakeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addresses, r.err
}
func TestMixedDNSAndVerification(t *testing.T) {
	c, _ := Parse(configJSON("https://receiver.example"), false)
	e, _ := c.Find(testProject, "staging", "receiver")
	msg := Message{DeliveryID: uuid.NewString(), KeyID: "key_1", Body: []byte(`{}`)}
	msg.BodyHash = Hash(msg.Body)
	for _, ips := range [][]netip.Addr{nil, {netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, {netip.MustParseAddr("::ffff:127.0.0.1")}} {
		o := (&Transport{Catalog: c, Resolver: fakeResolver{addresses: ips}}).Send(context.Background(), e, msg)
		if o.Code != "ADDRESS_BLOCKED" {
			t.Fatal(o)
		}
	}
	if (&Transport{Catalog: c, Resolver: fakeResolver{err: io.EOF}}).Send(context.Background(), e, msg).Code != "NETWORK_UNCERTAIN" {
		t.Fatal("dns error")
	}
	now := time.Now()
	key := []byte(strings.Repeat("k", 32))
	h := http.Header{}
	h.Set("X-CMS-Key-Id", "k")
	h.Set("X-CMS-Delivery-Id", msg.DeliveryID)
	h.Set("X-CMS-Body-Hash", msg.BodyHash)
	for _, delta := range []time.Duration{-301 * time.Second, 301 * time.Second} {
		stamp := strconv.FormatInt(now.Add(delta).Unix(), 10)
		h.Set("X-CMS-Timestamp", stamp)
		h.Set("X-CMS-Signature", signature(key, "k", stamp, msg.DeliveryID, msg.BodyHash))
		if Verify(key, h, msg.Body, now) {
			t.Fatal("timestamp")
		}
	}
	h.Set("X-CMS-Timestamp", "0")
	h.Set("X-CMS-Signature", "v1=bad")
	if Verify(key, h, msg.Body, now) {
		t.Fatal("bad signature")
	}
	h.Add("X-CMS-Key-Id", "duplicate")
	if Verify(key, h, msg.Body, now) {
		t.Fatal("duplicate header")
	}
	for _, tc := range []struct {
		s        string
		min, max time.Duration
	}{{"", 0, 0}, {"0", 5 * time.Second, 5 * time.Second}, {"999999999999", 2 * time.Hour, 2 * time.Hour}, {now.Add(time.Minute).UTC().Format(http.TimeFormat), 59 * time.Second, time.Minute}, {now.Add(-time.Hour).UTC().Format(http.TimeFormat), 5 * time.Second, 5 * time.Second}} {
		d := retryAfter(tc.s, now)
		if d < tc.min || d > tc.max {
			t.Fatal(tc, d)
		}
	}
}

type pinnedConn struct {
	net.Conn
	peer net.Addr
}

func (c pinnedConn) RemoteAddr() net.Addr { return c.peer }
func TestPublicDNSPinnedDialTLSAndRebinding(t *testing.T) {
	var mu sync.Mutex
	var host, sni string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		host = r.Host
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"deliveryId": r.Header.Get("X-CMS-Delivery-Id"), "bodyHash": r.Header.Get("X-CMS-Body-Hash"), "status": "stored"})
	}))
	server.TLS = &tls.Config{GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		sni = info.ServerName
		mu.Unlock()
		return nil, nil
	}}
	server.StartTLS()
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	catalog, err := Parse(configJSON("https://example.com/context"), false)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := catalog.Find(testProject, "staging", "receiver")
	msg := Message{DeliveryID: uuid.NewString(), KeyID: "key_1", Body: []byte(`{}`)}
	msg.BodyHash = Hash(msg.Body)
	peer := netip.MustParseAddr("8.8.8.8")
	var dials atomic.Int32
	transport := &Transport{Catalog: catalog, Resolver: fakeResolver{addresses: []netip.Addr{peer}}, roots: roots, dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		if address != "8.8.8.8:443" {
			t.Error("DNS result was not pinned", address)
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		if err != nil {
			return nil, err
		}
		return pinnedConn{Conn: conn, peer: &net.TCPAddr{IP: net.IP(peer.AsSlice()), Port: 443}}, nil
	}}
	o := transport.Send(context.Background(), endpoint, msg)
	mu.Lock()
	gotHost, gotSNI := host, sni
	mu.Unlock()
	if !o.Acknowledged || gotHost != "example.com" || gotSNI != "example.com" {
		t.Fatal(o, gotHost, gotSNI)
	}
	// A new DNS decision is required on every attempt. A public/private mixed
	// response must fail before any connection, even after a successful attempt.
	transport.Resolver = fakeResolver{addresses: []netip.Addr{peer, netip.MustParseAddr("127.0.0.1")}}
	if o := transport.Send(context.Background(), endpoint, msg); o.Code != "ADDRESS_BLOCKED" || dials.Load() != 1 {
		t.Fatal(o, dials.Load())
	}
	transport.Resolver = fakeResolver{addresses: []netip.Addr{peer}}
	transport.roots = nil
	if o := transport.Send(context.Background(), endpoint, msg); o.Acknowledged || !o.Retryable {
		t.Fatal("TLS trust bypass", o)
	}
	transport.roots = roots
	endpoint.URL = "https://wrong.example/context"
	if o := transport.Send(context.Background(), endpoint, msg); o.Acknowledged {
		t.Fatal("TLS hostname bypass")
	}
	transport.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	if o := transport.Send(context.Background(), endpoint, msg); o.Code != "ADDRESS_BLOCKED" || o.Retryable {
		t.Fatal("peer address mismatch", o)
	}
}
