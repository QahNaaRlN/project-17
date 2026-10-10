package manifestregistry_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/manifestregistry"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"strings"
	"sync"
	"testing"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

const base = cmstest.DefaultManifest
const native = `{"manifestVersion":"1.0","app":{"id":"test","version":"1.0.0","framework":"react"},"irVersions":["1.0"],"breakpoints":{},"tokens":{},"components":{"Native":{"props":{"title":{"type":"string"}}}}}`

func setup(t *testing.T) *cmstest.Env {
	e := cmstest.New(t, manifestregistry.Register, workflow.Register, publishing.Register)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	return e
}
func payload(raw, environment string) manifestregistry.Payload {
	return manifestregistry.Payload{Manifest: json.RawMessage(raw), Environment: environment}
}
func register(e *cmstest.Env, raw, env string) manifestregistry.Registration {
	var r manifestregistry.Registration
	e.Must(e.Admin, "register-manifest", payload(raw, env), &r)
	return r
}
func schemaManifest(version string, field string) string {
	return strings.TrimSuffix(base, "}") + `,"schemas":{"Product":{"version":` + version + `,"fields":{"title":{"type":"` + field + `"}}}}}`
}
func count(e *cmstest.Env, table string) int {
	var n int
	if err := e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		e.T.Fatal(err)
	}
	return n
}
func detail(e *cmstest.Env, id uuid.UUID) changes.ChangesetDetail {
	c, err := changes.GetChangeset(context.Background(), e.Q, e.Admin.ProjectID, id)
	if err != nil {
		e.T.Fatal(err)
	}
	return c
}
func active(e *cmstest.Env, env string) manifestregistry.Active {
	r, err := manifestregistry.GetActive(context.Background(), e.Q, e.Admin.ProjectID, env)
	if err != nil {
		e.T.Fatal(err)
	}
	return r
}
func createNative(e *cmstest.Env) (uuid.UUID, uuid.UUID) {
	var cs changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "native"}, &cs)
	var r changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "root": map[string]any{"id": "n_root", "type": "Native", "props": map[string]any{"title": "ok"}}}}}}, &r)
	return cs.ID, r.Operations[0].Target
}

// MF-002/004/020/021: immutable content, canonical hashes, service-only authorization.
func TestRegistrationAndAuthorization(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	human := e.Human("developer", auth.ManifestRegister)
	for _, actor := range []auth.Actor{human, {ID: e.Admin.ID, Kind: auth.ActorService, ProjectID: e.Admin.ProjectID, Rights: auth.RightSet{}}} {
		if code := cmstest.Code(e.Do(actor, "register-manifest", payload(base, "staging"), nil)); code != "FORBIDDEN" {
			t.Fatal(code)
		}
	}
	for _, tc := range []struct {
		p    manifestregistry.Payload
		code string
	}{
		{payload(base, ""), "VALIDATION_FAILED"}, {payload(`null`, "staging"), "MANIFEST_INVALID"}, {payload(`{}`, "staging"), "MANIFEST_INVALID"}, {payload(strings.Replace(base, `"test"`, `"\ud800"`, 1), "staging"), "VALIDATION_FAILED"},
		{manifestregistry.Payload{Manifest: json.RawMessage(base), Environment: "staging", CodeIndexUploadID: new(string)}, "CODE_INDEX_UPLOAD_NOT_READY"},
		{payload(strings.TrimSuffix(base, "}")+`,"codeIndex":{"uploaded":true,"hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`, "staging"), "CODE_INDEX_UPLOAD_NOT_READY"},
		{payload(base, "unknown"), "NOT_FOUND"},
	} {
		if code := cmstest.Code(e.Do(e.Admin, "register-manifest", tc.p, nil)); code != tc.code {
			t.Fatalf("want %s got %s", tc.code, code)
		}
	}
	r := register(e, native, "staging")
	if r.Activation != "active" || r.ChangesetID != nil {
		t.Fatal(r)
	}
	m, err := e.Q.GetManifestByHash(ctx, store.GetManifestByHashParams{ProjectID: e.Admin.ProjectID, Hash: r.ManifestHash})
	if err != nil {
		t.Fatal(err)
	}
	if m.AppVersion != "1.0.0" {
		t.Fatal(m.AppVersion)
	}
	other := e.Admin
	other.ID = e.Human("ci-author", auth.ManifestRegister).ID
	raw := strings.ReplaceAll(native, ",", ", ")
	var again manifestregistry.Registration
	e.Must(other, "register-manifest", payload(raw, "production"), &again)
	saved, err := e.Q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: e.Admin.ProjectID, ID: m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if saved.RegisteredBy != m.RegisteredBy || !saved.CreatedAt.Equal(m.CreatedAt) || again.ManifestHash != r.ManifestHash {
		t.Fatal("immutable manifest changed")
	}
	schemas, err := manifestregistry.GetSchemas(ctx, e.Q, e.Admin.ProjectID, "staging")
	if err != nil || string(schemas["schemas"].(json.RawMessage)) != "{}" {
		t.Fatal(schemas, err)
	}
	for _, env := range []string{"", "missing"} {
		if _, err := manifestregistry.GetActive(ctx, e.Q, e.Admin.ProjectID, env); err == nil {
			t.Fatal(env)
		}
		if _, err := manifestregistry.GetSchemas(ctx, e.Q, e.Admin.ProjectID, env); err == nil {
			t.Fatal(env)
		}
	}
	e.Exec("UPDATE environments SET active_manifest_id=NULL WHERE name='staging'")
	if _, err := manifestregistry.GetActive(ctx, e.Q, e.Admin.ProjectID, "staging"); cmstest.Code(err) != "MANIFEST_NOT_READY" {
		t.Fatal(err)
	}
	if _, err := manifestregistry.GetActive(ctx, e.Q, uuid.New(), "production"); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
}

// MF-003/025/026/033, CNT-002: first standard schemas wait, preview schemas are isolated.
func TestCandidatesAndPreviewIsolation(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.Exec("UPDATE environments SET active_manifest_id=NULL WHERE name='staging'")
	raw := schemaManifest("1.0", "text")
	r := register(e, raw, "staging")
	again := register(e, raw, "staging")
	if r.Activation != "pending" || r.ChangesetID == nil || *r.ChangesetID != *again.ChangesetID || count(e, "schema_versions") != 0 {
		t.Fatal(r, again)
	}
	if _, err := manifestregistry.GetActive(ctx, e.Q, e.Admin.ProjectID, "staging"); cmstest.Code(err) != "MANIFEST_NOT_READY" {
		t.Fatal(err)
	}
	cs := detail(e, *r.ChangesetID)
	if cs.Kind != "schema" || cs.CandidateManifestHash == nil || *cs.CandidateManifestHash != r.ManifestHash || cs.State != "open" {
		t.Fatal(cs)
	}
	c, err := validation.Load(ctx, e.Q, e.Admin.ProjectID, "staging", r.ChangesetID)
	if err != nil || c.ManifestHash != r.ManifestHash {
		t.Fatal(c, err)
	}
	e.Must(e.Admin, "create-environment", map[string]any{"name": "preview/pr-1", "kind": "preview"}, nil)
	preview := register(e, raw, "preview/pr-1")
	register(e, raw, "preview/pr-1")
	if preview.Activation != "active" || preview.ChangesetID != nil || count(e, "preview_schema_snapshots") != 1 || count(e, "schema_versions") != 0 {
		t.Fatal(preview)
	}
	schemas, err := manifestregistry.GetSchemas(ctx, e.Q, e.Admin.ProjectID, "preview/pr-1")
	if err != nil || !strings.Contains(string(schemas["schemas"].(json.RawMessage)), "Product") {
		t.Fatal(schemas, err)
	}
	newer := register(e, schemaManifest("2", "text"), "preview/pr-1")
	if newer.ManifestHash == preview.ManifestHash || count(e, "preview_schema_snapshots") != 2 {
		t.Fatal(newer)
	}
	register(e, base, "staging")
	c, err = validation.Load(ctx, e.Q, e.Admin.ProjectID, "staging", r.ChangesetID)
	if cmstest.Code(err) != "MANIFEST_CANDIDATE_MISMATCH" {
		t.Fatal(c, err)
	}
	stale := detail(e, *r.ChangesetID)
	if !stale.NeedsAttention {
		t.Fatal(stale)
	}
	next := register(e, raw, "staging")
	if *next.ChangesetID == *r.ChangesetID {
		t.Fatal("reused stale base")
	}
	e.Exec("UPDATE changesets SET state='abandoned' WHERE id=$1", *next.ChangesetID)
	replacement := register(e, raw, "staging")
	if *replacement.ChangesetID == *next.ChangesetID {
		t.Fatal("reused abandoned candidate")
	}
}

// CNT-002: preview origin never bypasses the global standard version registry.
func TestStandardSchemaVersions(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	raw := schemaManifest("2", "text")
	id := e.ActivateManifest("production", []byte(raw))
	if err := e.Q.InsertStandardSchemaVersion(ctx, store.InsertStandardSchemaVersionParams{ProjectID: e.Admin.ProjectID, SchemaName: "Product", Version: 2, Body: []byte(`{"version":2,"fields":{"title":{"type":"text"}}}`), ManifestID: id}); err != nil {
		t.Fatal(err)
	}
	e.Must(e.Admin, "create-environment", map[string]any{"name": "preview/check", "kind": "preview"}, nil)
	changed := schemaManifest("2", "number")
	register(e, changed, "preview/check")
	for _, bad := range []string{changed, schemaManifest("1", "text"), schemaManifest("2147483648", "text")} {
		before := active(e, "staging")
		err := e.Do(e.Admin, "register-manifest", payload(bad, "staging"), nil)
		if err == nil || active(e, "staging").ManifestHash != before.ManifestHash {
			t.Fatal("invalid version activated", err)
		}
	}
	reused := register(e, raw, "staging")
	if reused.Activation != "pending" {
		t.Fatal(reused)
	}
	advanced := register(e, schemaManifest("3", "number"), "staging")
	if advanced.Activation != "pending" || count(e, "schema_versions") != 1 {
		t.Fatal(advanced)
	}
}

// MF-022/023, CHG-034, PUB-020: drafts get diagnostics; published dependencies prevent activation.
func TestDraftAttentionPublicationAndRollback(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	register(e, native, "staging")
	register(e, native, "production")
	cs, doc := createNative(e)
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1, "targets": []string{"staging", "production"}}, nil)
	initial := detail(e, cs)
	if initial.NeedsAttention || initial.State != "approved" || len(initial.ManifestDiagnostics) != 2 {
		t.Fatal(initial)
	}
	broken := register(e, base, "staging")
	if broken.Activation != "active" {
		t.Fatal(broken)
	}
	attention := detail(e, cs)
	if !attention.NeedsAttention || attention.State != "approved" || attention.Seq != initial.Seq {
		t.Fatal(attention)
	}
	if attention.ManifestDiagnostics[1].Stale || len(attention.ManifestDiagnostics[1].Documents) == 0 {
		t.Fatal(attention.ManifestDiagnostics)
	}
	if len(attention.ManifestDiagnostics[0].Documents) != 0 {
		t.Fatal("production diagnostics overwritten")
	}
	for _, table := range []string{"publications", "published_pointers", "routes", "river_job"} {
		if count(e, table) != 0 {
			t.Fatal(table)
		}
	}
	if code := cmstest.Code(e.Do(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)); code != "VALIDATION_FAILED" {
		t.Fatal(code)
	}
	o, err := e.Q.GetObject(ctx, store.GetObjectParams{ProjectID: e.Admin.ProjectID, ID: doc})
	if err != nil || o.HeadVersionID != nil {
		t.Fatal(o, err)
	}
	register(e, native, "staging")
	if detail(e, cs).NeedsAttention {
		t.Fatal("successful recheck did not clear attention")
	}
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)
	before := active(e, "staging")
	n := count(e, "manifests")
	withSchema := strings.TrimSuffix(base, "}") + `,"schemas":{"Product":{"version":1,"fields":{"title":{"type":"text"}}}}}`
	if cmstest.Code(e.Do(e.Admin, "register-manifest", payload(withSchema, "staging"), nil)) != "MANIFEST_BREAKING_IN_USE" {
		t.Fatal("schema candidate bypassed application contract removal")
	}
	code := cmstest.Code(e.Do(e.Admin, "register-manifest", payload(base, "staging"), nil))
	if code != "MANIFEST_BREAKING_IN_USE" || count(e, "manifests") != n || active(e, "staging").ManifestHash != before.ManifestHash {
		t.Fatal(code)
	}
	// Same contract can activate in an environment where this document is not published.
	if r := register(e, base, "production"); r.Activation != "active" {
		t.Fatal(r)
	}
}

// CHG-034: content changes invalidate only recorded checks; recheck refreshes them.
func TestSequenceFreshness(t *testing.T) {
	e := setup(t)
	cs, doc := e.Draft(e.Admin, "before")
	register(e, base, "staging")
	register(e, base, "production")
	if detail(e, cs).NeedsAttention {
		t.Fatal("fresh diagnostics")
	}
	if err := e.Apply(e.Admin, cs, 1, doc, map[string]any{"type": "node.rename", "payload": map[string]any{"nodeId": "n_root", "name": "after"}}); err != nil {
		t.Fatal(err)
	}
	if !detail(e, cs).NeedsAttention {
		t.Fatal("seq did not invalidate checks")
	}
	register(e, base, "staging")
	d := detail(e, cs)
	if !d.NeedsAttention || d.ManifestDiagnostics[1].Stale || !d.ManifestDiagnostics[0].Stale {
		t.Fatal(d)
	}
	register(e, base, "production")
	if detail(e, cs).NeedsAttention {
		t.Fatal("still stale")
	}
}

// MF-025: registrations serialized by environment reuse a single candidate.
func TestConcurrentRegistration(t *testing.T) {
	e := setup(t)
	raw, _ := json.Marshal(payload(schemaManifest("1", "text"), "staging"))
	var wg sync.WaitGroup
	results := make(chan commandbus.Response, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := e.Bus.Dispatch(context.Background(), e.Admin, commandbus.Request{Name: "register-manifest", IdempotencyKey: uuid.NewString(), Payload: raw})
			results <- r
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count(e, "changeset_manifest_candidates") != 1 || count(e, "changesets") != 1 {
		t.Fatal("duplicate candidates")
	}
}

// MF-021/023: errors roll back the active pointer, immutable insert and diagnostic refresh together.
func TestDatabaseFailuresAreAtomic(t *testing.T) {
	for _, tc := range []struct {
		table, op, raw, env string
		draft, preview      bool
	}{
		{"manifests", "INSERT", native, "staging", false, false},
		{"environments", "UPDATE", native, "staging", false, false},
		{"changeset_manifest_diagnostics", "INSERT", native, "staging", true, false},
		{"changesets", "INSERT", schemaManifest("1", "text"), "staging", false, false},
		{"changeset_manifest_candidates", "INSERT", schemaManifest("1", "text"), "staging", false, false},
		{"preview_schema_snapshots", "INSERT", schemaManifest("1", "text"), "preview/fail", false, true},
	} {
		t.Run(tc.table, func(t *testing.T) {
			e := setup(t)
			if tc.draft {
				e.Draft(e.Admin, "draft")
			}
			if tc.preview {
				e.Must(e.Admin, "create-environment", map[string]any{"name": tc.env, "kind": "preview"}, nil)
			}
			env, err := e.Q.GetManifestEnvironment(context.Background(), store.GetManifestEnvironmentParams{ProjectID: e.Admin.ProjectID, Name: tc.env})
			if err != nil {
				t.Fatal(err)
			}
			before := count(e, "manifests")
			e.Break(tc.table, tc.op)
			cmstest.ExpectDBError(t, e.Do(e.Admin, "register-manifest", payload(tc.raw, tc.env), nil))
			after, err := e.Q.GetManifestEnvironment(context.Background(), store.GetManifestEnvironmentParams{ProjectID: e.Admin.ProjectID, Name: tc.env})
			if err != nil {
				t.Fatal(err)
			}
			if before != count(e, "manifests") || (env.ActiveManifestID == nil) != (after.ActiveManifestID == nil) || (env.ActiveManifestID != nil && *env.ActiveManifestID != *after.ActiveManifestID) {
				t.Fatal("failed transaction mutated manifest")
			}
		})
	}
}

// MF-022/023: read failures and failed dependency checks cannot partially activate a contract.
func TestReadFailures(t *testing.T) {
	for _, table := range []string{"environments", "manifests", "objects", "projects", "changesets", "changeset_objects"} {
		t.Run(table, func(t *testing.T) {
			e := setup(t)
			e.Draft(e.Admin, "draft")
			before := active(e, "staging")
			e.Break(table, "")
			cmstest.ExpectDBError(t, e.Do(e.Admin, "register-manifest", payload(native, "staging"), nil))
			if table != "environments" && table != "manifests" {
				if active(e, "staging").ManifestHash != before.ManifestHash {
					t.Fatal("partial activation")
				}
			}
		})
	}
	for _, table := range []string{"environments", "manifests"} {
		t.Run("query-"+table, func(t *testing.T) {
			e := setup(t)
			e.Break(table, "")
			if _, err := manifestregistry.GetActive(context.Background(), e.Q, e.Admin.ProjectID, "staging"); err == nil {
				t.Fatal("query swallowed DB failure")
			}
			if _, err := manifestregistry.GetSchemas(context.Background(), e.Q, e.Admin.ProjectID, "staging"); err == nil {
				t.Fatal("schemas swallowed DB failure")
			}
		})
	}
	for _, table := range []string{"schema_versions"} {
		t.Run("schemas-"+table, func(t *testing.T) {
			e := setup(t)
			e.Break(table, "")
			cmstest.ExpectDBError(t, e.Do(e.Admin, "register-manifest", payload(schemaManifest("1", "text"), "staging"), nil))
		})
	}
}

// MF-020/022: proposed required fields are checked against published documents.
func TestPublishedValidation(t *testing.T) {
	e := setup(t)
	register(e, native, "staging")
	cs, _ := createNative(e)
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1, "targets": []string{"staging"}}, nil)
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)
	addedRequired := strings.Replace(native, `"props":{"title":`, `"props":{"extra":{"type":"string","required":true},"title":`, 1)
	if cmstest.Code(e.Do(e.Admin, "register-manifest", payload(addedRequired, "staging"), nil)) != "MANIFEST_BREAKING_IN_USE" {
		t.Fatal("invalid published contract activated")
	}
}

// MF-020/022: removal used through a Composed reference reports its published page consumer.
func TestTransitivePublishedImpact(t *testing.T) {
	e := setup(t)
	register(e, native, "staging")
	var cs changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "transitive"}, &cs)
	var component changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "component", "root": map[string]any{"id": "n_root", "type": "Native", "props": map[string]any{"title": "ok"}}}}}}, &component)
	var page changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 1, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "root": map[string]any{"id": "n_root", "type": "Composed", "ref": map[string]any{"component": component.Operations[0].Target, "version": "live"}}}}}}, &page)
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs.ID, "expectedSeq": 2, "targets": []string{"staging"}}, nil)
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs.ID, "environment": "staging"}, nil)
	err := e.Do(e.Admin, "register-manifest", payload(base, "staging"), nil)
	if cmstest.Code(err) != "MANIFEST_BREAKING_IN_USE" {
		t.Fatal(err)
	}
	report := err.(*commandbus.Error).Params["impact"].([]validation.Impact)
	for _, item := range report {
		if item.Stage == "published" && item.ObjectID == page.Operations[0].Target && len(item.Changes) > 0 {
			return
		}
	}
	t.Fatal("transitive published page missing from impact")
}
