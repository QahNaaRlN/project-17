package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	gh "github.com/qahnaarln/project-17/apps/server/internal/capabilitygithub"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/riverqueue/river"
)

type githubFixture struct {
	fixture
	s     *GithubService
	admin auth.Actor
	input GithubAction
}

func githubSetup(t *testing.T) githubFixture {
	f := setup(t)
	f.create()
	must(f.e, f.a, "submit-capability-request", Action{ID: f.request.ID, ExpectedRevision: 1, ExpectedSeq: 1, ExpectedManifestHash: f.hash})
	admin := f.e.Human("github-admin", auth.ProjectAdmin)
	catalog, e := gh.Parse(raw(object{"repositories": []any{object{"configId": "repo", "projectId": f.a.ProjectID, "environment": "staging", "repositoryId": "42", "owner": "owner", "name": "repo", "baseBranch": "dev", "credentialId": "key", "token": "private-token", "requiredChecks": []any{object{"source": "check_run", "name": "Check", "target": "head", "appId": "7"}}}}}))
	if e != nil {
		t.Fatal(e)
	}
	s := &GithubService{Pool: f.e.Pool, Catalog: catalog}
	s.Register(f.e.Bus)
	g := githubFixture{fixture: f, s: s, admin: admin, input: GithubAction{RequestID: f.request.ID, ExpectedRequestRevision: 2, ExpectedConfigRevision: 1, PullNumber: 1}}
	g.command(admin, "set-capability-github-config", SetGithubConfig{Environment: "staging", ConfigID: "repo", Enabled: true})
	return g
}
func (f githubFixture) call(a auth.Actor, name string, p any, key string) (object, error) {
	if key == "" {
		key = uuid.NewString()
	}
	resp, e := f.e.Bus.Dispatch(context.Background(), a, commandbus.Request{Name: name, Payload: raw(p), IdempotencyKey: key, Reason: "explicit GitHub fact collection"})
	var v struct{ Result object }
	if e == nil {
		e = json.Unmarshal(resp.Body, &v)
	}
	return v.Result, e
}
func (f githubFixture) command(a auth.Actor, name string, p any) object {
	f.e.T.Helper()
	v, e := f.call(a, name, p, "")
	if e != nil {
		f.e.T.Fatalf("%s: %v", name, e)
	}
	return v
}
func (f githubFixture) link() store.CapabilityGithubLink {
	f.command(f.engineer, "link-capability-github-pr", f.input)
	return f.row()
}
func (f githubFixture) row() store.CapabilityGithubLink {
	f.e.T.Helper()
	l, e := f.e.Q.GetCapabilityGithubLink(context.Background(), store.GetCapabilityGithubLinkParams{ProjectID: f.a.ProjectID, RequestID: f.request.ID})
	if e != nil {
		f.e.T.Fatal(e)
	}
	return l
}
func (f githubFixture) sync() store.CapabilityGithubLink {
	f.e.T.Helper()
	if e := f.s.SyncGithub(context.Background(), GithubArgs{ProjectID: f.a.ProjectID, RequestID: f.request.ID}); e != nil {
		f.e.T.Fatal(e)
	}
	return f.row()
}

type githubFetcher func(context.Context, gh.Repository, int64) (gh.Observation, error)

func (f githubFetcher) Observe(c context.Context, r gh.Repository, n int64) (gh.Observation, error) {
	return f(c, r, n)
}
func observed(r gh.Repository, n int64) gh.Observation {
	return gh.Observation{RepositoryID: r.RepositoryID, PRID: "123", Number: n, State: "open", HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40), IntegrationType: "unavailable", CI: "success", Results: []gh.Result{{Source: "check_run", Name: "Check", ExactSHA: strings.Repeat("a", 40), Outcome: "success"}}}
}
func TestSTU295_298GithubCommands(t *testing.T) {
	f := githubSetup(t)
	ctx := context.Background()
	if _, e := f.s.GithubConfig(ctx, f.e.Q, f.a, "staging"); e != nil {
		t.Fatal(e)
	}
	config, e := f.s.GithubConfig(ctx, f.e.Q, f.admin, "staging")
	if e != nil || config["ready"] != true {
		t.Fatal(config, e)
	}
	if _, e = f.s.GithubGet(ctx, f.e.Q, f.admin, f.request.ID); e == nil {
		t.Fatal("admin sees private request")
	}
	before := f.request
	key := uuid.NewString()
	v, e := f.call(f.engineer, "link-capability-github-pr", f.input, key)
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.call(f.engineer, "link-capability-github-pr", f.input, key)
	if e != nil || v["id"] != again["id"] {
		t.Fatal(e)
	}
	l := f.row()
	p := f.input
	p.ExpectedLinkRevision = l.Revision
	f.command(f.engineer, "link-capability-github-pr", p)
	p.PullNumber = 2
	if _, e = f.call(f.engineer, "link-capability-github-pr", p, ""); e == nil {
		t.Fatal("replace without confirmation")
	}
	p.ConfirmReplace = true
	f.command(f.engineer, "link-capability-github-pr", p)
	l = f.row()
	if l.Generation != 2 {
		t.Fatal(l)
	}
	if _, e = f.call(f.a, "refresh-capability-github-pr", GithubAction{RequestID: f.request.ID, ExpectedRequestRevision: 2, ExpectedConfigRevision: 1, ExpectedLinkRevision: l.Revision}, ""); e == nil {
		t.Fatal("owner may not manage")
	}
	f.command(f.engineer, "unlink-capability-github-pr", GithubAction{RequestID: f.request.ID, ExpectedRequestRevision: 2, ExpectedLinkRevision: l.Revision})
	if f.row().Active {
		t.Fatal("still linked")
	}
	after, e := f.e.Q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: f.request.ID})
	if e != nil || after.Revision != 2 || after.Status != "submitted" || string(after.ExternalRefs) != "[]" || before.ID != after.ID {
		t.Fatal("GitHub changed engineering state", after, e)
	}
	wrong := f.input
	wrong.ExpectedRequestRevision = 1
	if _, e = f.call(f.engineer, "link-capability-github-pr", wrong, ""); e == nil {
		t.Fatal("stale request")
	}
}
func TestSTU300_311GithubObservations(t *testing.T) {
	f := githubSetup(t)
	ctx := context.Background()
	f.link()
	calls := 0
	f.s.Fetcher = githubFetcher(func(_ context.Context, r gh.Repository, n int64) (gh.Observation, error) {
		calls++
		return observed(r, n), nil
	})
	l := f.sync()
	if calls != 1 || l.State != "queued" || l.PollsLeft != 4 {
		t.Fatal(l, calls)
	}
	f.sync()
	if calls != 1 {
		t.Fatal("polled early")
	}
	view, e := f.s.GithubGet(ctx, f.e.Q, f.a, f.request.ID)
	if e != nil || view["current"] == nil {
		t.Fatal(view, e)
	}
	history, e := f.s.GithubHistory(ctx, f.e.Q, f.engineer, f.request.ID, 1, "")
	if e != nil || len(history["items"].([]any)) != 1 {
		t.Fatal(history, e)
	}
	f.e.Exec(`UPDATE capability_github_links SET next_at=now()-interval '1 minute' WHERE request_id=$1`, f.request.ID)
	f.s.Fetcher = githubFetcher(func(context.Context, gh.Repository, int64) (gh.Observation, error) {
		return gh.Observation{}, &gh.Failure{Code: "rate_limited", Retryable: true, RetryAt: time.Now().Add(2 * time.Minute)}
	})
	l = f.sync()
	if l.State != "retry_wait" || l.LastError != "rate_limited" {
		t.Fatal(l)
	}
	view, e = f.s.GithubGet(ctx, f.e.Q, f.a, f.request.ID)
	if e != nil || view["current"] != nil || view["lastKnown"] == nil {
		t.Fatal(view, e)
	}
	f.e.Exec(`UPDATE capability_github_links SET next_at=now()-interval '1 minute' WHERE request_id=$1`, f.request.ID)
	f.sync()
	if f.row().Attempts != 1 {
		t.Fatal("credential block ignored")
	}
	f.e.Exec(`UPDATE capability_github_credentials SET blocked_until=now()-interval '1 minute'`)
	f.e.Exec(`UPDATE capability_github_links SET next_at=now()-interval '1 minute' WHERE request_id=$1`, f.request.ID)
	f.s.Fetcher = githubFetcher(func(_ context.Context, r gh.Repository, n int64) (gh.Observation, error) { return observed(r, n), nil })
	f.sync()
	f.e.Exec(`UPDATE capability_github_observations SET fresh_until=now()-interval '1 second'`)
	view, _ = f.s.GithubGet(ctx, f.e.Q, f.a, f.request.ID)
	if view["current"] != nil || view["syncState"] != "stale" {
		t.Fatal(view)
	}
	if _, e = f.s.GithubHistory(ctx, f.e.Q, f.a, f.request.ID, 101, ""); e == nil {
		t.Fatal("limit")
	}
	if _, e = f.s.GithubHistory(ctx, f.e.Q, f.a, f.request.ID, 20, "bad"); e == nil {
		t.Fatal("cursor")
	}
	history, _ = f.s.GithubHistory(ctx, f.e.Q, f.a, f.request.ID, 1, "")
	cursor := history["nextCursor"].(string)
	next, e := f.s.GithubHistory(ctx, f.e.Q, f.a, f.request.ID, 1, cursor)
	if e != nil || len(next["items"].([]any)) != 1 {
		t.Fatal(next, e)
	}
	if _, e = f.s.GithubHistory(ctx, f.e.Q, f.engineer, f.request.ID, 1, cursor); e == nil {
		t.Fatal("actor scope cursor")
	}
}
func TestSTU306_307GithubLateResponse(t *testing.T) {
	f := githubSetup(t)
	f.link()
	f.s.Fetcher = githubFetcher(func(_ context.Context, r gh.Repository, n int64) (gh.Observation, error) {
		l := f.row()
		f.command(f.engineer, "unlink-capability-github-pr", GithubAction{RequestID: f.request.ID, ExpectedRequestRevision: 2, ExpectedLinkRevision: l.Revision})
		return observed(r, n), nil
	})
	l := f.sync()
	if l.Active || l.State != "stopped" {
		t.Fatal(l)
	}
	if _, e := f.e.Q.LatestCapabilityGithubObservation(context.Background(), store.LatestCapabilityGithubObservationParams{ProjectID: f.a.ProjectID, RequestID: f.request.ID}); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("late result applied", e)
	}
}
func TestSTU307GithubRevocation(t *testing.T) {
	f := githubSetup(t)
	f.link()
	f.e.Exec(`DELETE FROM role_bindings WHERE project_id=$1 AND actor_id=$2`, f.a.ProjectID, f.engineer.ID)
	f.s.Fetcher = githubFetcher(func(context.Context, gh.Repository, int64) (gh.Observation, error) {
		t.Fatal("network after revocation")
		return gh.Observation{}, nil
	})
	l := f.sync()
	if l.State != "stopped" || l.LastError != "auth_revoked" {
		t.Fatal(l)
	}
}
func TestSTU305GithubRetryRecovery(t *testing.T) {
	f := githubSetup(t)
	f.link()
	calls := 0
	f.s.Fetcher = githubFetcher(func(context.Context, gh.Repository, int64) (gh.Observation, error) {
		calls++
		return gh.Observation{}, &gh.Failure{Code: "temporary_unavailable", Retryable: true}
	})
	for i := 0; i < 3; i++ {
		f.e.Exec(`UPDATE capability_github_links SET next_at=now()-interval '1 minute'`)
		f.sync()
	}
	l := f.row()
	if l.State != "stopped" || l.Attempts != 3 || calls != 3 {
		t.Fatal(l, calls)
	}
	f.e.Exec(`UPDATE capability_github_links SET refresh_after=now()-interval '1 minute'`)
	f.command(f.engineer, "refresh-capability-github-pr", GithubAction{RequestID: f.request.ID, ExpectedRequestRevision: 2, ExpectedLinkRevision: l.Revision, ExpectedConfigRevision: 1})
	l = f.row()
	if l.Generation != 2 || l.Attempts != 0 {
		t.Fatal(l)
	}
	f.e.Exec(`UPDATE capability_github_links SET state='sending',attempts=1,lease_token=$1,lease_until=now()-interval '1 minute'`, uuid.New())
	f.sync()
	if f.row().Attempts != 2 {
		t.Fatal("expired lease not recovered")
	}
	f.command(f.admin, "set-capability-github-config", SetGithubConfig{Environment: "staging", ConfigID: "repo", Enabled: false, ExpectedConfigRevision: 1})
	l = f.sync()
	if l.State != "stopped" {
		t.Fatal(l)
	}
}

func TestSTU308GithubQueriesNeverHideStorageErrors(t *testing.T) {
	f := githubSetup(t)
	f.link()
	f.s.Fetcher = githubFetcher(func(_ context.Context, r gh.Repository, n int64) (gh.Observation, error) { return observed(r, n), nil })
	f.sync()
	ctx := context.Background()
	for _, needle := range []string{"FROM capability_requests", "FROM capability_github_configs", "FROM capability_github_links", "FROM capability_github_observations", "FROM actors", "FROM role_bindings"} {
		q := store.New(faultDB{DBTX: f.e.Pool, needle: needle})
		if _, e := f.s.GithubGet(ctx, q, f.a, f.request.ID); !errors.Is(e, storageFailure) {
			t.Fatal(needle, e)
		}
	}
	for _, needle := range []string{"FROM capability_requests", "FROM capability_github_observations"} {
		q := store.New(faultDB{DBTX: f.e.Pool, needle: needle})
		if _, e := f.s.GithubHistory(ctx, q, f.a, f.request.ID, 0, ""); !errors.Is(e, storageFailure) {
			t.Fatal(needle, e)
		}
	}
	for _, needle := range []string{"FROM environments", "FROM capability_github_configs"} {
		q := store.New(faultDB{DBTX: f.e.Pool, needle: needle})
		if _, e := f.s.GithubConfig(ctx, q, f.admin, "staging"); !errors.Is(e, storageFailure) {
			t.Fatal(needle, e)
		}
	}
	if _, e := f.s.GithubGet(ctx, f.e.Q, f.admin, f.request.ID); e == nil {
		t.Fatal("admin reads request")
	}
	if _, e := f.s.GithubHistory(ctx, f.e.Q, f.admin, f.request.ID, 0, ""); e == nil {
		t.Fatal("admin reads history")
	}
	if _, e := f.s.GithubConfig(ctx, f.e.Q, auth.Actor{Kind: auth.ActorAgent}, "staging"); e == nil {
		t.Fatal("agent sees settings")
	}
	if _, e := f.s.GithubConfig(ctx, f.e.Q, f.admin, "missing"); e == nil {
		t.Fatal("unknown env")
	}
	f.e.Exec(`UPDATE capability_github_configs SET enabled=false`)
	v, e := f.s.GithubGet(ctx, f.e.Q, f.a, f.request.ID)
	if e != nil || v["current"] != nil || v["syncState"] != "disabled" {
		t.Fatal(v, e)
	}
}
func TestSTU298GithubCommandStorageRollback(t *testing.T) {
	for _, stage := range []string{"config", "link", "unlink"} {
		for _, needle := range []string{"FROM environments", "FROM actors", "FROM role_bindings", "FROM capability_github_configs", "FROM capability_github_links", "INSERT INTO capability_github_audit"} {
			t.Run(stage+"/"+needle, func(t *testing.T) {
				f := githubSetup(t)
				if stage == "unlink" {
					f.link()
				}
				p := f.input
				if stage == "unlink" {
					p.ExpectedLinkRevision = 1
					p.PullNumber = 0
					p.ExpectedConfigRevision = 0
				}
				if stage == "config" && needle == "FROM capability_github_links" {
					needle = "UPDATE capability_github_links"
				}
				commandbus.Register(f.e.Bus, commandbus.Command[GithubAction, object]{Name: "test-github-fault", Right: auth.CodePropose, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p GithubAction) (object, error) {
					ft := faultTx{Tx: tx, needle: needle}
					if stage == "config" {
						return f.s.setGithubConfig(ctx, ft, f.admin, SetGithubConfig{Environment: "staging", ConfigID: "repo", Enabled: false, ExpectedConfigRevision: 1})
					}
					name := "link-capability-github-pr"
					if stage == "unlink" {
						name = "unlink-capability-github-pr"
					}
					return f.s.githubAction(ctx, ft, a, p, name)
				}})
				_, e := f.call(f.engineer, "test-github-fault", p, "")
				if !errors.Is(e, storageFailure) {
					t.Fatal(needle, e)
				}
				if stage == "config" {
					c, _ := f.e.Q.GetCapabilityGithubConfig(context.Background(), store.GetCapabilityGithubConfigParams{ProjectID: f.a.ProjectID, Environment: "staging"})
					if c.Revision != 1 || !c.Enabled {
						t.Fatal("partial config")
					}
				}
			})
		}
	}
}
func TestSTU296GithubValidationAndBudgets(t *testing.T) {
	f := githubSetup(t)
	ctx := context.Background()
	for _, p := range []GithubAction{{}, {RequestID: f.request.ID, ExpectedRequestRevision: 2, PullNumber: -1}, {RequestID: f.request.ID, ExpectedRequestRevision: 2, ExpectedLinkRevision: -1, PullNumber: 1}} {
		if _, e := f.call(f.engineer, "link-capability-github-pr", p, ""); e == nil {
			t.Fatal("validation")
		}
	}
	if _, e := f.call(f.admin, "set-capability-github-config", SetGithubConfig{}, ""); e == nil {
		t.Fatal("bad config")
	}
	if _, e := f.call(f.admin, "set-capability-github-config", SetGithubConfig{Environment: "missing", ConfigID: "repo"}, ""); e == nil {
		t.Fatal("env")
	}
	if _, e := f.call(f.admin, "set-capability-github-config", SetGithubConfig{Environment: "staging", ConfigID: "missing", ExpectedConfigRevision: 1}, ""); e == nil {
		t.Fatal("catalog")
	}
	if _, e := f.call(f.admin, "set-capability-github-config", SetGithubConfig{Environment: "staging", ConfigID: "repo", ExpectedConfigRevision: 0}, ""); e == nil {
		t.Fatal("config conflict")
	}
	p := f.input
	p.ExpectedConfigRevision = 2
	if _, e := f.call(f.engineer, "link-capability-github-pr", p, ""); e == nil {
		t.Fatal("wrong config")
	}
	p = f.input
	p.PullNumber = 0
	if _, e := f.call(f.engineer, "unlink-capability-github-pr", p, ""); e == nil {
		t.Fatal("unlink absent")
	}
	f.link()
	l := f.row()
	p = GithubAction{RequestID: f.request.ID, ExpectedRequestRevision: 2, ExpectedLinkRevision: l.Revision, ExpectedConfigRevision: 1}
	f.command(f.engineer, "refresh-capability-github-pr", p) // coalesced queued job
	f.e.Exec(`UPDATE capability_github_links SET state='idle'`)
	if _, e := f.call(f.engineer, "refresh-capability-github-pr", p, ""); e == nil {
		t.Fatal("cooldown")
	}
	f.e.Exec(`UPDATE capability_github_links SET refresh_after=now()-interval '1 minute'`)
	for i := 0; i < 20; i++ {
		if e := f.e.Q.InsertCapabilityGithubAudit(ctx, store.InsertCapabilityGithubAuditParams{ID: uuid.New(), ProjectID: f.a.ProjectID, RequestID: &f.request.ID, ActorID: &f.engineer.ID, Event: "refresh", Reason: "budget test"}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := f.call(f.engineer, "refresh-capability-github-pr", p, ""); e == nil {
		t.Fatal("project budget")
	}
}

func TestSTU298NoQueueRollsBackLink(t *testing.T) {
	f := githubSetup(t)
	bus := commandbus.New(f.e.Pool)
	f.s.Register(bus)
	_, e := bus.Dispatch(context.Background(), f.engineer, commandbus.Request{Name: "link-capability-github-pr", IdempotencyKey: uuid.NewString(), Reason: "atomic queue test", Payload: raw(f.input)})
	if !errors.Is(e, jobs.ErrNoQueue) {
		t.Fatal(e)
	}
	_, e = f.e.Q.GetCapabilityGithubLink(context.Background(), store.GetCapabilityGithubLinkParams{ProjectID: f.a.ProjectID, RequestID: f.request.ID})
	if !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("partial link", e)
	}
}
func TestSTU306GithubWorkerRecoveryAndFiniteWindow(t *testing.T) {
	f := githubSetup(t)
	ctx := context.Background()
	f.link()
	queue, e := jobs.NewInserter(f.e.Pool)
	if e != nil {
		t.Fatal(e)
	}
	worker := &GithubRecoveryWorker{Service: f.s, Queue: queue}
	if e = worker.Work(ctx, &river.Job[GithubRecoveryArgs]{}); e != nil {
		t.Fatal(e)
	}
	if (GithubRecoveryArgs{}).Kind() == "" || (GithubRecoveryArgs{}).InsertOpts().MaxAttempts != 1 {
		t.Fatal("recovery opts")
	}
	f.s.Fetcher = githubFetcher(func(_ context.Context, r gh.Repository, n int64) (gh.Observation, error) { return observed(r, n), nil })
	w := &GithubWorker{Service: f.s}
	if e = w.Work(ctx, &river.Job[GithubArgs]{Args: GithubArgs{ProjectID: f.a.ProjectID, RequestID: f.request.ID}}); e != nil {
		t.Fatal(e)
	}
	f.e.Exec(`UPDATE capability_github_links SET next_at=now()-interval '1 minute',polls_left=0`)
	f.sync()
	if f.row().State != "idle" {
		t.Fatal("polling budget ignored")
	}
	f.e.Exec(`UPDATE capability_github_links SET state='queued',next_at=now()-interval '1 minute',window_until=now()-interval '1 second'`)
	l := f.sync()
	if l.State != "stopped" || l.LastError != "exhausted" {
		t.Fatal(l)
	}
	if e = f.s.SyncGithub(ctx, GithubArgs{ProjectID: f.a.ProjectID, RequestID: uuid.New()}); e != nil {
		t.Fatal(e)
	}
}
func TestSTU307GithubTerminalAndInflightRevocation(t *testing.T) {
	for _, mode := range []string{"terminal", "config", "inflight", "expired", "lease_wait", "unknown_error"} {
		t.Run(mode, func(t *testing.T) {
			f := githubSetup(t)
			f.link()
			f.s.Fetcher = githubFetcher(func(_ context.Context, r gh.Repository, n int64) (gh.Observation, error) {
				if mode == "inflight" {
					f.e.Exec(`DELETE FROM role_bindings WHERE project_id=$1 AND actor_id=$2`, f.a.ProjectID, f.engineer.ID)
				}
				if mode == "unknown_error" {
					return gh.Observation{}, errors.New("private provider details")
				}
				return observed(r, n), nil
			})
			switch mode {
			case "terminal":
				f.e.Exec(`UPDATE capability_requests SET status='cancelled'`)
			case "config":
				f.e.Exec(`UPDATE capability_github_configs SET fingerprint='changed'`)
			case "expired":
				f.e.Exec(`UPDATE capability_github_links SET state='sending',lease_token=$1,lease_until=now()+interval '1 minute'`, uuid.New())
			case "lease_wait":
				f.e.Exec(`INSERT INTO capability_github_credentials(credential_id,lease_token,lease_until) VALUES('key',$1,now()+interval '1 minute')`, uuid.New())
			}
			l := f.sync()
			switch mode {
			case "expired":
				if l.State != "sending" {
					t.Fatal(l)
				}
			case "lease_wait":
				if l.State != "retry_wait" || l.Attempts != 0 {
					t.Fatal(l)
				}
			case "unknown_error":
				if l.LastError != "temporary_unavailable" || l.State != "retry_wait" {
					t.Fatal(l)
				}
			default:
				if l.State != "stopped" {
					t.Fatal(l)
				}
			}
		})
	}
}
func TestSTU307GithubGateStorageFailures(t *testing.T) {
	f := githubSetup(t)
	f.link()
	ctx := context.Background()
	for _, needle := range []string{"FROM environments", "FROM capability_requests", "FROM capability_github_configs", "FROM capability_github_links", "FROM actors", "FROM role_bindings"} {
		tx, e := f.e.Pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		_, _, _, _, e = f.s.lockGithub(ctx, faultTx{Tx: tx, needle: needle}, GithubArgs{ProjectID: f.a.ProjectID, RequestID: f.request.ID})
		tx.Rollback(ctx)
		if !errors.Is(e, storageFailure) {
			t.Fatal(needle, e)
		}
	}
}

func TestSTU295GithubCatalogReferencesExistingEnvironment(t *testing.T) {
	f := githubSetup(t)
	if e := f.s.ValidateCatalog(context.Background()); e != nil {
		t.Fatal(e)
	}
	repository := f.s.Catalog.All()[0]
	repository.Environment = "missing"
	catalog, e := gh.Parse(raw(object{"repositories": []gh.Repository{repository}}))
	if e != nil {
		t.Fatal(e)
	}
	bad := &GithubService{Pool: f.e.Pool, Catalog: catalog}
	if bad.ValidateCatalog(context.Background()) != gh.ErrConfig {
		t.Fatal("unknown environment accepted")
	}
}
