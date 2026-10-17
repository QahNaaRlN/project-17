// Package capabilitywebhook implements the operator-controlled, signed transport.
// Secrets never leave this package; callers retain only endpoint identities.
package capabilitywebhook

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
)

var ErrConfig = errors.New("invalid capability webhook configuration")
var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Endpoint struct {
	EndpointID  string    `json:"endpointId"`
	ProjectID   uuid.UUID `json:"projectId"`
	Environment string    `json:"environment"`
	URL         string    `json:"url"`
	Fingerprint string    `json:"fingerprint"`
	active      string
	keys        map[string][]byte
}

type Catalog struct {
	endpoints []Endpoint
	loopback  bool
}

func (c *Catalog) String() string { return "capability webhook catalog (redacted)" }
func (e Endpoint) String() string { return "capability webhook endpoint (redacted)" }

// FromEnv accepts an absent file as disabled, without disabling manual packs.
func FromEnv(getenv func(string) string) (*Catalog, error) {
	flag := getenv("CMS_CAPABILITY_WEBHOOK_ALLOW_LOOPBACK_HTTP")
	if flag != "" && flag != "true" && flag != "false" {
		return nil, ErrConfig
	}
	path := getenv("CMS_CAPABILITY_WEBHOOK_CONFIG_FILE")
	if path == "" {
		return &Catalog{loopback: flag == "true"}, nil
	}
	if !filepath.IsAbs(path) {
		return nil, ErrConfig
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrConfig
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return nil, ErrConfig
	}
	return Parse(b, flag == "true")
}

// Parse rejects duplicate JSON keys as well as unknown configuration fields.
func Parse(b []byte, loopback bool) (*Catalog, error) {
	if len(b) > 1<<20 {
		return nil, ErrConfig
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if uniqueJSON(d) != nil {
		return nil, ErrConfig
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrConfig
	}
	var file struct {
		Endpoints []struct {
			EndpointID  string            `json:"endpointId"`
			ProjectID   uuid.UUID         `json:"projectId"`
			Environment string            `json:"environment"`
			URL         string            `json:"url"`
			ActiveKeyID string            `json:"activeKeyId"`
			Keys        map[string]string `json:"keys"`
		} `json:"endpoints"`
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&file) != nil || len(file.Endpoints) < 1 || len(file.Endpoints) > 256 {
		return nil, ErrConfig
	}
	c := &Catalog{loopback: loopback}
	seen := map[string]bool{}
	for _, v := range file.Endpoints {
		if !identifier.MatchString(v.EndpointID) || v.ProjectID == uuid.Nil || !identifier.MatchString(v.Environment) || !identifier.MatchString(v.ActiveKeyID) || len(v.Keys) < 1 || len(v.Keys) > 8 || !ValidURL(v.URL, loopback) {
			return nil, ErrConfig
		}
		identity := v.ProjectID.String() + "/" + v.Environment + "/" + v.EndpointID
		if seen[identity] {
			return nil, ErrConfig
		}
		seen[identity] = true
		e := Endpoint{EndpointID: v.EndpointID, ProjectID: v.ProjectID, Environment: v.Environment, URL: v.URL, active: v.ActiveKeyID, keys: map[string][]byte{}}
		for id, encoded := range v.Keys {
			key, err := base64.StdEncoding.Strict().DecodeString(encoded)
			if !identifier.MatchString(id) || err != nil || len(key) < 32 || len(key) > 128 || base64.StdEncoding.EncodeToString(key) != encoded {
				return nil, ErrConfig
			}
			e.keys[id] = key
		}
		if e.keys[e.active] == nil {
			return nil, ErrConfig
		}
		e.Fingerprint, _ = manifest.Hash(map[string]any{"endpointId": e.EndpointID, "projectId": e.ProjectID.String(), "environment": e.Environment, "url": e.URL})
		c.endpoints = append(c.endpoints, e)
	}
	return c, nil
}

func uniqueJSON(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := t.(json.Delim); ok {
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				s, ok := k.(string)
				if !ok || seen[s] {
					return ErrConfig
				}
				seen[s] = true
				if err = uniqueJSON(d); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err = uniqueJSON(d); err != nil {
					return err
				}
			}
		default:
			return ErrConfig
		}
		_, err = d.Token()
		return err
	}
	return nil
}

func ValidURL(raw string, loopback bool) bool {
	if len(raw) == 0 || len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\\") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.Hostname() == "" || strings.HasSuffix(u.Hostname(), ".") {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return false
		}
	}
	if loopback && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1") && u.Port() != "" {
		return true
	}
	return u.Scheme == "https" && (u.Port() == "" || u.Port() == "443") && ip == nil && !strings.Contains(u.Hostname(), ":") && strings.Contains(u.Hostname(), ".")
}

func (c *Catalog) Endpoints(project uuid.UUID, environment string) []Endpoint {
	result := []Endpoint{}
	if c != nil {
		for _, e := range c.endpoints {
			if e.ProjectID == project && e.Environment == environment {
				result = append(result, e)
			}
		}
	}
	return result
}
func (c *Catalog) Find(project uuid.UUID, environment, id string) (Endpoint, bool) {
	for _, e := range c.Endpoints(project, environment) {
		if e.EndpointID == id {
			return e, true
		}
	}
	return Endpoint{}, false
}
func (c *Catalog) All() []Endpoint {
	if c == nil {
		return nil
	}
	return append([]Endpoint(nil), c.endpoints...)
}
func (e Endpoint) ActiveKeyID() string   { return e.active }
func (e Endpoint) HasKey(id string) bool { return e.keys[id] != nil }
func (e Endpoint) KeyFingerprint(id string) string {
	if !e.HasKey(id) {
		return ""
	}
	return Hash(e.keys[id])
}
