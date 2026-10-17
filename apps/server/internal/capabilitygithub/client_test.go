package capabilitygithub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func repoFixture() Repository {
	return Repository{ConfigID: "repo", ProjectID: uuid.New(), Environment: "staging", RepositoryID: "42", Owner: "owner", Name: "repo", BaseBranch: "dev", CredentialID: "key", Token: "private-token", RequiredChecks: []Rule{{Source: "check_run", Name: "Check", Target: "head", AppID: "7"}}}
}
func TestSTU295Config(t *testing.T) {
	r := repoFixture()
	b, _ := json.Marshal(map[string]any{"repositories": []Repository{r}})
	c, e := Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	got, ok := c.Find(r.ProjectID, "staging", "repo")
	if !ok || got.Fingerprint == "" || len(c.All()) != 1 {
		t.Fatal(got)
	}
	r.Token = "other"
	b, _ = json.Marshal(map[string]any{"repositories": []Repository{r}})
	other, _ := Parse(b)
	if got.Fingerprint != other.All()[0].Fingerprint {
		t.Fatal("token leaked into fingerprint")
	}
	for _, input := range []string{`{}`, `{"unknown":1}`, `{"repositories":[]}`, string(b) + `{}`, strings.Repeat("x", 1<<20+1)} {
		if _, e := Parse([]byte(input)); e == nil {
			t.Fatal("accepted invalid config")
		}
	}
	for _, mutate := range []func(*Repository){func(r *Repository) { r.Token = "bad\nsecret" }, func(r *Repository) { r.RepositoryID = "0" }, func(r *Repository) { r.Owner = ".." }, func(r *Repository) { r.RequiredChecks = nil }, func(r *Repository) { r.RequiredChecks[0].AppID = "" }, func(r *Repository) { r.RequiredChecks[0].Target = "branch" }} {
		r := repoFixture()
		mutate(&r)
		b, _ := json.Marshal(map[string]any{"repositories": []Repository{r}})
		if _, e := Parse(b); e == nil {
			t.Fatal("accepted invalid entry")
		}
	}
	if _, ok := (*Catalog)(nil).Find(uuid.New(), "", ""); ok {
		t.Fatal("nil catalog")
	}
	if c, e := FromEnv(func(string) string { return "" }); e != nil || len(c.All()) != 0 {
		t.Fatal(e)
	}
	if _, e := FromEnv(func(string) string { return "/not-present" }); e != ErrConfig {
		t.Fatal(e)
	}
}

type roundtrip func(*http.Request) (*http.Response, error)

func (f roundtrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(string(b))), Header: http.Header{}}
}
func TestSTU299_304ObserveExactCommit(t *testing.T) {
	head := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	merge := strings.Repeat("c", 40)
	r := repoFixture()
	r.RequiredChecks[0].Target = "integration"
	for _, mode := range []string{"success", "missing", "failure", "pending", "skipped", "neutral", "cancelled", "merged", "mismatch", "changed", "parents", "fork", "status", "empty_status", "newer_failure", "unknown", "pages", "incomplete"} {
		t.Run(mode, func(t *testing.T) {
			reads := 0
			c := &Client{client: &http.Client{Transport: roundtrip(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "api.github.com" || req.Header.Get("Authorization") != "Bearer private-token" || req.Header.Get("X-GitHub-Api-Version") == "" {
					t.Fatal("transport contract")
				}
				path := req.URL.Path
				switch {
				case path == "/repos/owner/repo":
					id := 42
					if mode == "mismatch" {
						id = 43
					}
					return response(200, map[string]any{"id": id, "full_name": "owner/repo"}), nil
				case strings.Contains(path, "/pulls/"):
					reads++
					sha := head
					if mode == "changed" && reads > 1 {
						sha = base
					}
					state := "open"
					merged := mode == "merged"
					if merged {
						state = "closed"
					}
					fork := 42
					if mode == "fork" {
						fork = 99
					}
					return response(200, map[string]any{"id": 123, "number": 1, "state": state, "draft": false, "merged": merged, "mergeable": true, "merge_commit_sha": merge, "head": map[string]any{"sha": sha, "repo": map[string]any{"id": fork}}, "base": map[string]any{"sha": base, "ref": "dev", "repo": map[string]any{"id": 42}}}), nil
				case strings.Contains(path, "/git/commits/"):
					parent := base
					if mode == "parents" {
						parent = head
					}
					return response(200, map[string]any{"sha": merge, "parents": []any{map[string]any{"sha": parent}, map[string]any{"sha": head}}}), nil
				case strings.HasSuffix(path, "/statuses"):
					rows := []any{}
					if mode == "status" {
						rows = append(rows, map[string]any{"id": 2, "context": "Build", "state": "success", "created_at": time.Now(), "creator": map[string]any{"id": 8}})
					}
					return response(200, rows), nil
				case strings.HasSuffix(path, "/check-runs"):
					rows := []any{}
					status := "completed"
					conclusion := "success"
					switch mode {
					case "failure", "skipped", "neutral", "cancelled", "unknown":
						conclusion = mode
					case "pending":
						status = "in_progress"
					}
					if mode != "missing" {
						rows = append(rows, map[string]any{"id": 1, "name": "Check", "head_sha": merge, "status": status, "conclusion": conclusion, "app": map[string]any{"id": 7}, "check_suite": map[string]any{"id": 1}})
					}
					if mode == "newer_failure" {
						rows = append(rows, map[string]any{"id": 2, "name": "Check", "head_sha": merge, "status": "completed", "conclusion": "failure", "app": map[string]any{"id": 7}, "check_suite": map[string]any{"id": 1}})
					}
					total := len(rows)
					if mode == "incomplete" {
						total = 101
					}
					if mode == "pages" && req.URL.Query().Get("page") == "1" {
						for len(rows) < 100 {
							rows = append(rows, map[string]any{"id": 99, "name": "Other"})
						}
						total = 101
					}
					return response(200, map[string]any{"total_count": total, "check_runs": rows}), nil
				default:
					return nil, fmt.Errorf("unexpected path %s", path)
				}
			})}}
			rr := r
			if mode == "status" || mode == "empty_status" {
				rr.RequiredChecks = []Rule{{Source: "commit_status", Name: "Build", Target: "head", CreatorID: "8"}}
			}
			o, e := c.Observe(context.Background(), rr, 1)
			if mode == "mismatch" || mode == "changed" || mode == "incomplete" {
				if e == nil {
					t.Fatal("expected failure")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			expected := "success"
			switch mode {
			case "missing", "skipped", "neutral", "unknown", "parents", "empty_status":
				expected = "unknown"
			case "failure", "newer_failure":
				expected = "failure"
			case "pending":
				expected = "pending"
			case "cancelled":
				expected = "cancelled"
			}
			if o.CI != expected {
				t.Fatalf("%s want %s", o.CI, expected)
			}
			if mode == "merged" && o.IntegrationType != "merged_commit" {
				t.Fatal(o)
			}
			if mode == "fork" && o.HeadRepositoryID != "99" {
				t.Fatal(o)
			}
		})
	}
}
func TestSTU309ErrorsAndBounds(t *testing.T) {
	for _, code := range []int{401, 403, 404, 429, 500, 302} {
		c := &Client{client: &http.Client{Transport: roundtrip(func(*http.Request) (*http.Response, error) {
			r := response(code, nil)
			if code == 429 {
				r.Header.Set("Retry-After", "120")
			}
			return r, nil
		})}}
		_, e := c.Observe(context.Background(), repoFixture(), 1)
		if e == nil {
			t.Fatal(code)
		}
	}
	for _, body := range []string{"not-json", strings.Repeat("x", (2<<20)+1)} {
		c := &Client{client: &http.Client{Transport: roundtrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		if _, e := c.Observe(context.Background(), repoFixture(), 1); e == nil {
			t.Fatal("unbounded")
		}
	}
	if _, e := githubDial(context.Background(), "tcp", "localhost:443"); e == nil {
		t.Fatal("host")
	}
	if _, e := githubDial(context.Background(), "tcp", "api.github.com:80"); e == nil {
		t.Fatal("port")
	}
	c := NewClient()
	if c.client.CheckRedirect(nil, nil) == nil {
		t.Fatal("redirect")
	}
	if got := CanonicalURL(repoFixture(), 2); got != "https://github.com/owner/repo/pull/2" {
		t.Fatal(got)
	}
}
func TestSTU303SuiteRerunAndPublishers(t *testing.T) {
	rule := repoFixture().RequiredChecks[0]
	var rows []check
	json.Unmarshal([]byte(`[{"id":10,"name":"Check","head_sha":"aaa","status":"completed","conclusion":"success","app":{"id":7},"check_suite":{"id":1}},{"id":11,"name":"Check","head_sha":"aaa","status":"in_progress","app":{"id":7},"check_suite":{"id":1}}]`), &rows)
	if got := chooseCheck(rows, rule, "aaa"); got.Outcome != "pending" {
		t.Fatal(got)
	}
	rows[1].App.ID = "9"
	if got := chooseCheck(rows, rule, "aaa"); got.Outcome != "success" {
		t.Fatal(got)
	}
	rows[1].App.ID = "7"
	rows[1].Suite.ID = "2"
	if got := chooseCheck(rows, rule, "aaa"); got.Outcome != "unknown" {
		t.Fatal("ambiguous suite must not be green")
	}
	for _, rs := range [][]Result{nil, {{Outcome: "unknown"}}, {{Outcome: "success"}, {Outcome: "unknown"}}, {{Outcome: "success"}, {Outcome: "failure"}}, {{Outcome: "pending"}, {Outcome: "cancelled"}}} {
		if aggregate(rs) == "success" {
			t.Fatal("unsafe aggregate")
		}
	}
}

func TestSTU303NewestSuitePrecedesOldSuccess(t *testing.T) {
	rule := repoFixture().RequiredChecks[0]
	var rows []check
	json.Unmarshal([]byte(`[{"id":10,"name":"Check","head_sha":"aaa","status":"completed","conclusion":"success","app":{"id":7},"check_suite":{"id":1}},{"id":11,"name":"Check","head_sha":"aaa","status":"in_progress","app":{"id":7},"check_suite":{"id":2}}]`), &rows)
	rows[0].SuiteCreated = time.Unix(100, 0)
	rows[1].SuiteCreated = time.Unix(200, 0)
	if got := chooseCheck(rows, rule, "aaa"); got.Outcome != "pending" {
		t.Fatal(got)
	}
}

type peerConn struct {
	net.Conn
	address net.Addr
	closed  bool
}

func (c *peerConn) RemoteAddr() net.Addr { return c.address }
func (c *peerConn) Close() error         { c.closed = true; return nil }
func TestSTU309PinnedNetwork(t *testing.T) {
	ctx := context.Background()
	public := netip.MustParseAddr("140.82.113.6")
	for _, mode := range []string{"lookup_error", "no_addresses", "private", "dial_error", "peer_mismatch", "success"} {
		t.Run(mode, func(t *testing.T) {
			lookup := func(context.Context, string, string) ([]netip.Addr, error) {
				switch mode {
				case "lookup_error":
					return nil, errors.New("dns")
				case "no_addresses":
					return nil, nil
				case "private":
					return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
				}
				return []netip.Addr{public}, nil
			}
			conn := &peerConn{address: &net.TCPAddr{IP: net.ParseIP(public.String()), Port: 443}}
			if mode == "peer_mismatch" {
				conn.address = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}
			}
			dial := func(context.Context, string, string) (net.Conn, error) {
				if mode == "dial_error" {
					return nil, errors.New("dial")
				}
				return conn, nil
			}
			c, e := githubConnect(ctx, "tcp", "api.github.com:443", lookup, dial)
			if mode == "success" {
				if e != nil || c != conn {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("unsafe connection")
			}
		})
	}
}
func TestSTU305_309BudgetsAndRetryHeaders(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"calls", "bytes", "path", "network", "body", "rate_remaining", "rate_reset", "rate_date"} {
		t.Run(mode, func(t *testing.T) {
			s := session{repo: repoFixture(), client: &http.Client{Transport: roundtrip(func(*http.Request) (*http.Response, error) {
				if mode == "network" {
					return nil, errors.New("network")
				}
				if mode == "body" {
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(errorReader{})}, nil
				}
				r := response(200, map[string]any{})
				if strings.HasPrefix(mode, "rate_") {
					r.StatusCode = 403
					switch mode {
					case "rate_remaining":
						r.Header.Set("X-RateLimit-Remaining", "0")
					case "rate_reset":
						r.Header.Set("X-RateLimit-Remaining", "0")
						r.Header.Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(time.Hour).Unix()))
					case "rate_date":
						r.Header.Set("Retry-After", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))
					}
				}
				return r, nil
			})}}
			path := "/repos/owner/repo"
			if mode == "calls" {
				s.calls = 40
			}
			if mode == "bytes" {
				s.bytes = 10 << 20
			}
			if mode == "path" {
				path = "\n"
			}
			if e := s.get(ctx, path, new(any)); e == nil {
				t.Fatal("budget/error ignored")
			}
		})
	}
	s := session{repo: repoFixture(), client: &http.Client{Transport: roundtrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/statuses") {
			rows := make([]commitStatus, 100)
			return response(200, rows), nil
		}
		return response(200, map[string]any{"total_count": 1001, "check_runs": make([]check, 100)}), nil
	})}}
	if _, e := s.statuses(ctx, "/repos/owner/repo", strings.Repeat("a", 40)); e == nil {
		t.Fatal("pages")
	}
	s.calls = 0
	if _, e := s.checks(ctx, "/repos/owner/repo", strings.Repeat("a", 40)); e == nil {
		t.Fatal("pages")
	}
	if _, e := NewClient().Observe(ctx, repoFixture(), 0); e == nil {
		t.Fatal("zero PR")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func TestSTU303SuiteMetadataAndStatusOrdering(t *testing.T) {
	rule := repoFixture().RequiredChecks[0]
	sha := strings.Repeat("a", 40)
	var rows []check
	json.Unmarshal([]byte(fmt.Sprintf(`[{"id":10,"name":"Check","head_sha":"%s","status":"completed","conclusion":"success","app":{"id":7},"check_suite":{"id":1}},{"id":11,"name":"Check","head_sha":"%s","status":"in_progress","app":{"id":7},"check_suite":{"id":2}}]`, sha, sha)), &rows)
	for _, mode := range []string{"valid", "mismatch", "error"} {
		s := session{repo: repoFixture(), client: &http.Client{Transport: roundtrip(func(req *http.Request) (*http.Response, error) {
			if mode == "error" {
				return response(404, nil), nil
			}
			id := 1
			if strings.HasSuffix(req.URL.Path, "/2") {
				id = 2
			}
			head := sha
			if mode == "mismatch" {
				head = "bad"
			}
			return response(200, map[string]any{"id": id, "head_sha": head, "created_at": time.Unix(int64(id)*100, 0).UTC(), "app": map[string]any{"id": 7}}), nil
		})}}
		got, e := s.suiteDates(context.Background(), "/repos/owner/repo", sha, rows, rule)
		if mode == "valid" {
			if e != nil || chooseCheck(got, rule, sha).Outcome != "pending" {
				t.Fatal(got, e)
			}
		} else if e == nil {
			t.Fatal("invalid suite trusted")
		}
	}
	statusRule := Rule{Source: "commit_status", Name: "Build", Target: "head", CreatorID: "8"}
	var statuses []commitStatus
	json.Unmarshal([]byte(`[{"id":1,"context":"Build","state":"success","created_at":"2026-10-17T00:00:00Z","creator":{"id":8}},{"id":2,"context":"Build","state":"pending","created_at":"2026-10-17T00:00:00Z","creator":{"id":8}}]`), &statuses)
	if chooseStatus(statuses, statusRule, sha).Outcome != "pending" {
		t.Fatal("old success won")
	}
	for _, state := range []string{"failure", "error", "unknown"} {
		statuses[1].State = state
		if chooseStatus(statuses, statusRule, sha).Outcome == "success" {
			t.Fatal(state)
		}
	}
	statuses[1].ID = ""
	if chooseStatus(statuses, statusRule, sha).Outcome != "unknown" {
		t.Fatal("invalid status id")
	}
	rows[0].ID = ""
	if chooseCheck(rows, rule, sha).Outcome != "unknown" {
		t.Fatal("invalid check id")
	}
	rows = rows[1:]
	for _, state := range []string{"requested", "waiting", "unknown"} {
		rows[0].Status = state
		if chooseCheck(rows, rule, sha).Outcome == "success" {
			t.Fatal(state)
		}
	}
}
func TestSTU295ConfigFileAndScope(t *testing.T) {
	r := repoFixture()
	path := filepath.Join(t.TempDir(), "config.json")
	b, _ := json.Marshal(map[string]any{"repositories": []Repository{r}})
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := FromEnv(func(string) string { return path }); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"duplicate", "scope", "credential", "rule", "publisher", "id", "field"} {
		rr := repoFixture()
		doc := map[string]any{"repositories": []Repository{rr}}
		switch mode {
		case "duplicate":
			doc["repositories"] = []Repository{rr, rr}
		case "scope":
			copy := rr
			copy.ConfigID = "other"
			doc["repositories"] = []Repository{rr, copy}
		case "credential":
			copy := rr
			copy.ProjectID = uuid.New()
			copy.ConfigID = "other"
			copy.Token = "different"
			doc["repositories"] = []Repository{rr, copy}
		case "rule":
			rr.RequiredChecks = append(rr.RequiredChecks, rr.RequiredChecks[0])
			doc["repositories"] = []Repository{rr}
		case "publisher":
			rr.RequiredChecks = []Rule{{Source: "commit_status", Name: "Build", Target: "head", CreatorID: "bad"}}
			doc["repositories"] = []Repository{rr}
		case "id":
			rr.CredentialID = "bad id"
			doc["repositories"] = []Repository{rr}
		case "field":
			doc["unexpected"] = true
		}
		b, _ := json.Marshal(doc)
		if _, e := Parse(b); e == nil {
			t.Fatal(mode)
		}
	}
	if _, ok := (&Catalog{}).Find(uuid.New(), "staging", "missing"); ok {
		t.Fatal("unexpected config")
	}
	if (*Catalog)(nil).All() != nil {
		t.Fatal("nil catalog")
	}
}

func TestSTU303ConflictingDuplicateCheckCannotPass(t *testing.T) {
	rule := repoFixture().RequiredChecks[0]
	var rows []check
	json.Unmarshal([]byte(`[{"id":10,"name":"Check","head_sha":"aaa","status":"completed","conclusion":"success","app":{"id":7},"check_suite":{"id":1}},{"id":10,"name":"Check","head_sha":"aaa","status":"in_progress","app":{"id":7},"check_suite":{"id":1}}]`), &rows)
	if chooseCheck(rows, rule, "aaa").Outcome != "unknown" {
		t.Fatal("conflicting duplicate was trusted")
	}
}

func TestSTU303ConflictingDuplicateStatusCannotPass(t *testing.T) {
	rule := Rule{Source: "commit_status", Name: "Build", Target: "head", CreatorID: "8"}
	var rows []commitStatus
	json.Unmarshal([]byte(`[{"id":10,"context":"Build","state":"success","created_at":"2026-10-17T00:00:00Z","creator":{"id":8}},{"id":10,"context":"Build","state":"pending","created_at":"2026-10-17T00:00:00Z","creator":{"id":8}}]`), &rows)
	if chooseStatus(rows, rule, "aaa").Outcome != "unknown" {
		t.Fatal("conflicting commit status was trusted")
	}
}

// Real GitHub Actions attribution observed on PR #62: results are attached to
// head.sha even when workflow checkout tests the synthetic merge. Never move them.
func TestSTU301HeadChecksDoNotProveSyntheticMergeChecks(t *testing.T) {
	r := repoFixture()
	r.RequiredChecks = []Rule{{Source: "check_run", Name: "Check", Target: "head", AppID: "7"}, {Source: "check_run", Name: "Check", Target: "integration", AppID: "7"}}
	head, base, merge := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	c := &Client{client: &http.Client{Transport: roundtrip(func(req *http.Request) (*http.Response, error) {
		p := req.URL.Path
		switch {
		case p == "/repos/owner/repo":
			return response(200, map[string]any{"id": 42, "full_name": "owner/repo"}), nil
		case strings.Contains(p, "/pulls/"):
			return response(200, map[string]any{"id": 123, "number": 1, "state": "open", "mergeable": true, "merge_commit_sha": merge, "head": map[string]any{"sha": head, "repo": map[string]any{"id": 42}}, "base": map[string]any{"sha": base, "ref": "dev", "repo": map[string]any{"id": 42}}}), nil
		case strings.Contains(p, "/git/commits/"):
			return response(200, map[string]any{"sha": merge, "parents": []any{map[string]any{"sha": base}, map[string]any{"sha": head}}}), nil
		case strings.Contains(p, "/commits/"+head+"/check-runs"):
			return response(200, map[string]any{"total_count": 1, "check_runs": []any{map[string]any{"id": 1, "name": "Check", "head_sha": head, "status": "completed", "conclusion": "success", "app": map[string]any{"id": 7}, "check_suite": map[string]any{"id": 1}}}}), nil
		default:
			return response(200, map[string]any{"total_count": 0, "check_runs": []any{}}), nil
		}
	})}}
	o, e := c.Observe(context.Background(), r, 1)
	if e != nil {
		t.Fatal(e)
	}
	if o.CI != "unknown" || o.Results[0].Outcome != "success" || o.Results[0].ExactSHA != head || o.Results[1].Outcome != "unknown" || o.Results[1].ExactSHA != merge {
		t.Fatal(o)
	}
}

func TestSTU302KnownInconclusiveResultsRemainAuditable(t *testing.T) {
	rule := repoFixture().RequiredChecks[0]
	for _, conclusion := range []string{"neutral", "skipped", "stale"} {
		var rows []check
		json.Unmarshal([]byte(fmt.Sprintf(`[{"id":1,"name":"Check","head_sha":"aaa","status":"completed","conclusion":"%s","app":{"id":7},"check_suite":{"id":1}}]`, conclusion)), &rows)
		got := chooseCheck(rows, rule, "aaa")
		if got.Outcome != "unknown" || got.Conclusion != conclusion {
			t.Fatal("known inconclusive result lost", got)
		}
	}
}

func TestSTU304OutagesBetweenProviderReadsNeverPublishPartialFacts(t *testing.T) {
	for _, phase := range []string{"pull", "commit", "checks", "statuses", "after", "suite"} {
		t.Run(phase, func(t *testing.T) {
			r := repoFixture()
			if phase == "statuses" {
				r.RequiredChecks = []Rule{{Source: "commit_status", Name: "Build", Target: "head", CreatorID: "8"}}
			}
			head, base, merge := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
			reads := 0
			c := &Client{client: &http.Client{Transport: roundtrip(func(req *http.Request) (*http.Response, error) {
				p := req.URL.Path
				switch {
				case p == "/repos/owner/repo":
					return response(200, map[string]any{"id": 42, "full_name": "owner/repo"}), nil
				case strings.Contains(p, "/pulls/"):
					reads++
					if phase == "pull" || phase == "after" && reads > 1 {
						return response(503, nil), nil
					}
					return response(200, map[string]any{"id": 123, "number": 1, "state": "open", "mergeable": phase == "commit", "merge_commit_sha": merge, "head": map[string]any{"sha": head, "repo": map[string]any{"id": 42}}, "base": map[string]any{"sha": base, "ref": "dev", "repo": map[string]any{"id": 42}}}), nil
				case strings.Contains(p, "/git/commits/"), strings.Contains(p, "/statuses"), strings.Contains(p, "/check-suites/"):
					return response(503, nil), nil
				case strings.Contains(p, "/check-runs"):
					if phase == "checks" {
						return response(503, nil), nil
					}
					rows := []any{}
					if phase == "suite" {
						for i := 1; i <= 2; i++ {
							rows = append(rows, map[string]any{"id": i, "name": "Check", "head_sha": head, "status": "completed", "conclusion": "success", "app": map[string]any{"id": 7}, "check_suite": map[string]any{"id": i}})
						}
					}
					return response(200, map[string]any{"total_count": len(rows), "check_runs": rows}), nil
				default:
					t.Fatal("unexpected provider request")
					return nil, errors.New("unexpected")
				}
			})}}
			o, e := c.Observe(context.Background(), r, 1)
			if e == nil || o.RepositoryID != "" {
				t.Fatal("provider outage produced a partial observation", o, e)
			}
		})
	}
}
func TestSTU295OperatorPathMustBeAbsoluteAndReadable(t *testing.T) {
	if _, e := FromEnv(func(string) string { return "relative-config.json" }); e != ErrConfig {
		t.Fatal(e)
	}
	if _, e := FromEnv(func(string) string { return t.TempDir() }); e != ErrConfig {
		t.Fatal("directory accepted as config", e)
	}
}
