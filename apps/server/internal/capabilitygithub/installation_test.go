package capabilitygithub

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var installationTestKey = sync.OnceValue(func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
})

func installationRepo(t *testing.T) Repository {
	t.Helper()
	r := repoFixture()
	r.Token = ""
	dir := t.TempDir()
	path := filepath.Join(dir, "app.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(installationTestKey())}), 0600); err != nil {
		t.Fatal(err)
	}
	r.Installation = &InstallationAuth{AppID: "17", InstallationID: "23", PrivateKeyFile: path}
	b, _ := json.Marshal(map[string]any{"repositories": []Repository{r}})
	config := filepath.Join(dir, "github.json")
	if err := os.WriteFile(config, b, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := FromEnv(func(string) string { return config })
	if err != nil {
		t.Fatal(err)
	}
	return c.All()[0]
}

func requireFailure(t *testing.T, err error, code string) {
	t.Helper()
	var f *Failure
	if !errors.As(err, &f) || f.Code != code {
		t.Fatalf("expected controlled %s failure, got %v", code, err)
	}
}

func verifyAppJWT(t *testing.T, header string, now time.Time) {
	t.Helper()
	parts := strings.Split(strings.TrimPrefix(header, "Bearer "), ".")
	if len(parts) != 3 {
		t.Fatal("invalid App JWT structure")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || rsa.VerifyPKCS1v15(&installationTestKey().PublicKey, crypto.SHA256, digest[:], signature) != nil {
		t.Fatal("invalid App JWT signature")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || string(decoded) != `{"alg":"RS256","typ":"JWT"}` {
		t.Fatal("unexpected JWT algorithm")
	}
	decoded, err = base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Issuer  string `json:"iss"`
		Issued  int64  `json:"iat"`
		Expires int64  `json:"exp"`
	}
	if json.Unmarshal(decoded, &claims) != nil || claims.Issuer != "17" || claims.Issued < now.Add(-62*time.Second).Unix() || claims.Issued > now.Add(-59*time.Second).Unix() || claims.Expires < now.Add(9*time.Minute-time.Second).Unix() || claims.Expires > now.Add(9*time.Minute+time.Second).Unix() {
		t.Fatal("invalid App JWT lifetime/issuer")
	}
}

func installationHTTP(t *testing.T, r Repository, now func() time.Time, issued *atomic.Int32, mutate func(*http.Request) (*http.Response, error)) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundtrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.github.com" || req.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			t.Fatal("unexpected network boundary")
		}
		if mutate != nil {
			if response, err := mutate(req); response != nil || err != nil {
				return response, err
			}
		}
		if req.Method == "POST" {
			if req.URL.Path != "/app/installations/23/access_tokens" || req.Header.Get("Content-Type") != "application/json" {
				t.Fatal("unexpected write endpoint")
			}
			verifyAppJWT(t, req.Header.Get("Authorization"), now())
			var payload struct {
				Repositories []json.Number     `json:"repository_ids"`
				Permissions  map[string]string `json:"permissions"`
			}
			if json.NewDecoder(req.Body).Decode(&payload) != nil || len(payload.Repositories) != 1 || payload.Repositories[0].String() != r.RepositoryID || Digest(payload.Permissions) != Digest(installationPermissions()) {
				t.Fatal("token scope not restricted")
			}
			n := issued.Add(1)
			return response(201, map[string]any{"token": "ghs_17_" + strings.Repeat("x", 200) + string(rune('a'+n)), "expires_at": now().Add(time.Hour), "permissions": installationPermissions()}), nil
		}
		if req.Method != "GET" || !strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ghs_17_") {
			t.Fatal("App JWT used for resource access")
		}
		switch {
		case req.URL.Path == "/installation/repositories":
			if req.URL.RawQuery != "per_page=100&page=1" {
				t.Fatal("unbounded repository check")
			}
			return response(200, map[string]any{"total_count": 1, "repositories": []any{map[string]any{"id": json.Number(r.RepositoryID), "full_name": r.Owner + "/" + r.Name}}}), nil
		case req.URL.Path == "/repos/owner/repo":
			return response(200, map[string]any{"id": 42, "full_name": "owner/repo"}), nil
		case req.URL.Path == "/repos/owner/repo/pulls/1":
			return response(200, map[string]any{"id": 123, "number": 1, "state": "open", "head": map[string]any{"sha": strings.Repeat("a", 40), "repo": map[string]any{"id": 42}}, "base": map[string]any{"sha": strings.Repeat("b", 40), "ref": "dev", "repo": map[string]any{"id": 42}}}), nil
		case strings.HasSuffix(req.URL.Path, "/check-runs"):
			return response(200, map[string]any{"total_count": 1, "check_runs": []any{map[string]any{"id": 1, "name": "Check", "head_sha": strings.Repeat("a", 40), "status": "completed", "conclusion": "success", "app": map[string]any{"id": 7}, "check_suite": map[string]any{"id": 1}}}}), nil
		}
		t.Fatal("unexpected installation request")
		return nil, errors.New("unexpected request")
	})}
}

func TestInstallationObserveRefreshAndRevocation(t *testing.T) {
	r := installationRepo(t)
	var clock atomic.Int64
	clock.Store(time.Now().Unix())
	now := func() time.Time { return time.Unix(clock.Load(), 0) }
	var issued atomic.Int32
	var reject atomic.Bool
	httpClient := installationHTTP(t, r, now, &issued, func(req *http.Request) (*http.Response, error) {
		if reject.Load() && req.URL.Path == "/repos/owner/repo" {
			return response(401, map[string]any{"message": "never expose provider body"}), nil
		}
		return nil, nil
	})
	c := &Client{client: httpClient, now: now}
	for range 2 {
		o, err := c.Observe(context.Background(), r, 1)
		if err != nil || o.CI != "success" {
			t.Fatal("App observation failed", err)
		}
	}
	if issued.Load() != 1 {
		t.Fatal("token not reused across observations")
	}
	clock.Add(int64(59 * time.Minute / time.Second))
	c = &Client{client: httpClient, now: now}
	if _, err := c.Observe(context.Background(), r, 1); err != nil || issued.Load() != 2 {
		t.Fatal("token not renewed before expiration", err)
	}
	reject.Store(true)
	_, err := c.Observe(context.Background(), r, 1)
	requireFailure(t, err, "credential_unavailable")
	reject.Store(false)
	if _, err = c.Observe(context.Background(), r, 1); err != nil || issued.Load() != 3 {
		t.Fatal("revoked token remained cached", err)
	}
	if r.Token != "" {
		t.Fatal("installation token copied into repository")
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "ghs_") || strings.Contains(string(b), "BEGIN RSA") {
		t.Fatal("runtime credential serialized")
	}
}

func TestInstallationConcurrentRefreshAndCancellation(t *testing.T) {
	r := installationRepo(t)
	now := time.Now
	var issued atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	httpClient := installationHTTP(t, r, now, &issued, func(req *http.Request) (*http.Response, error) {
		if req.Method == "POST" {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
		}
		return nil, nil
	})
	result := make(chan error, 16)
	for range 16 {
		go func() {
			s := session{client: httpClient, repo: r}
			_, err := s.credential(context.Background())
			result <- err
		}()
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := session{client: httpClient, repo: r}
	_, err := s.credential(ctx)
	requireFailure(t, err, "temporary_unavailable")
	close(release)
	for range 16 {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
	if issued.Load() != 1 {
		t.Fatal("parallel token refresh was not coalesced")
	}
}

func TestInstallationRejectsUnsafeResponses(t *testing.T) {
	for _, mode := range []string{"expired", "near_expiry", "far_expiry", "empty_token", "whitespace", "write", "missing", "extra", "other_repo", "wide_repo", "bad_name", "verification_denied", "http401", "http403", "rate_limit", "http500", "redirect", "malformed", "network"} {
		t.Run(mode, func(t *testing.T) {
			r := installationRepo(t)
			now := time.Now
			var issued atomic.Int32
			client := installationHTTP(t, r, now, &issued, func(req *http.Request) (*http.Response, error) {
				if req.Method == "POST" {
					status := map[string]int{"http401": 401, "http403": 403, "rate_limit": 429, "http500": 500, "redirect": 302}[mode]
					if status != 0 {
						return response(status, nil), nil
					}
					if mode == "network" {
						return nil, errors.New("do not expose transport detail")
					}
					if mode == "malformed" {
						return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader("{")), Header: http.Header{}}, nil
					}
					permissions := installationPermissions()
					token := "ghs_17_test"
					expires := now().Add(time.Hour)
					switch mode {
					case "expired":
						expires = now().Add(-time.Minute)
					case "near_expiry":
						expires = now().Add(time.Minute)
					case "far_expiry":
						expires = now().Add(2 * time.Hour)
					case "empty_token":
						token = ""
					case "whitespace":
						token = "bad secret"
					case "write":
						permissions["contents"] = "write"
					case "missing":
						delete(permissions, "checks")
					case "extra":
						permissions["administration"] = "read"
					}
					return response(201, map[string]any{"token": token, "expires_at": expires, "permissions": permissions}), nil
				}
				if req.URL.Path == "/installation/repositories" {
					switch mode {
					case "other_repo":
						return response(200, map[string]any{"total_count": 1, "repositories": []any{map[string]any{"id": 43, "full_name": "owner/repo"}}}), nil
					case "wide_repo":
						return response(200, map[string]any{"total_count": 2, "repositories": []any{map[string]any{"id": 42, "full_name": "owner/repo"}, map[string]any{"id": 43, "full_name": "owner/other"}}}), nil
					case "bad_name":
						return response(200, map[string]any{"total_count": 1, "repositories": []any{map[string]any{"id": 42, "full_name": "owner/other"}}}), nil
					case "verification_denied":
						return response(403, nil), nil
					}
				}
				return nil, nil
			})
			s := session{client: client, repo: r}
			_, err := s.credential(context.Background())
			if err == nil || strings.Contains(err.Error(), "secret") || r.Installation.state.token != "" {
				t.Fatal("unsafe credential accepted or exposed")
			}
			if mode == "rate_limit" || mode == "http500" || mode == "network" {
				var f *Failure
				if !errors.As(err, &f) || !f.Retryable {
					t.Fatal("lost retry classification")
				}
			}
		})
	}
}

func TestInstallationConfigurationAndPrivateKey(t *testing.T) {
	r := installationRepo(t)
	first := r.Fingerprint
	for _, mode := range []string{"both", "missing_auth", "app_id", "installation_id", "relative_key", "unknown_field"} {
		t.Run(mode, func(t *testing.T) {
			copy := r
			auth := *r.Installation
			copy.Installation = &auth
			switch mode {
			case "both":
				copy.Token = "pat"
			case "missing_auth":
				copy.Installation = nil
			case "app_id":
				auth.AppID = "../1"
			case "installation_id":
				auth.InstallationID = "0"
			case "relative_key":
				auth.PrivateKeyFile = "app.pem"
			}
			b, _ := json.Marshal(map[string]any{"repositories": []Repository{copy}})
			if mode == "unknown_field" {
				b = []byte(strings.Replace(string(b), `"appId":`, `"unexpected":1,"appId":`, 1))
			}
			if _, err := Parse(b); err != ErrConfig {
				t.Fatal("invalid installation configuration accepted")
			}
		})
	}
	copy := r
	auth := *r.Installation
	copy.Installation = &auth
	auth.PrivateKeyFile = "/different/secret.pem"
	b, _ := json.Marshal(map[string]any{"repositories": []Repository{copy}})
	catalog, err := Parse(b)
	if err != nil || catalog.All()[0].Fingerprint != first {
		t.Fatal("secret path entered configuration fingerprint")
	}
	auth.InstallationID = "24"
	b, _ = json.Marshal(map[string]any{"repositories": []Repository{copy}})
	catalog, err = Parse(b)
	if err != nil || catalog.All()[0].Fingerprint == first {
		t.Fatal("installation identity not bound to configuration")
	}
	copy.ConfigID = "other"
	copy.ProjectID = repoFixture().ProjectID
	b, _ = json.Marshal(map[string]any{"repositories": []Repository{r, copy}})
	if _, err = Parse(b); err != ErrConfig {
		t.Fatal("credential ID reused for different installation")
	}
	for _, mode := range []string{"pkcs8", "permissions", "missing", "directory", "malformed", "trailing", "unsupported", "ecdsa", "weak", "oversize", "invalid_pkcs1", "invalid_pkcs8"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "key.pem")
			permissions := os.FileMode(0600)
			block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(installationTestKey())}
			switch mode {
			case "pkcs8":
				block.Type = "PRIVATE KEY"
				block.Bytes, _ = x509.MarshalPKCS8PrivateKey(installationTestKey())
			case "permissions":
				permissions = 0644
			case "missing":
				_, err := readInstallationKey(path)
				if err != ErrConfig {
					t.Fatal(err)
				}
				return
			case "directory":
				_, err := readInstallationKey(filepath.Dir(path))
				if err != ErrConfig {
					t.Fatal(err)
				}
				return
			case "unsupported":
				block.Type = "PUBLIC KEY"
			case "ecdsa":
				key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				block.Type = "PRIVATE KEY"
				block.Bytes, _ = x509.MarshalPKCS8PrivateKey(key)
			case "weak":
				key, _ := rsa.GenerateKey(rand.Reader, 1024)
				block.Bytes = x509.MarshalPKCS1PrivateKey(key)
			case "invalid_pkcs1":
				block.Bytes = []byte("invalid")
			case "invalid_pkcs8":
				block.Type = "PRIVATE KEY"
				block.Bytes = []byte("invalid")
			}
			data := pem.EncodeToMemory(block)
			switch mode {
			case "malformed":
				data = []byte("not pem")
			case "trailing":
				data = append(data, []byte("extra")...)
			case "oversize":
				data = []byte(strings.Repeat("x", (64<<10)+1))
			}
			if err := os.WriteFile(path, data, permissions); err != nil {
				t.Fatal(err)
			}
			key, err := readInstallationKey(path)
			if mode == "pkcs8" {
				if err != nil || key == nil {
					t.Fatal(err)
				}
			} else if err != ErrConfig {
				t.Fatal("unsafe key accepted")
			}
			copy := r
			auth := *r.Installation
			copy.Installation = &auth
			auth.PrivateKeyFile = path
			b, _ := json.Marshal(map[string]any{"repositories": []Repository{copy}})
			config := filepath.Join(filepath.Dir(path), "config.json")
			if err := os.WriteFile(config, b, 0600); err != nil {
				t.Fatal(err)
			}
			_, err = FromEnv(func(string) string { return config })
			if mode != "pkcs8" && err != ErrConfig {
				t.Fatal("invalid key passed startup")
			}
		})
	}
	s := session{repo: r}
	s.repo.Installation = &InstallationAuth{AppID: "17", InstallationID: "23"}
	_, err = s.credential(context.Background())
	requireFailure(t, err, "credential_unavailable")
	s.repo = r
	s.repo.RepositoryID = "43"
	_, err = s.credential(context.Background())
	requireFailure(t, err, "credential_unavailable")
	s.repo = repoFixture()
	s.repo.Token = ""
	_, err = s.credential(context.Background())
	requireFailure(t, err, "credential_unavailable")
	s.invalidateInstallationToken("absent")
	if installationScope(repoFixture()) != "" {
		t.Fatal("PAT has installation scope")
	}
}
