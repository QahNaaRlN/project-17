// Package capabilitygithub reads a bounded, read-only projection of GitHub facts.
package capabilitygithub

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var ErrConfig = errors.New("invalid capability GitHub configuration")
var segment = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Rule struct {
	Source    string `json:"source"`
	Name      string `json:"name"`
	Target    string `json:"target"`
	AppID     string `json:"appId,omitempty"`
	CreatorID string `json:"creatorId,omitempty"`
}
type Repository struct {
	ConfigID       string            `json:"configId"`
	ProjectID      uuid.UUID         `json:"projectId"`
	Environment    string            `json:"environment"`
	RepositoryID   string            `json:"repositoryId"`
	Owner          string            `json:"owner"`
	Name           string            `json:"name"`
	BaseBranch     string            `json:"baseBranch"`
	CredentialID   string            `json:"credentialId"`
	Token          string            `json:"token"`
	Installation   *InstallationAuth `json:"installation,omitempty"`
	RequiredChecks []Rule            `json:"requiredChecks"`
	Fingerprint    string            `json:"-"`
}
type Catalog struct{ repositories []Repository }

func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
func positive(s string) bool {
	if len(s) == 0 || len(s) > 20 || s[0] == '0' {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func text(s string, n int) bool {
	return len(s) > 0 && len(s) <= n && !strings.ContainsAny(s, "\x00\r\n\t")
}
func Parse(b []byte) (*Catalog, error) {
	var doc struct {
		Repositories []Repository `json:"repositories"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if len(b) > 1<<20 || d.Decode(&doc) != nil || d.Decode(new(any)) != io.EOF || len(doc.Repositories) == 0 || len(doc.Repositories) > 256 {
		return nil, ErrConfig
	}
	seen := map[string]bool{}
	scope := map[string]bool{}
	credentials := map[string]string{}
	for i := range doc.Repositories {
		r := &doc.Repositories[i]
		key := r.ProjectID.String() + "/" + r.Environment
		if !identifier.MatchString(r.ConfigID) || !identifier.MatchString(r.CredentialID) || r.ProjectID == uuid.Nil || !text(r.Environment, 100) || !positive(r.RepositoryID) || !segment.MatchString(r.Owner) || !segment.MatchString(r.Name) || r.Owner == "." || r.Owner == ".." || r.Name == "." || r.Name == ".." || !text(r.BaseBranch, 255) || !validCredential(*r) || seen[r.ConfigID] || scope[key] || len(r.RequiredChecks) == 0 || len(r.RequiredChecks) > 32 {
			return nil, ErrConfig
		}
		credential := credentialIdentity(*r)
		if old, ok := credentials[r.CredentialID]; ok && old != credential {
			return nil, ErrConfig
		}
		credentials[r.CredentialID] = credential
		seen[r.ConfigID] = true
		scope[key] = true
		rules := map[string]bool{}
		for _, v := range r.RequiredChecks {
			if !text(v.Name, 200) || (v.Target != "head" && v.Target != "integration") || (v.Source != "check_run" && v.Source != "commit_status") || v.Source == "check_run" && (!positive(v.AppID) || v.CreatorID != "") || v.Source == "commit_status" && (!positive(v.CreatorID) || v.AppID != "") || rules[Digest(v)] {
				return nil, ErrConfig
			}
			rules[Digest(v)] = true
		}
		public := *r
		public.Token = ""
		if r.Installation != nil {
			public.Installation = &InstallationAuth{AppID: r.Installation.AppID, InstallationID: r.Installation.InstallationID}
		}
		r.Fingerprint = Digest(public)
	}
	return &Catalog{repositories: doc.Repositories}, nil
}
func FromEnv(getenv func(string) string) (*Catalog, error) {
	path := getenv("CMS_CAPABILITY_GITHUB_CONFIG_FILE")
	if path == "" {
		return &Catalog{}, nil
	}
	if !filepath.IsAbs(path) {
		return nil, ErrConfig
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, ErrConfig
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil {
		return nil, ErrConfig
	}
	catalog, e := Parse(b)
	if e != nil {
		return nil, e
	}
	for i := range catalog.repositories {
		r := &catalog.repositories[i]
		if r.Installation != nil {
			key, err := readInstallationKey(r.Installation.PrivateKeyFile)
			if err != nil {
				return nil, ErrConfig
			}
			r.Installation.state = &installationState{key: key, scope: installationScope(*r)}
		}
	}
	return catalog, nil
}
func (c *Catalog) Find(project uuid.UUID, env, id string) (Repository, bool) {
	if c != nil {
		for _, r := range c.repositories {
			if r.ProjectID == project && r.Environment == env && r.ConfigID == id {
				return r, true
			}
		}
	}
	return Repository{}, false
}
func (c *Catalog) All() []Repository {
	if c == nil {
		return nil
	}
	return append([]Repository(nil), c.repositories...)
}
