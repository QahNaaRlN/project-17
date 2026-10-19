package capabilitygithub

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// InstallationAuth belongs only to the operator catalogue, never a public DTO.
// The parsed key and cached tokens are process-local and are not serialized.
type InstallationAuth struct {
	AppID          string `json:"appId"`
	InstallationID string `json:"installationId"`
	PrivateKeyFile string `json:"privateKeyFile,omitempty"`
	state          *installationState
}

type installationState struct {
	key     *rsa.PrivateKey
	scope   string
	mu      sync.Mutex
	token   string
	expires time.Time
	flight  *tokenFlight
}

type tokenFlight struct {
	done  chan struct{}
	token string
	err   error
}

func validCredential(r Repository) bool {
	if r.Installation == nil {
		return text(r.Token, 4096) && !strings.Contains(r.Token, " ")
	}
	a := r.Installation
	return r.Token == "" && positive(a.AppID) && positive(a.InstallationID) && text(a.PrivateKeyFile, 4096) && filepath.IsAbs(a.PrivateKeyFile)
}

func credentialIdentity(r Repository) string {
	if r.Installation == nil {
		return Digest([]string{"pat", r.Token})
	}
	a := r.Installation
	return Digest([]string{"installation", a.AppID, a.InstallationID, a.PrivateKeyFile})
}

func installationScope(r Repository) string {
	if r.Installation == nil {
		return ""
	}
	return Digest([]string{r.CredentialID, r.Installation.AppID, r.Installation.InstallationID, r.RepositoryID, r.Owner, r.Name})
}

func readInstallationKey(path string) (*rsa.PrivateKey, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrConfig
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrConfig
	}
	b, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(b) > 64<<10 {
		return nil, ErrConfig
	}
	block, rest := pem.Decode(b)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, ErrConfig
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		var parsed any
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		key, _ = parsed.(*rsa.PrivateKey)
	default:
		return nil, ErrConfig
	}
	if err != nil || key == nil || key.N.BitLen() < 2048 || key.N.BitLen() > 8192 || key.Validate() != nil {
		return nil, ErrConfig
	}
	return key, nil
}

func (s *session) timeNow() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *session) credential(ctx context.Context) (string, error) {
	if s.repo.Installation == nil {
		if !validCredential(s.repo) {
			return "", failure("credential_unavailable")
		}
		return s.repo.Token, nil
	}
	a := s.repo.Installation
	state := a.state
	if state == nil || state.key == nil || state.scope != installationScope(s.repo) {
		return "", failure("credential_unavailable")
	}
	state.mu.Lock()
	if state.token != "" && state.expires.After(s.timeNow().Add(2*time.Minute)) {
		token := state.token
		state.mu.Unlock()
		return token, nil
	}
	if flight := state.flight; flight != nil {
		state.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", &Failure{Code: "temporary_unavailable", Retryable: true}
		case <-flight.done:
			return flight.token, flight.err
		}
	}
	flight := &tokenFlight{done: make(chan struct{})}
	state.flight = flight
	state.mu.Unlock()
	token, expires, err := s.issueInstallationToken(ctx, state.key)
	state.mu.Lock()
	state.token, state.expires = "", time.Time{}
	if err == nil {
		state.token, state.expires = token, expires
	}
	flight.token, flight.err = token, err
	state.flight = nil
	close(flight.done)
	state.mu.Unlock()
	return token, err
}

func (s *session) invalidateInstallationToken(token string) {
	if s.repo.Installation == nil || s.repo.Installation.state == nil {
		return
	}
	state := s.repo.Installation.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.token == token {
		state.token, state.expires = "", time.Time{}
	}
}

func appJWT(key *rsa.PrivateKey, appID string, now time.Time) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{"iss": appID, "iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix()})
	if err != nil {
		return "", failure("credential_unavailable")
	}
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", failure("credential_unavailable")
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func installationPermissions() map[string]string {
	return map[string]string{"checks": "read", "contents": "read", "metadata": "read", "pull_requests": "read", "statuses": "read"}
}

func (s *session) issueInstallationToken(ctx context.Context, key *rsa.PrivateKey) (string, time.Time, error) {
	var empty time.Time
	jwt, err := appJWT(key, s.repo.Installation.AppID, s.timeNow())
	if err != nil {
		return "", empty, err
	}
	body, err := json.Marshal(map[string]any{"repository_ids": []json.Number{json.Number(s.repo.RepositoryID)}, "permissions": installationPermissions()})
	if err != nil {
		return "", empty, failure("invalid_response")
	}
	var result struct {
		Token       string            `json:"token"`
		Expires     time.Time         `json:"expires_at"`
		Permissions map[string]string `json:"permissions"`
	}
	path := "/app/installations/" + s.repo.Installation.InstallationID + "/access_tokens"
	if err := s.request(ctx, "POST", path, jwt, bytes.NewReader(body), http.StatusCreated, &result); err != nil {
		return "", empty, err
	}
	if !text(result.Token, 16<<10) || strings.Contains(result.Token, " ") || !result.Expires.After(s.timeNow().Add(2*time.Minute)) || result.Expires.After(s.timeNow().Add(65*time.Minute)) {
		return "", empty, failure("invalid_response")
	}
	wanted := installationPermissions()
	if len(result.Permissions) != len(wanted) {
		return "", empty, failure("permission_denied")
	}
	for name, level := range wanted {
		if result.Permissions[name] != level {
			return "", empty, failure("permission_denied")
		}
	}
	// Verify the issued token's repository projection rather than trusting names
	// or inheriting a wider installation. The only non-repository GET is fixed.
	var repositories struct {
		Count int        `json:"total_count"`
		Items []repoInfo `json:"repositories"`
	}
	if err := s.request(ctx, "GET", "/installation/repositories?per_page=100&page=1", result.Token, nil, http.StatusOK, &repositories); err != nil {
		return "", empty, err
	}
	if repositories.Count != 1 || len(repositories.Items) != 1 || repositories.Items[0].ID.String() != s.repo.RepositoryID || repositories.Items[0].FullName != s.repo.Owner+"/"+s.repo.Name {
		return "", empty, failure("repository_mismatch")
	}
	return result.Token, result.Expires, nil
}
