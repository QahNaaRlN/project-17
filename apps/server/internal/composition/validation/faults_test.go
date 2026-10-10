package validation_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"testing"
)

// PUB-020: infrastructure failures propagate instead of passing an incomplete check.
func TestValidationDatabaseFaults(t *testing.T) {
	for _, table := range []string{"environments", "manifests", "changeset_manifest_candidates", "published_pointers", "objects", "object_versions"} {
		t.Run(table, func(t *testing.T) {
			e := setup(t)
			cs := newCS(e)
			id := create(e, cs, 0, "page", ref(uuid.New(), "live"))
			d := doc(e, cs, id)
			c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &cs)
			if err != nil {
				t.Fatal(err)
			}
			e.Break(table, "")
			if table == "environments" || table == "manifests" || table == "changeset_manifest_candidates" {
				_, err = validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &cs)
			} else if table == "published_pointers" {
				_, err = c.CheckPublication(context.Background(), []validation.Document{d})
			} else {
				_, err = c.Validate(context.Background(), d)
			}
			cmstest.ExpectDBError(t, err)
		})
	}
	for _, table := range []string{"environments", "objects", "object_versions", "manifests"} {
		t.Run("impact "+table, func(t *testing.T) {
			e := setup(t)
			e.Break(table, "")
			_, err := validation.AnalyzeImpact(context.Background(), e.Q, e.Admin.ProjectID, "staging", nil)
			cmstest.ExpectDBError(t, err)
		})
	}
	e := setup(t)
	for _, args := range []struct {
		project uuid.UUID
		env     string
	}{{uuid.New(), "staging"}, {e.Admin.ProjectID, "missing"}} {
		if _, err := validation.Load(context.Background(), e.Q, args.project, args.env, nil); cmstest.Code(err) != "NOT_FOUND" {
			t.Fatal(err)
		}
	}
	if err := validation.LockProjectEnvironments(context.Background(), store.New(e.Pool), uuid.New()); err != nil {
		t.Fatal(err)
	}
	e.Break("environments", "")
	cmstest.ExpectDBError(t, validation.LockProjectEnvironments(context.Background(), e.Q, e.Admin.ProjectID))
}

// Cross-project component references and candidate bindings cannot cross storage boundaries.
func TestForeignProjectAndCandidateIdentity(t *testing.T) {
	e := setup(t)
	other, err := e.Q.CreateProject(context.Background(), store.CreateProjectParams{ID: uuid.New(), Slug: "other", Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := e.Q.CreateObject(context.Background(), store.CreateObjectParams{ID: uuid.New(), ProjectID: other.ID, DocKind: ptr("component")})
	if err != nil {
		t.Fatal(err)
	}
	cs := newCS(e)
	missing := uuid.New()
	if _, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &missing); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	e.Exec("UPDATE changesets SET kind='schema' WHERE id=$1", cs)
	if _, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &cs); cmstest.Code(err) != "MANIFEST_CANDIDATE_REQUIRED" {
		t.Fatal(err)
	}
	e.Exec("UPDATE changesets SET kind='standard' WHERE id=$1", cs)
	id := create(e, cs, 0, "page", ref(foreign.ID, "live"))
	if r := validate(e, cs, id, "staging"); !has(r, "COMPONENT_NOT_FOUND") {
		t.Fatal(r)
	}
	// A standard CS cannot use a candidate; after switching to schema its exact base is checked.
	env, _ := e.Q.GetEnvironmentByName(context.Background(), store.GetEnvironmentByNameParams{ProjectID: e.Admin.ProjectID, Name: "staging"})
	candidate := e.ActivateManifest("unused", []byte(contract))
	e.Exec("INSERT INTO changeset_manifest_candidates(changeset_id,project_id,environment_id,manifest_id,base_manifest_id) VALUES($1,$2,$3,$4,$5)", cs, e.Admin.ProjectID, env.ID, candidate, env.ActiveManifestID)
	if _, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &cs); cmstest.Code(err) != "MANIFEST_CANDIDATE_MISMATCH" {
		t.Fatal(err)
	}
	e.Exec("UPDATE changesets SET kind='schema' WHERE id=$1", cs)
	if _, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &cs); err != nil {
		t.Fatal(err)
	}
	e.ActivateManifest("staging", []byte(contract))
	if _, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &cs); cmstest.Code(err) != "MANIFEST_CANDIDATE_MISMATCH" {
		t.Fatal(err)
	}
	// Database FK rejects a binding to a foreign project's environment.
	if _, err := e.Pool.Exec(context.Background(), "INSERT INTO changeset_manifest_candidates(changeset_id,project_id,environment_id,manifest_id) VALUES($1,$2,$3,$4)", uuid.New(), other.ID, env.ID, candidate); err == nil {
		t.Fatal("foreign binding")
	}
}
func ptr(s string) *string { return &s }
