package capabilities

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	gh "github.com/qahnaarln/project-17/apps/server/internal/capabilitygithub"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// STU-333: a storage failure on any table the preview reads is reported; it never
// becomes a silent substitution or a partial answer.
func TestSTU333PreviewReadsReportStorageFailures(t *testing.T) {
	for _, table := range []string{"capability_github_links", "preview_registrations", "capability_github_observations", "capability_preview_args", "manifests", "capability_requests", "environments"} {
		t.Run(table, func(t *testing.T) {
			f := previewSetup(t)
			ctx := context.Background()
			f.createEnv()
			f.register(f.build(shaA, object{"value": object{"type": "string"}}))
			env := f.environment()
			body := f.body()
			args := SetPreviewArgs{RequestID: f.request.ID, ExpectedRequestRevision: 2, Environment: f.env, ManifestHash: "sha256:" + shaA + shaA[:24], Handlers: raw([]any{object{"action": "store.favorite"}})}
			f.e.Break(table, "")
			cs := f.cs
			if out, e := ProjectPreview(ctx, f.e.Q, f.a.ProjectID, env.ID, &cs, f.doc, body); e == nil {
				t.Fatal("projection hid a storage failure", string(out))
			}
			if _, e := f.s.PreviewGet(ctx, f.e.Q, f.a, f.request.ID); e == nil {
				t.Fatal("preview list hid a storage failure")
			}
			if _, e := ChangesetPreview(ctx, f.e.Q, f.a, f.cs, f.env); e == nil {
				t.Fatal("Change Set preview hid a storage failure")
			}
			if _, e := f.dispatch(f.a, f.e.Bus, "set-capability-preview-args", args); e == nil {
				t.Fatal("preview args saved despite a storage failure")
			}
		})
	}
}

func TestSTU333PreviewWritesRollBackOnStorageFailure(t *testing.T) {
	ctx := context.Background()
	t.Run("args", func(t *testing.T) {
		f := previewSetup(t)
		f.createEnv()
		f.register(f.build(shaA, object{"value": object{"type": "string", "required": true}}))
		env := f.environment()
		man, _ := f.e.Q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: f.a.ProjectID, ID: *env.ActiveManifestID})
		f.e.Break("preview_environment_audit", "INSERT")
		_, e := f.dispatch(f.a, f.e.Bus, "set-capability-preview-args", SetPreviewArgs{RequestID: f.request.ID, ExpectedRequestRevision: 2, Environment: f.env, ManifestHash: man.Hash, Handlers: raw([]any{object{"action": "store.favorite", "args": object{"value": object{"lit": "x"}}}})})
		var saved int
		f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM capability_preview_args`).Scan(&saved)
		if e == nil || saved != 0 {
			t.Fatal("args survived a failed audit", e, saved)
		}
	})
	for _, c := range []struct{ table, op, command string }{
		{"preview_environment_audit", "INSERT", "create-environment"},
		{"preview_environment_audit", "INSERT", "register-manifest"},
		{"preview_registrations", "INSERT", "register-manifest"},
		{"preview_environment_audit", "INSERT", "delete-environment"},
		{"presence_leases", "DELETE", "delete-environment"},
	} {
		t.Run(c.command+"/"+c.table, func(t *testing.T) {
			f := previewSetup(t)
			if c.command != "create-environment" {
				f.createEnv()
			}
			payload := any(object{"environment": f.env})
			switch c.command {
			case "create-environment":
				payload = object{"name": f.env, "kind": "preview", "source": object{"baseEnvironment": "staging", "pullNumber": 1}}
			case "register-manifest":
				payload = f.build(shaA, nil)
			}
			f.e.Break(c.table, c.op)
			if _, e := f.dispatch(f.e.Admin, f.ci, c.command, payload); e == nil {
				t.Fatal("storage failure hidden")
			}
			var count, seq int
			f.e.Pool.QueryRow(ctx, `SELECT count(*), coalesce(max(registration_seq),0) FROM environments WHERE kind='preview' AND archived_at IS NULL`).Scan(&count, &seq)
			if c.command == "create-environment" && count != 0 || c.command != "create-environment" && (count != 1 || seq != 0) {
				t.Fatal("partial preview write", count, seq)
			}
		})
	}
}

func TestSTU326LifecycleJobsReportStorageFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("ttl", func(t *testing.T) {
		f := previewSetup(t)
		f.createEnv()
		f.e.Exec(`UPDATE environments SET expires_at=now()-interval '1 second' WHERE name=$1`, f.env)
		f.e.Break("preview_environment_audit", "INSERT")
		if e := ExpirePreviews(ctx, f.e.Pool); e == nil {
			t.Fatal("TTL sweep hid a failure")
		}
		if f.environment().ArchivedAt != nil {
			t.Fatal("partial archive")
		}
		f.e.Break("environments", "")
		if e := ExpirePreviews(ctx, f.e.Pool); e == nil {
			t.Fatal("TTL query failure hidden")
		}
	})
	t.Run("merged", func(t *testing.T) {
		f := previewSetup(t)
		f.createEnv()
		f.s.Fetcher = githubFetcher(func(_ context.Context, r gh.Repository, n int64) (gh.Observation, error) {
			o := observed(r, n)
			o.State = "closed"
			return o, nil
		})
		f.e.Exec(`UPDATE capability_github_links SET refresh_after=now()`)
		l := f.row()
		f.command(f.engineer, "refresh-capability-github-pr", GithubAction{RequestID: f.request.ID, ExpectedRequestRevision: 2, ExpectedConfigRevision: 1, ExpectedLinkRevision: l.Revision})
		f.e.Exec(`UPDATE capability_github_links SET next_at=now()`)
		f.e.Break("preview_environment_audit", "INSERT")
		if e := f.s.SyncGithub(ctx, GithubArgs{ProjectID: f.a.ProjectID, RequestID: f.request.ID}); e == nil {
			t.Fatal("archive failure hidden behind the observation")
		}
		if f.environment().ArchivedAt != nil {
			t.Fatal("partial archive")
		}
	})
	t.Run("expiry worker", func(t *testing.T) {
		f := previewSetup(t)
		f.createEnv()
		f.e.Exec(`UPDATE environments SET expires_at=now()-interval '1 second' WHERE name=$1`, f.env)
		w := &PreviewExpiryWorker{Pool: f.e.Pool}
		if e := w.Work(ctx, nil); e != nil {
			t.Fatal(e)
		}
		var reason string
		f.e.Pool.QueryRow(ctx, `SELECT archive_reason FROM environments WHERE archived_name=$1`, f.env).Scan(&reason)
		if reason != "ttl" || (PreviewExpiryArgs{}).Kind() != "capability_preview_expiry" || (PreviewExpiryArgs{}).InsertOpts().MaxAttempts != 1 {
			t.Fatal("expiry worker", reason)
		}
	})
}

// STU-315: the source resolver refuses environments without an enabled config.
func TestSTU315PreviewSourceResolution(t *testing.T) {
	f := previewSetup(t)
	ctx := context.Background()
	if _, e := f.s.PreviewSource(ctx, f.e.Q, f.a.ProjectID, "missing"); e == nil {
		t.Fatal("unknown base")
	}
	src, e := f.s.PreviewSource(ctx, f.e.Q, f.a.ProjectID, "staging")
	if e != nil || src.RepositoryID != "42" || src.ConfigID == uuid.Nil {
		t.Fatal(src, e)
	}
	f.command(f.admin, "set-capability-github-config", SetGithubConfig{Environment: "staging", ConfigID: "repo", Enabled: false, ExpectedConfigRevision: 1})
	if _, e = f.s.PreviewSource(ctx, f.e.Q, f.a.ProjectID, "staging"); e == nil {
		t.Fatal("disabled configuration accepted")
	}
	_, e = f.dispatch(f.e.Admin, f.ci, "create-environment", object{"name": f.env, "kind": "preview", "source": object{"baseEnvironment": "staging", "pullNumber": 1}})
	expectCode(t, e, "PREVIEW_SOURCE_INVALID")
	f.e.Break("capability_github_configs", "")
	if _, e = f.s.PreviewSource(ctx, f.e.Q, f.a.ProjectID, "staging"); e == nil {
		t.Fatal("storage failure hidden")
	}
	f.e.Break("environments", "")
	if _, e = f.s.PreviewSource(ctx, f.e.Q, f.a.ProjectID, "staging"); e == nil {
		t.Fatal("storage failure hidden")
	}
}

// STU-315/325: lifecycle refusals that do not depend on GitHub facts.
func TestSTU315_325PreviewLifecycleRefusals(t *testing.T) {
	f := previewSetup(t)
	_, e := f.dispatch(f.e.Admin, f.ci, "delete-environment", object{"environment": "Bad Name"})
	expectCode(t, e, "VALIDATION_FAILED")
	_, e = f.dispatch(f.e.Admin, f.ci, "delete-environment", object{"environment": "preview/pr-9"})
	expectCode(t, e, "NOT_FOUND")
	if _, e = f.dispatch(f.e.Admin, f.ci, "create-environment", object{"name": f.env, "kind": "preview"}); e != nil {
		t.Fatal("legacy preview without source", e)
	}
	_, e = f.dispatch(f.e.Admin, f.ci, "create-environment", object{"name": f.env, "kind": "preview", "source": object{"baseEnvironment": "staging", "pullNumber": 1}})
	expectCode(t, e, "ENVIRONMENT_EXISTS")
}

func TestSTU333PreviewLifecycleStorageFailures(t *testing.T) {
	for _, c := range []struct{ table, command string }{
		{"environments", "create-environment"},
		{"environments", "delete-environment"},
		{"preview_registrations", "register-manifest"},
		{"code_index_uploads", "register-manifest"},
	} {
		t.Run(c.command+"/"+c.table, func(t *testing.T) {
			f := previewSetup(t)
			if c.command != "create-environment" {
				f.createEnv()
			}
			payload := any(object{"environment": f.env})
			switch c.command {
			case "create-environment":
				payload = object{"name": f.env, "kind": "preview", "source": object{"baseEnvironment": "staging", "pullNumber": 1}}
			case "register-manifest":
				payload = f.build(shaA, nil)
			}
			f.e.Break(c.table, "")
			if _, e := f.dispatch(f.e.Admin, f.ci, c.command, payload); e == nil {
				t.Fatal("storage failure hidden")
			}
		})
	}
}

// STU-318…321: every mapping and argument refusal is explicit.
func TestSTU318_321PreviewStatesAndArgumentRefusals(t *testing.T) {
	f := previewSetup(t)
	ctx := context.Background()
	f.createEnv()
	value := object{"type": "string", "required": true}
	args := func() SetPreviewArgs {
		man, _ := f.e.Q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: f.a.ProjectID, ID: *f.environment().ActiveManifestID})
		return SetPreviewArgs{RequestID: f.request.ID, ExpectedRequestRevision: 2, Environment: f.env, ManifestHash: man.Hash, Handlers: raw([]any{object{"action": "store.favorite", "args": object{"value": object{"lit": "x"}}}})}
	}
	_, e := f.dispatch(f.a, f.e.Bus, "set-capability-preview-args", SetPreviewArgs{RequestID: f.request.ID, ExpectedRequestRevision: 2, Environment: f.env, ManifestHash: "sha256:" + shaA + shaA[:24], Handlers: raw([]any{object{"action": "store.favorite"}})})
	expectCode(t, e, "PREVIEW_NOT_READY")
	f.register(f.build(shaA, object{"value": value, "extra": object{"type": "boolean", "required": true}}))
	if f.state() != PreviewContractIncompatible {
		t.Fatal(f.state())
	}
	f.register(f.build(shaA, object{"value": value}))
	p := args()
	_, e = f.dispatch(f.a, f.e.Bus, "set-capability-preview-args", SetPreviewArgs{RequestID: p.RequestID, ExpectedRequestRevision: 2, Environment: "preview/pr-9", ManifestHash: p.ManifestHash, Handlers: p.Handlers})
	expectCode(t, e, "NOT_FOUND")
	resp, e := f.e.Bus.Dispatch(ctx, f.a, commandbus.Request{Name: "set-capability-preview-args", Payload: raw(p), IdempotencyKey: uuid.NewString()})
	if e == nil {
		t.Fatal("reason is required", string(resp.Body))
	}
	// The anchor or document no longer holds the request's pending marker.
	r, _ := f.e.Q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: f.request.ID})
	if ok, err := previewCall(ctx, f.e.Q, r, f.environment(), []byte(`{"nodes":{}}`), object{"action": "store.favorite"}, false); ok || err != nil {
		t.Fatal("substituted a moved anchor", ok, err)
	}
	cs := f.cs
	foreign := []byte(`{"nodes":{"n_root":{"id":"n_root","on":{"click":[{"action":"pending:` + uuid.NewString() + `"}]}}}}`)
	if out, err := ProjectPreview(ctx, f.e.Q, f.a.ProjectID, f.environment().ID, &cs, f.doc, foreign); err != nil || string(out) != string(foreign) {
		t.Fatal("unknown pending request changed", err)
	}
	if _, err := ChangesetPreview(ctx, f.e.Q, f.a, uuid.New(), f.env); err == nil {
		t.Fatal("unknown Change Set")
	}
	staging, _ := f.e.Q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: f.a.ProjectID, Name: "staging"})
	if m, err := matchPreview(ctx, f.e.Q, r, staging, time.Now()); err != nil || m.State != PreviewNoGithubLink {
		t.Fatal("standard environment is never a preview", m.State, err)
	}
	f.e.Exec(`UPDATE capability_github_observations SET body=jsonb_set(body,'{repositoryId}','"43"')`)
	if f.state() != PreviewNoGithubLink {
		t.Fatal("foreign repository", f.state())
	}
	f.e.Exec(`UPDATE capability_github_observations SET generation=99`)
	if f.state() != PreviewCommitUnverified {
		t.Fatal("observation of another link generation", f.state())
	}
	f.e.Exec(`DELETE FROM capability_github_observations`)
	f.e.Exec(`DELETE FROM capability_github_audit WHERE request_id IS NOT NULL`)
	f.e.Exec(`DELETE FROM capability_github_links`)
	if f.state() != PreviewNoGithubLink {
		t.Fatal("no link", f.state())
	}
	// A terminal request accepts no preview arguments.
	f.e.Exec(`UPDATE capability_requests SET status='cancelled'`)
	_, e = f.dispatch(f.a, f.e.Bus, "set-capability-preview-args", args())
	expectCode(t, e, "CAPABILITY_STATE_INVALID")
}
