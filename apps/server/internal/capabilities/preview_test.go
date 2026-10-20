package capabilities

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	gh "github.com/qahnaarln/project-17/apps/server/internal/capabilitygithub"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/manifestregistry"
	"github.com/qahnaarln/project-17/apps/server/internal/previeworigins"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

var (
	shaA = strings.Repeat("a", 40)
	shaB = strings.Repeat("c", 40)
)

type previewFixture struct {
	githubFixture
	ci  *commandbus.Bus
	env string
}

// previewSetup: a submitted request linked to PR #1 of the configured repository,
// a CI bus with preview sources and one allowed preview origin.
func previewSetup(t *testing.T) previewFixture {
	g := githubSetup(t)
	origins, e := previeworigins.Parse("https://pr.example.test, http://127.0.0.1:4100")
	if e != nil {
		t.Fatal(e)
	}
	ci := commandbus.New(g.e.Pool)
	projects.RegisterWith(ci, projects.Preview{Source: g.s.PreviewSource, Origins: origins})
	manifestregistry.RegisterWith(ci, origins)
	g.s.Fetcher = githubFetcher(func(_ context.Context, r gh.Repository, n int64) (gh.Observation, error) { return observed(r, n), nil })
	g.link()
	g.sync()
	return previewFixture{githubFixture: g, ci: ci, env: projects.PreviewName(1)}
}

func (f previewFixture) dispatch(a auth.Actor, bus *commandbus.Bus, name string, p any) (object, error) {
	resp, e := bus.Dispatch(context.Background(), a, commandbus.Request{Name: name, Payload: raw(p), IdempotencyKey: uuid.NewString(), Reason: "preview acceptance"})
	var v struct{ Result object }
	if e == nil {
		e = json.Unmarshal(resp.Body, &v)
	}
	return v.Result, e
}

func (f previewFixture) createEnv() object {
	f.e.T.Helper()
	v, e := f.dispatch(f.e.Admin, f.ci, "create-environment", object{"name": f.env, "kind": "preview", "appUrl": "https://pr.example.test/pr-1", "source": object{"baseEnvironment": "staging", "pullNumber": 1}})
	if e != nil {
		f.e.T.Fatal(e)
	}
	return v
}

// build uploads a code index for commit and returns the manifest payload.
func (f previewFixture) build(commit string, args object) manifestregistry.Payload {
	f.e.T.Helper()
	index := object{"repo": object{"root": ".", "commit": commit}, "elements": object{}, "conventions": object{"registry": "src/registry.ts", "testCommand": "pnpm test", "manifestCommand": "pnpm cms build"}}
	indexHash, _ := manifest.Hash(index)
	m := parse([]byte(cmstest.DefaultManifest))
	m["codeIndex"] = object{"uploaded": false, "hash": indexHash}
	if args != nil {
		m["actions"] = object{"store.favorite": object{"args": args}}
	}
	hash, _ := manifest.Hash(m)
	var upload manifestregistry.UploadedIndex
	f.e.Must(f.e.Admin, "upload-code-index", object{"manifestHash": hash, "indexHash": indexHash, "index": index}, &upload)
	id := upload.UploadID.String()
	head := commit
	return manifestregistry.Payload{Manifest: raw(m), Environment: f.env, CodeIndexUploadID: &id, HeadSHA: &head}
}

func (f previewFixture) register(p manifestregistry.Payload) object {
	f.e.T.Helper()
	v, e := f.dispatch(f.e.Admin, f.ci, "register-manifest", p)
	if e != nil {
		f.e.T.Fatal(e)
	}
	return v
}

func (f previewFixture) environment() store.Environment {
	f.e.T.Helper()
	env, e := f.e.Q.GetEnvironmentByName(context.Background(), store.GetEnvironmentByNameParams{ProjectID: f.a.ProjectID, Name: f.env})
	if e != nil {
		f.e.T.Fatal(e)
	}
	return env
}

func (f previewFixture) state() string {
	f.e.T.Helper()
	r, e := f.e.Q.GetCapabilityRequest(context.Background(), store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: f.request.ID})
	if e != nil {
		f.e.T.Fatal(e)
	}
	m, e := matchPreview(context.Background(), f.e.Q, r, f.environment(), time.Now())
	if e != nil {
		f.e.T.Fatal(e)
	}
	return m.State
}

func (f previewFixture) body() []byte {
	f.e.T.Helper()
	rows, e := f.e.Q.ChangesetWorkingVersions(context.Background(), f.cs)
	if e != nil {
		f.e.T.Fatal(e)
	}
	for _, v := range rows {
		if v.ObjectID == f.doc {
			return v.Body
		}
	}
	f.e.T.Fatal("working version missing")
	return nil
}

func (f previewFixture) project(env uuid.UUID) object {
	f.e.T.Helper()
	cs := f.cs
	out, e := ProjectPreview(context.Background(), f.e.Q, f.a.ProjectID, env, &cs, f.doc, f.body())
	if e != nil {
		f.e.T.Fatal(e)
	}
	return parse(out)
}

func clickAction(doc object) any {
	return obj(arr(obj(obj(obj(doc["nodes"])["n_root"])["on"])["click"])[0])["action"]
}

func TestSTU315_317PreviewEnvironmentAndRegistrationJournal(t *testing.T) {
	f := previewSetup(t)
	before := f.request
	if _, e := f.dispatch(f.e.Admin, f.ci, "create-environment", object{"name": "preview/pr-2", "kind": "preview", "source": object{"baseEnvironment": "staging", "pullNumber": 1}}); cmstest.Code(e) != "VALIDATION_FAILED" {
		t.Fatal("STU-315 name must be preview/pr-<N>", e)
	}
	_, e := f.dispatch(f.e.Admin, f.ci, "create-environment", object{"name": f.env, "kind": "preview", "source": object{"baseEnvironment": "production", "pullNumber": 1}})
	expectCode(t, e, "PREVIEW_SOURCE_INVALID")
	_, e = f.dispatch(f.e.Admin, f.e.Bus, "create-environment", object{"name": f.env, "kind": "preview", "source": object{"baseEnvironment": "staging", "pullNumber": 1}})
	expectCode(t, e, "PREVIEW_SOURCE_INVALID")
	_, e = f.dispatch(f.e.Admin, f.ci, "create-environment", object{"name": f.env, "kind": "preview", "appUrl": "https://evil.example.test/", "source": object{"baseEnvironment": "staging", "pullNumber": 1}})
	expectCode(t, e, "PREVIEW_APP_ORIGIN_NOT_ALLOWED")
	created := f.createEnv()
	again := f.createEnv()
	if created["id"] != again["id"] || obj(created["source"])["repositoryId"] != "42" || created["appUrl"] != "https://pr.example.test/pr-1" {
		t.Fatal("STU-315 idempotent create with configured repository", created, again)
	}
	if f.state() != PreviewNotRegistered {
		t.Fatal(f.state())
	}

	p := f.build(shaA, object{"value": object{"type": "string", "required": true}})
	missing := p
	missing.HeadSHA = nil
	_, e = f.dispatch(f.e.Admin, f.ci, "register-manifest", missing)
	expectCode(t, e, "PREVIEW_COMMIT_MISMATCH")
	wrong := p
	other := shaB
	wrong.HeadSHA = &other
	_, e = f.dispatch(f.e.Admin, f.ci, "register-manifest", wrong)
	expectCode(t, e, "PREVIEW_COMMIT_MISMATCH")
	short := shaA[:7]
	wrong.HeadSHA = &short
	_, e = f.dispatch(f.e.Admin, f.ci, "register-manifest", wrong)
	expectCode(t, e, "PREVIEW_COMMIT_MISMATCH")
	bad := "https://evil.example.test/"
	wrong = p
	wrong.AppURL = &bad
	_, e = f.dispatch(f.e.Admin, f.ci, "register-manifest", wrong)
	expectCode(t, e, "PREVIEW_APP_ORIGIN_NOT_ALLOWED")
	standard := p
	standard.Environment = "staging"
	if _, e = f.dispatch(f.e.Admin, f.ci, "register-manifest", standard); cmstest.Code(e) != "VALIDATION_FAILED" {
		t.Fatal("headSha only for sourced preview", e)
	}

	app := "https://pr.example.test/pr-1?x=1"
	p.AppURL = &app
	_, e = f.dispatch(f.e.Admin, f.ci, "register-manifest", p)
	expectCode(t, e, "PREVIEW_APP_ORIGIN_NOT_ALLOWED")
	app = "https://pr.example.test/pr-1"
	first := obj(f.register(p)["previewRegistration"])
	repeat := obj(f.register(p)["previewRegistration"])
	if first["seq"] != float64(1) || repeat["seq"] != float64(1) || first["commit"] != shaA {
		t.Fatal("STU-316 idempotent registration", first, repeat)
	}
	next := obj(f.register(f.build(shaB, object{"value": object{"type": "string", "required": true}}))["previewRegistration"])
	if next["seq"] != float64(2) {
		t.Fatal("STU-317 new head appends", next)
	}
	var journal int
	f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM preview_registrations`).Scan(&journal)
	env := f.environment()
	if journal != 2 || env.RegistrationSeq != 2 || env.LastRegisteredAt == nil || env.ExpiresAt == nil || env.ExpiresAt.Before(time.Now().Add(13*24*time.Hour)) {
		t.Fatal("STU-317/326 journal and TTL window", journal, env)
	}
	after, _ := f.e.Q.GetCapabilityRequest(context.Background(), store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: f.request.ID})
	if after.Revision != 2 || after.Status != "submitted" || after.ID != before.ID {
		t.Fatal("STU-331 registration changed request", after)
	}
	listed, e := projects.ListEnvironments(context.Background(), f.e.Q, f.a.ProjectID)
	if e != nil || len(listed) != 3 || listed[0].Name != f.env || *listed[0].RegistrationSeq != 2 || listed[0].Source.PullNumber != 1 {
		t.Fatal("environment list exposes preview lifecycle", listed, e)
	}
}

func TestSTU318_323PreviewMatchingArgsAndProjection(t *testing.T) {
	f := previewSetup(t)
	ctx := context.Background()
	f.createEnv()
	value := object{"type": "string", "required": true}
	f.register(f.build(shaA, object{"value": value}))
	env := f.environment()
	if f.state() != PreviewReady {
		t.Fatal("STU-318 matching head", f.state())
	}
	staging, _ := f.e.Q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: f.a.ProjectID, Name: "staging"})
	if clickAction(f.project(env.ID)) != "pending:"+f.request.ID.String() {
		t.Fatal("STU-321 required args must be set before substitution")
	}

	man, _ := f.e.Q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: f.a.ProjectID, ID: *env.ActiveManifestID})
	call := object{"action": "store.favorite", "args": object{"value": object{"lit": "chosen"}}}
	args := SetPreviewArgs{RequestID: f.request.ID, ExpectedRequestRevision: 2, Environment: f.env, ManifestHash: man.Hash, Handlers: raw([]any{call})}
	_, e := f.dispatch(f.engineer, f.e.Bus, "set-capability-preview-args", args)
	if e == nil {
		t.Fatal("STU-327 engineer may not set preview args")
	}
	wrongArgs := args
	wrongArgs.Handlers = raw([]any{object{"action": "store.favorite", "args": object{"value": object{"lit": 42}}}})
	_, e = f.dispatch(f.a, f.e.Bus, "set-capability-preview-args", wrongArgs)
	expectCode(t, e, "PREVIEW_ARGS_INVALID")
	wrongArgs.Handlers = raw([]any{object{"action": "other.action"}})
	_, e = f.dispatch(f.a, f.e.Bus, "set-capability-preview-args", wrongArgs)
	expectCode(t, e, "PREVIEW_ARGS_INVALID")
	saved, e := f.dispatch(f.a, f.e.Bus, "set-capability-preview-args", args)
	if e != nil || saved["revision"] != float64(1) {
		t.Fatal(saved, e)
	}
	_, e = f.dispatch(f.a, f.e.Bus, "set-capability-preview-args", args)
	expectCode(t, e, "CAPABILITY_REVISION_CONFLICT")

	projected := f.project(env.ID)
	if clickAction(projected) != "store.favorite" {
		t.Fatal("STU-320 ready request is substituted", projected)
	}
	if clickAction(f.project(staging.ID)) != "pending:"+f.request.ID.String() || clickAction(parse(f.body())) != "pending:"+f.request.ID.String() {
		t.Fatal("STU-322 standard preview and stored draft keep pending")
	}
	other := uuid.New()
	if out, _ := ProjectPreview(ctx, f.e.Q, f.a.ProjectID, env.ID, &other, f.doc, f.body()); clickAction(parse(out)) != "pending:"+f.request.ID.String() {
		t.Fatal("STU-320 only the request's Change Set")
	}
	if out, _ := ProjectPreview(ctx, f.e.Q, f.a.ProjectID, env.ID, nil, f.doc, f.body()); clickAction(parse(out)) != "pending:"+f.request.ID.String() {
		t.Fatal("STU-322 published reads keep pending")
	}

	list, e := f.s.PreviewGet(ctx, f.e.Q, f.a, f.request.ID)
	item := obj(arr(list["items"])[0])
	if e != nil || list["canOpen"] != true || item["state"] != PreviewReady || obj(item["args"])["status"] != "set" {
		t.Fatal("STU-319 owner view", list, e)
	}
	dev, e := f.s.PreviewGet(ctx, f.e.Q, f.engineer, f.request.ID)
	if e != nil || dev["canOpen"] != false || dev["canSetArgs"] != false {
		t.Fatal("STU-327 engineer sees state, not the page", dev, e)
	}
	cs, e := ChangesetPreview(ctx, f.e.Q, f.a, f.cs, f.env)
	sub := obj(arr(cs["substitutions"])[0])
	if e != nil || sub["substituted"] != true || sub["state"] != PreviewReady {
		t.Fatal("STU-323 Change Set preview", cs, e)
	}
	if _, e = ChangesetPreview(ctx, f.e.Q, f.engineer, f.cs, f.env); e == nil {
		t.Fatal("STU-327 engineer has no page preview")
	}
	if _, e = ChangesetPreview(ctx, f.e.Q, f.a, f.cs, "staging"); e == nil {
		t.Fatal("only PR preview environments")
	}

	f.e.Exec(`UPDATE capability_github_observations SET fresh_until=now()-interval '1 minute'`)
	if f.state() != PreviewReadyLastKnown || clickAction(f.project(env.ID)) != "store.favorite" {
		t.Fatal("§11.2 last-known still substitutes", f.state())
	}

	// A newer head observed on GitHub makes the build outdated.
	f.s.Fetcher = githubFetcher(func(_ context.Context, r gh.Repository, n int64) (gh.Observation, error) {
		o := observed(r, n)
		o.HeadSHA = shaB
		return o, nil
	})
	f.e.Exec(`UPDATE capability_github_links SET refresh_after=now()`)
	l := f.row()
	f.command(f.engineer, "refresh-capability-github-pr", GithubAction{RequestID: f.request.ID, ExpectedRequestRevision: 2, ExpectedConfigRevision: 1, ExpectedLinkRevision: l.Revision})
	f.e.Exec(`UPDATE capability_github_links SET next_at=now()`)
	f.sync()
	if f.state() != PreviewCommitBehind || clickAction(f.project(env.ID)) != "pending:"+f.request.ID.String() {
		t.Fatal("STU-319 commit_behind removes substitution", f.state())
	}
	f.register(f.build(shaB, object{"value": value}))
	if f.state() != PreviewReady || clickAction(f.project(f.environment().ID)) != "pending:"+f.request.ID.String() {
		t.Fatal("STU-321 args of the previous manifest are stale", f.state())
	}
	_, e = f.dispatch(f.a, f.e.Bus, "set-capability-preview-args", args)
	expectCode(t, e, "PREVIEW_ARGS_STALE")

	f.register(f.build(strings.Repeat("d", 40), object{"value": value}))
	if f.state() != PreviewCommitUnverified {
		t.Fatal("registration newer than observation", f.state())
	}
	f.e.Exec(`UPDATE capability_github_observations SET observed_at=now()+interval '1 minute'`)
	f.register(f.build(shaB, object{"value": object{"type": "number", "required": true}}))
	if f.state() != PreviewContractIncompatible {
		t.Fatal(f.state())
	}
	f.register(f.build(shaB, nil))
	if f.state() != PreviewKeyMissing {
		t.Fatal(f.state())
	}
	f.register(f.build(shaB, object{"value": object{"type": "string"}}))
	if f.state() != PreviewReady || clickAction(f.project(f.environment().ID)) != "store.favorite" {
		t.Fatal("STU-321 action without required args needs no form", f.state())
	}

	after, _ := f.e.Q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: f.request.ID})
	changeset, _ := f.e.Q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
	if after.Revision != 2 || after.Status != "submitted" || after.FulfilledManifestID != nil || changeset.Seq != 2 {
		t.Fatal("STU-331 preview never fulfills or edits", after, changeset.Seq)
	}
	l = f.row()
	f.command(f.engineer, "unlink-capability-github-pr", GithubAction{RequestID: f.request.ID, ExpectedRequestRevision: 2, ExpectedLinkRevision: l.Revision})
	if f.state() != PreviewNoGithubLink {
		t.Fatal(f.state())
	}
}

func TestSTU324_326ArchiveDeleteTTLAndClosedPR(t *testing.T) {
	f := previewSetup(t)
	ctx := context.Background()
	f.createEnv()
	f.register(f.build(shaA, object{"value": object{"type": "string"}}))
	var key []byte
	f.e.Pool.QueryRow(ctx, `SELECT preview_key FROM environments WHERE name=$1`, f.env).Scan(&key)

	_, e := f.dispatch(f.e.Admin, f.ci, "delete-environment", object{"environment": "staging"})
	expectCode(t, e, "ENVIRONMENT_NOT_EPHEMERAL")
	_, e = f.dispatch(f.a, f.ci, "delete-environment", object{"environment": f.env})
	expectCode(t, e, "FORBIDDEN")
	seq := int32(7)
	_, e = f.dispatch(f.e.Admin, f.ci, "delete-environment", object{"environment": f.env, "expectedRegistrationSeq": seq})
	expectCode(t, e, "PREVIEW_REGISTRATION_CHANGED")

	deleted, e := f.dispatch(f.e.Admin, f.ci, "delete-environment", object{"environment": f.env, "expectedRegistrationSeq": 1})
	if e != nil || deleted["archiveReason"] != "deleted" || deleted["name"] != f.env {
		t.Fatal("STU-325 delete archives", deleted, e)
	}
	repeat, e := f.dispatch(f.e.Admin, f.ci, "delete-environment", object{"environment": f.env})
	if e != nil || repeat["id"] != deleted["id"] {
		t.Fatal("STU-325 repeated delete", repeat, e)
	}
	var rotated []byte
	f.e.Pool.QueryRow(ctx, `SELECT preview_key FROM environments WHERE id=$1`, uuid.MustParse(deleted["id"].(string))).Scan(&rotated)
	if len(key) != 32 || string(rotated) == string(key) {
		t.Fatal("STU-325 archive rotates the preview key")
	}
	_, e = f.dispatch(f.e.Admin, f.ci, "register-manifest", f.build(shaA, nil))
	expectCode(t, e, "ENVIRONMENT_ARCHIVED")
	list, e := f.s.PreviewGet(ctx, f.e.Q, f.a, f.request.ID)
	if e != nil || len(arr(list["items"])) != 1 || obj(arr(list["items"])[0])["state"] != PreviewEnvironmentExpired {
		t.Fatal("archived environment is shown expired", list, e)
	}
	recreated := f.createEnv()
	if recreated["id"] == deleted["id"] {
		t.Fatal("STU-325 name is released")
	}

	// TTL: only an expired window is archived; a registration extends it.
	f.e.Exec(`UPDATE environments SET expires_at=now()-interval '1 second' WHERE name=$1`, f.env)
	if e = ExpirePreviews(ctx, f.e.Pool); e != nil {
		t.Fatal(e)
	}
	var reason string
	f.e.Pool.QueryRow(ctx, `SELECT archive_reason FROM environments WHERE id=$1`, uuid.MustParse(recreated["id"].(string))).Scan(&reason)
	if reason != "ttl" {
		t.Fatal("STU-326 TTL archive", reason)
	}
	if e = ExpirePreviews(ctx, f.e.Pool); e != nil {
		t.Fatal("TTL sweep is idempotent", e)
	}
	third := f.createEnv()
	f.register(f.build(shaA, object{"value": object{"type": "string"}}))

	// A verified observation of a merged PR archives its previews.
	f.s.Fetcher = githubFetcher(func(_ context.Context, r gh.Repository, n int64) (gh.Observation, error) {
		o := observed(r, n)
		o.State = "merged"
		return o, nil
	})
	f.e.Exec(`UPDATE capability_github_links SET refresh_after=now()`)
	l := f.row()
	f.command(f.engineer, "refresh-capability-github-pr", GithubAction{RequestID: f.request.ID, ExpectedRequestRevision: 2, ExpectedConfigRevision: 1, ExpectedLinkRevision: l.Revision})
	f.e.Exec(`UPDATE capability_github_links SET next_at=now()`)
	f.sync()
	f.e.Pool.QueryRow(ctx, `SELECT archive_reason FROM environments WHERE id=$1`, uuid.MustParse(third["id"].(string))).Scan(&reason)
	if reason != "pr_merged" {
		t.Fatal("STU-326 merged PR archives preview", reason)
	}
	var audits int
	f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM preview_environment_audit WHERE event='archived'`).Scan(&audits)
	if audits != 3 {
		t.Fatal("STU-332 archive audit", audits)
	}
	if listed, _ := projects.ListEnvironments(ctx, f.e.Q, f.a.ProjectID); len(listed) != 2 {
		t.Fatal("archived environments are not listed", listed)
	}
}
