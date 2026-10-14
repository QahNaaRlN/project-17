package reviewdiff_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/agents"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/reviewdiff"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"testing"
	"time"
)

func TestMain(m *testing.M) { pgtest.Main(m) }
func TestSnapshotsAllTargetsRightsAndApproval(t *testing.T) {
	e := cmstest.New(t, workflow.Register, publishing.Register)
	ctx := context.Background()
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	author := e.Human("Alice", auth.AllRights...)
	reviewer := e.Human("Bob", auth.DesignRead, auth.ContentRead, auth.ContentPublish)
	initial, doc := e.Draft(author, "v1")
	var pub publishing.Publication
	e.Must(author, "submit-changeset", map[string]any{"changesetId": initial, "expectedSeq": 1, "targets": []string{"staging"}}, nil)
	e.Must(author, "publish", map[string]any{"changesetId": initial, "environment": "staging", "expectedSeq": 1}, &pub)
	e.Must(e.Admin, "promote", map[string]any{"publicationId": pub.ID, "toEnvironment": "production"}, nil)
	second := e.Edit(author, doc, "v2")
	e.Must(author, "submit-changeset", map[string]any{"changesetId": second, "expectedSeq": 1, "targets": []string{"staging"}}, nil)
	e.Must(author, "publish", map[string]any{"changesetId": second, "environment": "staging", "expectedSeq": 1}, nil)
	var cs changes.Changeset
	e.Must(author, "create-changeset", map[string]any{"title": "Both targets", "targets": []string{"staging", "production"}}, &cs)
	if err := e.Apply(author, cs.ID, 0, doc, cmstest.Op("node.rename", map[string]any{"nodeId": "n_root", "name": "v3"})); err != nil {
		t.Fatal(err)
	}
	first, err := reviewdiff.ReadIndex(ctx, e.Q, author, cs.ID, "staging")
	if err != nil {
		t.Fatal(err)
	}
	prod, err := reviewdiff.ReadIndex(ctx, e.Q, author, cs.ID, "production")
	if err != nil || prod.Snapshot != first.Snapshot || prod.Objects[0].PublishedVersionID == nil || *prod.Objects[0].PublishedVersionID == *first.Objects[0].PublishedVersionID {
		t.Fatal(prod, err)
	}
	for _, env := range []string{"staging", "production"} {
		for _, base := range []string{"base", "published"} {
			p, err := reviewdiff.ReadPage(ctx, e.Q, author, cs.ID, doc, env, base, first.Snapshot, "", 100)
			if err != nil || p.TotalChanges == 0 {
				t.Fatal(p, err)
			}
			raw, _ := json.Marshal(p)
			if env == "production" && base == "published" && !contains(string(raw), "v1") {
				t.Fatal(string(raw))
			}
		}
	}
	weak := author
	weak.Rights = auth.RightSet{}
	if _, err := reviewdiff.ReadIndex(ctx, e.Q, weak, cs.ID, "staging"); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	if _, err := reviewdiff.ReadIndex(ctx, e.Q, reviewer, cs.ID, "staging"); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 1, "medium": 1, "high": 1}, nil)
	if err := reviewdiff.CheckExpected(ctx, e.Q, author, cs.ID, &first.Snapshot); cmstest.Code(err) != "DIFF_SNAPSHOT_STALE" {
		t.Fatal(err)
	}
	e.Must(author, "submit-changeset", map[string]any{"changesetId": cs.ID, "expectedSeq": 1}, nil)
	fresh, err := reviewdiff.ReadIndex(ctx, e.Q, reviewer, cs.ID, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Do(reviewer, "approve-changeset", map[string]any{"changesetId": cs.ID, "expectedSeq": 1, "expectedDiffSnapshot": first.Snapshot}, nil); cmstest.Code(err) != "DIFF_SNAPSHOT_STALE" {
		t.Fatal(err)
	}
	e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": cs.ID, "expectedSeq": 1, "expectedDiffSnapshot": fresh.Snapshot}, nil)
	if err := reviewdiff.CheckExpected(ctx, e.Q, reviewer, cs.ID, &fresh.Snapshot); err != nil {
		t.Fatal("approval count must not change snapshot", err)
	}
	// An environment pointer can shift without changing seq, body or global head.
	e.Exec(`UPDATE published_pointers SET version_id=$1 WHERE object_id=$2 AND environment_id=(SELECT id FROM environments WHERE project_id=$3 AND name='production')`, first.Objects[0].PublishedVersionID, doc, author.ProjectID)
	if err := e.Do(author, "publish", map[string]any{"changesetId": cs.ID, "expectedSeq": 1, "environment": "staging", "expectedDiffSnapshot": fresh.Snapshot}, nil); cmstest.Code(err) != "DIFF_SNAPSHOT_STALE" {
		t.Fatal(err)
	}
	if err := reviewdiff.CheckExpected(ctx, e.Q, author, cs.ID, nil); err != nil {
		t.Fatal(err)
	}
	bad := "bad"
	if err := reviewdiff.CheckExpected(ctx, e.Q, author, cs.ID, &bad); cmstest.Code(err) != "PAYLOAD_INVALID" {
		t.Fatal(err)
	}
}
func contains(a, b string) bool {
	for i := 0; i+len(b) <= len(a); i++ {
		if a[i:i+len(b)] == b {
			return true
		}
	}
	return false
}
func TestDiffAccessFaultsAndWriterSerialization(t *testing.T) {
	ctx := context.Background()
	e := cmstest.New(t, workflow.Register, publishing.Register)
	cs, doc := e.Draft(e.Admin, "test")
	e.Exec(`UPDATE changesets SET targets=ARRAY['staging'] WHERE id=$1`, cs)
	for _, row := range []struct {
		id   uuid.UUID
		env  string
		code string
	}{{uuid.New(), "staging", "NOT_FOUND"}, {cs, "other", "DIFF_ENVIRONMENT_FORBIDDEN"}} {
		_, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, row.id, row.env)
		if cmstest.Code(err) != row.code {
			t.Fatal(err)
		}
	}
	for _, state := range []string{"merged", "abandoned", "checking"} {
		e.Exec(`UPDATE changesets SET state=$1 WHERE id=$2`, state, cs)
		_, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging")
		if cmstest.Code(err) != "DIFF_STATE_UNSUPPORTED" {
			t.Fatal(state, err)
		}
	}
	e.Exec(`UPDATE changesets SET state='open' WHERE id=$1`, cs)
	snapshot, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		baseline, snap string
		limit          int
		object         uuid.UUID
		code           string
	}{{"wrong", snapshot.Snapshot, 10, doc, "PAYLOAD_INVALID"}, {"base", "bad", 10, doc, "PAYLOAD_INVALID"}, {"base", snapshot.Snapshot, 101, doc, "PAYLOAD_INVALID"}, {"base", snapshot.Snapshot, 10, uuid.New(), "NOT_FOUND"}} {
		_, err := reviewdiff.ReadPage(ctx, e.Q, e.Admin, cs, row.object, "staging", row.baseline, row.snap, "", row.limit)
		if cmstest.Code(err) != row.code {
			t.Fatal(err)
		}
	}
	tx, err := e.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := validation.LockProjectEnvironments(ctx, store.New(tx), e.Admin.ProjectID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- e.Do(e.Admin, "set-approval-policy", map[string]int{"low": 2, "medium": 2, "high": 2}, nil)
	}()
	select {
	case err := <-done:
		t.Fatal("policy writer bypassed review lock", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := reviewdiff.CheckExpected(ctx, e.Q, e.Admin, cs, &snapshot.Snapshot); cmstest.Code(err) != "DIFF_SNAPSHOT_STALE" {
		t.Fatal(err)
	}
}

// STU-149/151/155: persisted mixed work, full rights gate and bounded complete responses.
func TestMixedObjectsFaultsAndLimits(t *testing.T) {
	e := cmstest.New(t, workflow.Register, publishing.Register)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "page one")
	e.Exec(`UPDATE changesets SET targets=ARRAY['staging'] WHERE id=$1`, cs)
	other, doc2 := e.Draft(e.Admin, "page two")
	e.Exec(`UPDATE changeset_objects SET changeset_id=$1 WHERE changeset_id=$2`, cs, other)
	e.Exec(`UPDATE object_versions SET changeset_id=$1 WHERE changeset_id=$2`, cs, other)
	entity := e.ContentFixture("entity", "Article", map[string]any{"title": map[string]any{"ru": "Before"}, "number": json.Number("9007199254740993")}, "staging")
	asset := e.ContentFixture("asset", "", map[string]any{"title": "Picture", "fileHash": "a", "storage_key": map[string]any{"key": "never-disclose"}}, "staging")
	for _, id := range []uuid.UUID{entity, asset} {
		version := uuid.New()
		e.Exec(`INSERT INTO object_versions(id,project_id,object_id,number,state,changeset_id,schema_version,body,body_hash,created_by) SELECT $1,project_id,object_id,NULL,'working',$2,schema_version,body,body_hash,created_by FROM object_versions WHERE id=(SELECT head_version_id FROM objects WHERE id=$3)`, version, cs, id)
		e.Exec(`INSERT INTO changeset_objects(changeset_id,object_id,base_version_id,working_version_id) SELECT $1,id,head_version_id,$2 FROM objects WHERE id=$3`, cs, version, id)
		e.Exec(`UPDATE object_versions SET body=body || '{"title":"After"}'::jsonb WHERE id=$1`, version)
	}
	idx, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging")
	if err != nil || len(idx.Objects) != 4 {
		t.Fatal(idx, err)
	}
	for _, id := range []uuid.UUID{doc, doc2, entity, asset} {
		p, err := reviewdiff.ReadPage(ctx, e.Q, e.Admin, cs, id, "staging", "base", idx.Snapshot, "", 1)
		if err != nil || len(p.Items) != 1 {
			t.Fatal(p, err)
		}
		raw, _ := json.Marshal(p)
		if contains(string(raw), "never-disclose") {
			t.Fatal("secret exposed")
		}
		if p.NextCursor != nil {
			if _, err := reviewdiff.ReadPage(ctx, e.Q, e.Admin, cs, id, "staging", "published", idx.Snapshot, *p.NextCursor, 1); cmstest.Code(err) != "DIFF_CURSOR_INVALID" {
				t.Fatal(err)
			}
			if _, err := reviewdiff.ReadPage(ctx, e.Q, e.Admin, cs, id, "staging", "base", idx.Snapshot, *p.NextCursor, 100); err != nil {
				t.Fatal(err)
			}
		}
	}
	weak := e.Admin
	weak.Rights = auth.RightSet{auth.DesignRead: true}
	if _, err := reviewdiff.ReadIndex(ctx, e.Q, weak, cs, "staging"); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	e.Exec(`UPDATE changesets SET kind='schema' WHERE id=$1`, cs)
	if _, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging"); cmstest.Code(err) != "DIFF_KIND_UNSUPPORTED" {
		t.Fatal(err)
	}
	e.Exec(`UPDATE changesets SET kind='standard' WHERE id=$1`, cs)
	e.Exec(`UPDATE object_versions SET body='{"nodes":{},"root":"missing"}' WHERE id=(SELECT working_version_id FROM changeset_objects WHERE changeset_id=$1 AND object_id=$2)`, cs, doc)
	if _, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging"); cmstest.Code(err) != "DIFF_PROJECTION_UNSUPPORTED" {
		t.Fatal(err)
	}
	e.Exec(`UPDATE object_versions SET body='{"root":"n","nodes":{"n":{"id":"n","type":"Box"}}}' WHERE id=(SELECT working_version_id FROM changeset_objects WHERE changeset_id=$1 AND object_id=$2)`, cs, doc)
	e.Exec(`UPDATE object_versions SET body=jsonb_build_object('title',repeat('x',$1)) WHERE id=(SELECT working_version_id FROM changeset_objects WHERE changeset_id=$2 AND object_id=$3)`, reviewdiff.MaxBytes, cs, entity)
	idx, err = reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging")
	if cmstest.Code(err) != "DIFF_LIMIT_EXCEEDED" {
		t.Fatal(err)
	} // Huge title exceeds index budget; never silently truncate.
}

func TestRepeatableReadMutableBodyAndObjectLimit(t *testing.T) {
	e := cmstest.New(t)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "snapshot")
	e.Exec(`UPDATE changesets SET targets=ARRAY['staging'] WHERE id=$1`, cs)
	tx, err := e.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	first, err := reviewdiff.ReadIndex(ctx, store.New(tx), e.Admin, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{nodes,n_root,name}','"concurrent"') WHERE id=(SELECT working_version_id FROM changeset_objects WHERE changeset_id=$1 AND object_id=$2)`, cs, doc)
	repeat, err := reviewdiff.ReadIndex(ctx, store.New(tx), e.Admin, cs, "staging")
	if err != nil || repeat.Snapshot != first.Snapshot {
		t.Fatal("mixed read", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	fresh, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging")
	if err != nil || fresh.Snapshot == first.Snapshot || fresh.Seq != first.Seq {
		t.Fatal("mutable body not fingerprinted", err)
	}
	agent := e.Admin
	agent.Kind = auth.ActorAgent
	if _, err := reviewdiff.ReadIndex(ctx, e.Q, agent, cs, "staging"); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	agent.AllowedChangesets = []uuid.UUID{cs}
	if _, err := reviewdiff.ReadIndex(ctx, e.Q, agent, cs, "staging"); err != nil {
		t.Fatal(err)
	}
	other := e.Admin
	other.ProjectID = uuid.New()
	if _, err := reviewdiff.ReadIndex(ctx, e.Q, other, cs, "staging"); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	e.Exec(`WITH g AS (SELECT gen_random_uuid() oid,gen_random_uuid() vid FROM generate_series(1,999)), o AS (INSERT INTO objects(id,project_id,kind) SELECT oid,$1,'asset' FROM g RETURNING id), v AS (INSERT INTO object_versions(id,project_id,object_id,state,changeset_id,body,body_hash,created_by) SELECT g.vid,$1,g.oid,'working',$2,'{}',decode('00','hex'),$3 FROM g JOIN o ON o.id=g.oid RETURNING id,object_id) INSERT INTO changeset_objects(changeset_id,object_id,working_version_id) SELECT $2,object_id,id FROM v`, e.Admin.ProjectID, cs, e.Admin.ID)
	idx, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging")
	if err != nil || len(idx.Objects) != 1000 || !idx.Complete {
		t.Fatal(len(idx.Objects), err)
	}
	e.Exec(`WITH o AS (INSERT INTO objects(id,project_id,kind) VALUES(gen_random_uuid(),$1,'asset') RETURNING id), v AS (INSERT INTO object_versions(id,project_id,object_id,state,changeset_id,body,body_hash,created_by) SELECT gen_random_uuid(),$1,id,'working',$2,'{}',decode('00','hex'),$3 FROM o RETURNING id,object_id) INSERT INTO changeset_objects(changeset_id,object_id,working_version_id) SELECT $2,object_id,id FROM v`, e.Admin.ProjectID, cs, e.Admin.ID)
	if _, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging"); cmstest.Code(err) != "DIFF_LIMIT_EXCEEDED" {
		t.Fatal(err)
	}
}

func TestIndexMarksWorkingTombstone(t *testing.T) {
	e := cmstest.New(t)
	ctx := context.Background()
	cs, _ := e.Draft(e.Admin, "deleted")
	e.Exec(`UPDATE changesets SET targets=ARRAY['staging'] WHERE id=$1`, cs)
	e.Exec(`UPDATE object_versions SET deleted=true WHERE changeset_id=$1`, cs)
	idx, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging")
	if err != nil || !idx.Objects[0].WorkingDeleted || idx.Objects[0].Summaries["base"].Effect != "metadata-only" {
		t.Fatal(idx, err)
	}
}

func TestStorageFailuresAndUnsupportedImages(t *testing.T) {
	for _, table := range []string{"changesets", "changeset_objects", "objects", "environments", "projects", "published_pointers", "object_versions"} {
		t.Run(table, func(t *testing.T) {
			e := cmstest.New(t)
			cs, _ := e.Draft(e.Admin, "fault")
			e.Exec(`UPDATE changesets SET targets=ARRAY['staging'] WHERE id=$1`, cs)
			e.Exec(`ALTER TABLE ` + table + ` RENAME TO ` + table + `_broken`)
			_, err := reviewdiff.ReadIndex(context.Background(), e.Q, e.Admin, cs, "staging")
			if err == nil || cmstest.Code(err) != "" {
				t.Fatal("expected storage failure", err)
			}
		})
	}
	for _, body := range []string{`null`, `[]`, `{"nodes":{"n_root":"bad"},"root":"n_root"}`} {
		t.Run(body, func(t *testing.T) {
			e := cmstest.New(t)
			cs, _ := e.Draft(e.Admin, "bad")
			e.Exec(`UPDATE changesets SET targets=ARRAY['staging'] WHERE id=$1`, cs)
			e.Exec(`UPDATE object_versions SET body=$1 WHERE changeset_id=$2`, body, cs)
			_, err := reviewdiff.ReadIndex(context.Background(), e.Q, e.Admin, cs, "staging")
			if cmstest.Code(err) != "DIFF_PROJECTION_UNSUPPORTED" {
				t.Fatal(err)
			}
		})
	}
	for _, fault := range []string{"wrongObject", "committedWorking", "workingBase", "settings", "missingEnvironment", "unknownKind"} {
		t.Run(fault, func(t *testing.T) {
			e := cmstest.New(t)
			cs, doc := e.Draft(e.Admin, "fault")
			e.Exec(`UPDATE changesets SET targets=ARRAY['staging'] WHERE id=$1`, cs)
			env := "staging"
			code := "DIFF_BASE_UNAVAILABLE"
			switch fault {
			case "wrongObject":
				other, _ := e.Draft(e.Admin, "other")
				e.Exec(`UPDATE changeset_objects SET working_version_id=(SELECT working_version_id FROM changeset_objects WHERE changeset_id=$1) WHERE changeset_id=$2`, other, cs)
			case "committedWorking":
				e.Exec(`UPDATE object_versions SET state='committed',number=1 WHERE changeset_id=$1`, cs)
			case "workingBase":
				e.Exec(`UPDATE changeset_objects SET base_version_id=working_version_id WHERE changeset_id=$1 AND object_id=$2`, cs, doc)
			case "settings":
				e.Exec(`UPDATE projects SET settings='[]' WHERE id=$1`, e.Admin.ProjectID)
				code = "DIFF_PROJECTION_UNSUPPORTED"
			case "unknownKind":
				e.Exec(`ALTER TABLE objects DROP CONSTRAINT objects_kind_check`)
				e.Exec(`UPDATE objects SET kind='unsupported',doc_kind=NULL WHERE id=$1`, doc)
				code = "DIFF_PROJECTION_UNSUPPORTED"
			case "missingEnvironment":
				e.Exec(`UPDATE changesets SET targets=ARRAY['missing'] WHERE id=$1`, cs)
				env = "missing"
				code = "DIFF_ENVIRONMENT_FORBIDDEN"
			}
			_, err := reviewdiff.ReadIndex(context.Background(), e.Q, e.Admin, cs, env)
			if cmstest.Code(err) != code {
				t.Fatal(err)
			}
		})
	}
}
func TestDetailSingleValueAndBytePaginationLimits(t *testing.T) {
	e := cmstest.New(t)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "large")
	e.Exec(`UPDATE changesets SET targets=ARRAY['staging'] WHERE id=$1`, cs)
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{!oversize}',to_jsonb(repeat('x',$1))) WHERE changeset_id=$2`, reviewdiff.MaxBytes, cs)
	idx, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reviewdiff.ReadPage(ctx, e.Q, e.Admin, cs, doc, "staging", "base", idx.Snapshot, "", 100); cmstest.Code(err) != "DIFF_LIMIT_EXCEEDED" {
		t.Fatal(err)
	}
	base := uuid.New()
	e.Exec(`INSERT INTO object_versions(id,project_id,object_id,number,state,body,body_hash,created_by,path,ir_version) SELECT $1,project_id,object_id,1,'committed',jsonb_set(body #- '{!oversize}','{fields}','{"a":"old","b":"old"}'),body_hash,created_by,path,ir_version FROM object_versions WHERE changeset_id=$2`, base, cs)
	e.Exec(`UPDATE objects SET head_version_id=$1 WHERE id=$2`, base, doc)
	e.Exec(`UPDATE changeset_objects SET base_version_id=$1 WHERE changeset_id=$2`, base, cs)
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body #- '{!oversize}','{fields}',jsonb_build_object('a',repeat('x',$1),'b',repeat('y',$1))) WHERE changeset_id=$2`, reviewdiff.MaxBytes/2+1000, cs)
	idx, err = reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	page, err := reviewdiff.ReadPage(ctx, e.Q, e.Admin, cs, doc, "staging", "base", idx.Snapshot, "", 100)
	if err != nil || page.NextCursor == nil || len(page.Items) == 0 {
		t.Fatal(page, err)
	}
	next, err := reviewdiff.ReadPage(ctx, e.Q, e.Admin, cs, doc, "staging", "base", idx.Snapshot, *page.NextCursor, 100)
	if err != nil || len(next.Items) == 0 {
		t.Fatal(next, err)
	}
	if _, err := reviewdiff.ReadPage(ctx, e.Q, e.Admin, cs, doc, "staging", "base", idx.Snapshot, "bad", 100); cmstest.Code(err) != "DIFF_CURSOR_INVALID" {
		t.Fatal(err)
	}

	env, err := e.Q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: e.Admin.ProjectID, Name: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	cursorContext := e.Admin.ProjectID.String() + ":" + cs.String() + ":" + doc.String() + ":staging:base:" + idx.Snapshot
	token := base64.RawURLEncoding.EncodeToString([]byte(cursorContext + ":9999"))
	mac := hmac.New(sha256.New, env.PreviewKey)
	mac.Write([]byte(token))
	token += "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if _, err := reviewdiff.ReadPage(ctx, e.Q, e.Admin, cs, doc, "staging", "base", idx.Snapshot, token, 100); cmstest.Code(err) != "DIFF_CURSOR_INVALID" {
		t.Fatal(err)
	}
}

func TestDeletedCounterpartTitlesAndProtectedWriterFailures(t *testing.T) {
	e := cmstest.New(t, workflow.Register, publishing.Register)
	ctx := context.Background()
	cs, _ := e.Draft(e.Admin, "title")
	e.Exec(`UPDATE changesets SET targets=ARRAY['staging'] WHERE id=$1`, cs)
	asset := e.ContentFixture("asset", "", map[string]any{"title": map[string]any{"fr": "Picture title"}}, "staging")
	version := uuid.New()
	e.Exec(`INSERT INTO object_versions(id,project_id,object_id,state,changeset_id,body,body_hash,created_by,deleted) SELECT $1,project_id,object_id,'working',$2,'{"title":"deleted title"}',body_hash,created_by,true FROM object_versions WHERE id=(SELECT head_version_id FROM objects WHERE id=$3)`, version, cs, asset)
	e.Exec(`INSERT INTO changeset_objects(changeset_id,object_id,working_version_id,base_version_id) SELECT $1,id,$2,head_version_id FROM objects WHERE id=$3`, cs, version, asset)
	for _, publishedOnly := range []bool{false, true} {
		if publishedOnly {
			e.Exec(`UPDATE changeset_objects SET base_version_id=NULL WHERE changeset_id=$1 AND object_id=$2`, cs, asset)
		}
		idx, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging")
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range idx.Objects {
			if o.ID == asset && (!o.WorkingDeleted || o.Title != "Picture title") {
				t.Fatal(o)
			}
		}
	}
	idx, err := reviewdiff.ReadIndex(ctx, e.Q, e.Admin, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	e.Exec(`UPDATE changesets SET state='merged' WHERE id=$1`, cs)
	if err := reviewdiff.CheckExpected(ctx, e.Q, e.Admin, cs, &idx.Snapshot); cmstest.Code(err) != "DIFF_STATE_UNSUPPORTED" {
		t.Fatal(err)
	}
	(&agents.Service{Pool: e.Pool}).Register(e.Bus)
	e.Exec(`ALTER TABLE environments RENAME TO environments_broken`)
	for _, row := range []struct {
		name    string
		payload any
	}{{"set-approval-policy", map[string]int{"low": 1, "medium": 1, "high": 1}}, {"set-agent-policy", auth.AgentPolicy{Allow: []string{"design.read"}}}, {"promote", map[string]any{"publicationId": uuid.New(), "toEnvironment": "production"}}} {
		cmstest.ExpectDBError(t, e.Do(e.Admin, row.name, row.payload, nil))
	}
}
