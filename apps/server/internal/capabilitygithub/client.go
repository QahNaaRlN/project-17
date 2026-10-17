package capabilitygithub

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"

	"github.com/qahnaarln/project-17/apps/server/internal/capabilitywebhook"
)

type Failure struct {
	Code      string
	RetryAt   time.Time
	Retryable bool
}

func (f *Failure) Error() string { return f.Code }
func failure(code string) error  { return &Failure{Code: code} }

// Fetcher is injected at the service boundary in tests, never through runtime env.
type Fetcher interface {
	Observe(context.Context, Repository, int64) (Observation, error)
}
type Client struct{ client *http.Client }

func NewClient() *Client {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 32 << 10, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "api.github.com"}, DialContext: githubDial}
	return &Client{client: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect blocked") }}}
}
func githubDial(ctx context.Context, network, address string) (net.Conn, error) {
	return githubConnect(ctx, network, address, net.DefaultResolver.LookupNetIP, (&net.Dialer{}).DialContext)
}
func githubConnect(ctx context.Context, network, address string, lookup func(context.Context, string, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil || host != "api.github.com" || port != "443" {
		return nil, errors.New("address blocked")
	}
	ips, e := lookup(ctx, "ip", host)
	if e != nil {
		return nil, e
	}
	if len(ips) == 0 {
		return nil, errors.New("address blocked")
	}
	for _, ip := range ips {
		if !capabilitywebhook.Public(ip) {
			return nil, errors.New("address blocked")
		}
	}
	var last error
	for _, ip := range ips {
		c, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if err != nil {
			last = err
			continue
		}
		peer, err := netip.ParseAddrPort(c.RemoteAddr().String())
		if err != nil || peer.Addr() != ip {
			c.Close()
			return nil, errors.New("peer blocked")
		}
		return c, nil
	}
	return nil, last
}

type session struct {
	client       *http.Client
	repo         Repository
	calls, bytes int
}

func (s *session) get(ctx context.Context, path string, out any) error {
	s.calls++
	if s.calls > 40 {
		return failure("incomplete")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://api.github.com"+path, nil)
	if e != nil {
		return failure("invalid_response")
	}
	req.Header.Set("Authorization", "Bearer "+s.repo.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	resp, e := s.client.Do(req)
	if e != nil {
		return &Failure{Code: "temporary_unavailable", Retryable: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		code := "invalid_response"
		retry := false
		var at time.Time
		switch resp.StatusCode {
		case 401:
			code = "credential_unavailable"
		case 404:
			code = "not_found_or_inaccessible"
		case 403:
			code = "permission_denied"
		case 429:
			code = "rate_limited"
			retry = true
		}
		if resp.StatusCode == 403 && (resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "") {
			code = "rate_limited"
			retry = true
		}
		if resp.StatusCode >= 500 {
			code = "temporary_unavailable"
			retry = true
		}
		if retry {
			at = time.Now().Add(time.Minute)
			if n, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && time.Unix(n, 0).After(at) {
				at = time.Unix(n, 0)
			}
			if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n > 0 && n <= 86400 {
				t := time.Now().Add(time.Duration(n) * time.Second)
				if t.After(at) {
					at = t
				}
			} else if t, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && t.After(at) {
				at = t
			}
		}
		return &Failure{Code: code, RetryAt: at, Retryable: retry}
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	s.bytes += len(b)
	if e != nil || len(b) > 2<<20 || s.bytes > 10<<20 {
		return failure("incomplete")
	}
	if json.Unmarshal(b, out) != nil {
		return failure("invalid_response")
	}
	return nil
}

type repoInfo struct {
	ID       json.Number `json:"id"`
	FullName string      `json:"full_name"`
}
type branch struct {
	SHA  string    `json:"sha"`
	Ref  string    `json:"ref"`
	Repo *repoInfo `json:"repo"`
}
type pull struct {
	ID        json.Number `json:"id"`
	Number    int64       `json:"number"`
	State     string      `json:"state"`
	Draft     bool        `json:"draft"`
	Merged    bool        `json:"merged"`
	Mergeable *bool       `json:"mergeable"`
	MergeSHA  *string     `json:"merge_commit_sha"`
	MergedAt  *time.Time  `json:"merged_at"`
	Head      branch      `json:"head"`
	Base      branch      `json:"base"`
}
type Result struct {
	Source     string `json:"source"`
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	Target     string `json:"target"`
	AppID      string `json:"appId,omitempty"`
	CreatorID  string `json:"creatorId,omitempty"`
	ExactSHA   string `json:"exactSha,omitempty"`
	Status     string `json:"status,omitempty"`
	Conclusion string `json:"conclusion,omitempty"`
	Outcome    string `json:"outcome"`
}
type Observation struct {
	RepositoryID     string     `json:"repositoryId"`
	PRID             string     `json:"prId"`
	Number           int64      `json:"number"`
	State            string     `json:"state"`
	Draft            bool       `json:"draft"`
	HeadRepositoryID string     `json:"headRepositoryId"`
	HeadSHA          string     `json:"headSha"`
	BaseSHA          string     `json:"baseSha"`
	BaseRef          string     `json:"baseRef"`
	IntegrationSHA   string     `json:"integrationSha,omitempty"`
	IntegrationType  string     `json:"integrationType"`
	MergedAt         *time.Time `json:"mergedAt,omitempty"`
	PolicyDigest     string     `json:"policyDigest"`
	CI               string     `json:"ci"`
	Results          []Result   `json:"results"`
}

func (c *Client) Observe(ctx context.Context, r Repository, number int64) (Observation, error) {
	var o Observation
	if number < 1 {
		return o, failure("invalid_response")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	s := session{client: c.client, repo: r}
	root := "/repos/" + r.Owner + "/" + r.Name
	var repository repoInfo
	if e := s.get(ctx, root, &repository); e != nil {
		return o, e
	}
	if repository.ID.String() != r.RepositoryID || repository.FullName != r.Owner+"/"+r.Name {
		return o, failure("repository_mismatch")
	}
	path := root + "/pulls/" + strconv.FormatInt(number, 10)
	var p pull
	if e := s.get(ctx, path, &p); e != nil {
		return o, e
	}
	if !positive(p.ID.String()) || p.Number != number || p.Base.Repo == nil || p.Base.Repo.ID.String() != r.RepositoryID || p.Base.Ref != r.BaseBranch || !shaPattern.MatchString(p.Base.SHA) || !shaPattern.MatchString(p.Head.SHA) || p.Head.Repo == nil || !positive(p.Head.Repo.ID.String()) || (p.State != "open" && p.State != "closed") {
		return o, failure("repository_mismatch")
	}
	o = Observation{RepositoryID: r.RepositoryID, PRID: p.ID.String(), Number: number, State: p.State, Draft: p.Draft, HeadRepositoryID: p.Head.Repo.ID.String(), HeadSHA: p.Head.SHA, BaseSHA: p.Base.SHA, BaseRef: p.Base.Ref, IntegrationType: "unavailable", MergedAt: p.MergedAt, PolicyDigest: Digest(r.RequiredChecks), Results: []Result{}}
	if p.Merged {
		o.State = "merged"
		if p.MergeSHA != nil && shaPattern.MatchString(*p.MergeSHA) {
			o.IntegrationSHA = *p.MergeSHA
			o.IntegrationType = "merged_commit"
		}
	} else if p.State == "open" && p.Mergeable != nil && *p.Mergeable && p.MergeSHA != nil && shaPattern.MatchString(*p.MergeSHA) {
		var commit struct {
			SHA     string `json:"sha"`
			Parents []struct {
				SHA string `json:"sha"`
			} `json:"parents"`
		}
		if e := s.get(ctx, root+"/git/commits/"+*p.MergeSHA, &commit); e != nil {
			return Observation{}, e
		}
		if commit.SHA == *p.MergeSHA && len(commit.Parents) == 2 && commit.Parents[0].SHA == p.Base.SHA && commit.Parents[1].SHA == p.Head.SHA {
			o.IntegrationSHA = *p.MergeSHA
			o.IntegrationType = "test_merge"
		}
	}
	// Fetch each SHA/source only once; all rules are evaluated against full pages.
	checks := map[string][]check{}
	statuses := map[string][]commitStatus{}
	for _, rule := range r.RequiredChecks {
		sha := o.HeadSHA
		if rule.Target == "integration" {
			sha = o.IntegrationSHA
		}
		result := Result{Source: rule.Source, Name: rule.Name, Target: rule.Target, AppID: rule.AppID, CreatorID: rule.CreatorID, ExactSHA: sha, Outcome: "unknown"}
		if sha != "" {
			if rule.Source == "check_run" {
				rows, ok := checks[sha]
				if !ok {
					var e error
					rows, e = s.checks(ctx, root, sha)
					if e != nil {
						return Observation{}, e
					}
					checks[sha] = rows
				}
				rows, e := s.suiteDates(ctx, root, sha, rows, rule)
				if e != nil {
					return Observation{}, e
				}
				result = chooseCheck(rows, rule, sha)
			} else {
				rows, ok := statuses[sha]
				if !ok {
					var e error
					rows, e = s.statuses(ctx, root, sha)
					if e != nil {
						return Observation{}, e
					}
					statuses[sha] = rows
				}
				result = chooseStatus(rows, rule, sha)
			}
		}
		o.Results = append(o.Results, result)
	}
	var after pull
	if e := s.get(ctx, path, &after); e != nil {
		return Observation{}, e
	}
	if Digest(p) != Digest(after) {
		return Observation{}, &Failure{Code: "changed_during_sync", Retryable: true}
	}
	o.CI = aggregate(o.Results)
	return o, nil
}

type check struct {
	SuiteCreated time.Time   `json:"-"`
	ID           json.Number `json:"id"`
	Name         string      `json:"name"`
	SHA          string      `json:"head_sha"`
	Status       string      `json:"status"`
	Conclusion   string      `json:"conclusion"`
	App          struct {
		ID json.Number `json:"id"`
	} `json:"app"`
	Suite struct {
		ID json.Number `json:"id"`
	} `json:"check_suite"`
}
type commitStatus struct {
	ID        json.Number `json:"id"`
	Context   string      `json:"context"`
	State     string      `json:"state"`
	CreatedAt time.Time   `json:"created_at"`
	Creator   struct {
		ID json.Number `json:"id"`
	} `json:"creator"`
}

func (s *session) checks(ctx context.Context, root, sha string) ([]check, error) {
	var all []check
	for page := 1; page <= 10; page++ {
		var v struct {
			Total int     `json:"total_count"`
			Rows  []check `json:"check_runs"`
		}
		if e := s.get(ctx, fmt.Sprintf("%s/commits/%s/check-runs?filter=all&per_page=100&page=%d", root, sha, page), &v); e != nil {
			return nil, e
		}
		all = append(all, v.Rows...)
		if len(v.Rows) < 100 {
			if len(all) < v.Total {
				return nil, failure("incomplete")
			}
			return all, nil
		}
	}
	return nil, failure("incomplete")
}
func (s *session) statuses(ctx context.Context, root, sha string) ([]commitStatus, error) {
	var all []commitStatus
	for page := 1; page <= 10; page++ {
		var rows []commitStatus
		if e := s.get(ctx, fmt.Sprintf("%s/commits/%s/statuses?per_page=100&page=%d", root, sha, page), &rows); e != nil {
			return nil, e
		}
		all = append(all, rows...)
		if len(rows) < 100 {
			return all, nil
		}
	}
	return nil, failure("incomplete")
}
func bigger(a, b json.Number) bool {
	as, bs := a.String(), b.String()
	return len(as) > len(bs) || len(as) == len(bs) && as > bs
}
func chooseCheck(rows []check, r Rule, sha string) Result {
	out := Result{Source: r.Source, Name: r.Name, Target: r.Target, AppID: r.AppID, ExactSHA: sha, Outcome: "unknown"}
	var selected *check
	var suite string
	seen := map[string]check{}
	for i := range rows {
		v := &rows[i]
		if v.Name != r.Name || v.App.ID.String() != r.AppID || v.SHA != sha {
			continue
		}
		if !positive(v.ID.String()) || !positive(v.Suite.ID.String()) {
			return out
		}
		if old, ok := seen[v.ID.String()]; ok && Digest(old) != Digest(*v) {
			return out
		}
		seen[v.ID.String()] = *v
		if suite != "" && suite != v.Suite.ID.String() {
			if selected.SuiteCreated.IsZero() || v.SuiteCreated.IsZero() {
				return out
			}
			if v.SuiteCreated.Before(selected.SuiteCreated) || v.SuiteCreated.Equal(selected.SuiteCreated) && !bigger(v.Suite.ID, selected.Suite.ID) {
				continue
			}
			selected = nil
		}
		suite = v.Suite.ID.String()
		if selected == nil || bigger(v.ID, selected.ID) {
			selected = v
		}
	}
	if selected == nil {
		return out
	}
	out.ID = selected.ID.String()
	out.Status = selected.Status
	switch selected.Status {
	case "queued", "in_progress", "pending", "waiting", "requested":
		out.Outcome = "pending"
	case "completed":
		out.Conclusion = selected.Conclusion
		switch selected.Conclusion {
		case "success":
			out.Outcome = "success"
		case "failure", "timed_out", "action_required", "startup_failure":
			out.Outcome = "failure"
		case "cancelled":
			out.Outcome = "cancelled"
		default:
			out.Conclusion = "unknown"
		}
	default:
		out.Status = "unknown"
	}
	return out
}
func chooseStatus(rows []commitStatus, r Rule, sha string) Result {
	out := Result{Source: r.Source, Name: r.Name, Target: r.Target, CreatorID: r.CreatorID, ExactSHA: sha, Outcome: "unknown"}
	var latest *commitStatus
	seen := map[string]commitStatus{}
	for i := range rows {
		v := &rows[i]
		if v.Context == r.Name && v.Creator.ID.String() == r.CreatorID {
			if !positive(v.ID.String()) || v.CreatedAt.IsZero() {
				return out
			}
			if old, ok := seen[v.ID.String()]; ok && Digest(old) != Digest(*v) {
				return out
			}
			seen[v.ID.String()] = *v
			if latest == nil || v.CreatedAt.After(latest.CreatedAt) || v.CreatedAt.Equal(latest.CreatedAt) && bigger(v.ID, latest.ID) {
				latest = v
			}
		}
	}
	if latest != nil {
		out.ID = latest.ID.String()
		switch latest.State {
		case "success":
			out.Status = "success"
			out.Outcome = "success"
		case "pending":
			out.Status = "pending"
			out.Outcome = "pending"
		case "failure", "error":
			out.Status = latest.State
			out.Outcome = "failure"
		default:
			out.Status = "unknown"
		}
	}
	return out
}
func aggregate(rows []Result) string {
	if len(rows) == 0 {
		return "unknown"
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Outcome]++
	}
	for _, s := range []string{"failure", "cancelled", "pending"} {
		if counts[s] > 0 {
			return s
		}
	}
	if counts["success"] == len(rows) {
		return "success"
	}
	return "unknown"
}

// CanonicalURL is constructed locally; provider-supplied links never escape.
func CanonicalURL(r Repository, number int64) string {
	return "https://github.com/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Name) + "/pull/" + strconv.FormatInt(number, 10)
}

func (s *session) suiteDates(ctx context.Context, root, sha string, rows []check, r Rule) ([]check, error) {
	ids := map[string]bool{}
	for _, v := range rows {
		if v.Name == r.Name && v.App.ID.String() == r.AppID && v.SHA == sha && positive(v.Suite.ID.String()) {
			ids[v.Suite.ID.String()] = true
		}
	}
	if len(ids) < 2 {
		return rows, nil
	}
	dates := map[string]time.Time{}
	for id := range ids {
		var suite struct {
			ID      json.Number `json:"id"`
			SHA     string      `json:"head_sha"`
			Created time.Time   `json:"created_at"`
			App     struct {
				ID json.Number `json:"id"`
			} `json:"app"`
		}
		if e := s.get(ctx, root+"/check-suites/"+id, &suite); e != nil {
			return nil, e
		}
		if suite.ID.String() != id || suite.SHA != sha || suite.App.ID.String() != r.AppID || suite.Created.IsZero() {
			return nil, failure("invalid_response")
		}
		dates[id] = suite.Created
	}
	for i := range rows {
		rows[i].SuiteCreated = dates[rows[i].Suite.ID.String()]
	}
	return rows, nil
}
