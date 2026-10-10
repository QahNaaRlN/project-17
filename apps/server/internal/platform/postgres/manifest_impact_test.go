package postgres_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"testing"
)

// MF-020/MF-023: head, опубликованные и незавершённые версии не подменяют друг друга.
func TestManifestImpactVersions(t *testing.T) {
	e := cmstest.New(t, workflow.Register, publishing.Register)
	ctx := context.Background()
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	cs, doc := e.Draft(e.Admin, "v1")
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, nil)
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)
	open := e.Edit(e.Admin, doc, "v2")
	abandoned := e.Edit(e.Admin, doc, "discard")
	e.Must(e.Admin, "abandon-changeset", map[string]any{"changesetId": abandoned}, nil)
	rows, err := e.Q.ManifestImpactVersions(ctx, store.ManifestImpactVersionsParams{ProjectID: e.Admin.ProjectID, Environment: "staging"})
	if err != nil || len(rows) != 3 {
		t.Fatalf("staging: %+v %v", rows, err)
	}
	stages := map[string]bool{}
	var headID uuid.UUID
	for _, row := range rows {
		stages[row.Stage] = true
		if row.ObjectID != doc {
			t.Fatalf("wrong object: %+v", row)
		}
		if row.Stage == "working" {
			if row.ChangesetID == nil || *row.ChangesetID != open {
				t.Fatalf("wrong CS: %+v", row)
			}
		} else {
			if row.ChangesetID != nil {
				t.Fatal("finished stage has CS")
			}
			headID = row.VersionID
		}
	}
	if len(stages) != 3 || !stages["head"] || !stages["published"] || !stages["working"] {
		t.Fatalf("stages: %+v", stages)
	}
	rows, err = e.Q.ManifestImpactVersions(ctx, store.ManifestImpactVersionsParams{ProjectID: e.Admin.ProjectID, Environment: "production"})
	if err != nil || len(rows) != 2 {
		t.Fatalf("other environment: %+v %v", rows, err)
	}
	for _, row := range rows {
		if row.Stage == "published" {
			t.Fatal("leaked staging published pointer")
		}
		if row.Stage == "working" && row.VersionID == headID {
			t.Fatal("head substituted for draft")
		}
	}
	for _, params := range []store.ManifestImpactVersionsParams{{ProjectID: e.Admin.ProjectID, Environment: "missing"}, {ProjectID: uuid.New(), Environment: "staging"}} {
		rows, err = e.Q.ManifestImpactVersions(ctx, params)
		if err != nil || len(rows) != 0 {
			t.Fatalf("scope: %+v %v", rows, err)
		}
	}
	// MF-020: published remains relevant even when head has been tombstoned.
	e.Exec("UPDATE objects SET deleted_at=now(), head_version_id=NULL WHERE id=$1", doc)
	rows, err = e.Q.ManifestImpactVersions(ctx, store.ManifestImpactVersionsParams{ProjectID: e.Admin.ProjectID, Environment: "staging"})
	if err != nil || len(rows) != 2 {
		t.Fatalf("tombstoned head: %+v %v", rows, err)
	}
}
