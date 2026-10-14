package validation_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"testing"
)

// STU-166/169: prospective published uniqueness does not consult or mutate head.
func TestPublishedRollbackConstraints(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	var app map[string]any
	if err := json.Unmarshal([]byte(cmstest.DefaultManifest), &app); err != nil {
		t.Fatal(err)
	}
	app["schemas"] = map[string]any{"Article": map[string]any{"version": 1, "singleton": true, "fields": map[string]any{"slug": map[string]any{"type": "string", "unique": true}, "title": map[string]any{"type": "text", "localized": true, "unique": true}}}}
	raw, _ := json.Marshal(app)
	e.ActivateManifest("staging", raw)
	c, err := validation.LoadSnapshot(ctx, e.Q, e.Admin.ProjectID, "staging", nil)
	if err != nil {
		t.Fatal(err)
	}
	docs := map[uuid.UUID]validation.Document{}
	for i := 0; i < 2; i++ {
		id := e.ContentFixture("entity", "Article", map[string]any{"slug": "same", "title": map[string]any{"ru": "Одинаково"}})
		o, err := e.Q.ContentIdentity(ctx, store.ContentIdentityParams{ProjectID: e.Admin.ProjectID, ID: id})
		if err != nil {
			t.Fatal(err)
		}
		v, err := e.Q.GetVersion(ctx, *o.HeadVersionID)
		if err != nil {
			t.Fatal(err)
		}
		docs[id] = validation.Document{ObjectID: id, VersionID: v.ID, Body: v.Body}
	}
	problems, err := c.PublishedConstraints(ctx, docs)
	if err != nil || !validation.HasErrors(problems) {
		t.Fatal(problems, err)
	}
	codes := map[string]bool{}
	for _, ds := range problems {
		for _, d := range ds {
			codes[string(d.Code)] = true
		}
	}
	if !codes["CONTENT_SINGLETON"] || !codes["CONTENT_UNIQUE"] {
		t.Fatal(codes)
	}
	for id, d := range docs {
		d.Deleted = true
		docs[id] = d
		break
	}
	problems, err = c.PublishedConstraints(ctx, docs)
	if err != nil || validation.HasErrors(problems) {
		t.Fatal(problems, err)
	}
	for id, d := range docs {
		if !d.Deleted {
			d.Body = []byte("{")
			docs[id] = d
			break
		}
	}
	if _, err := c.PublishedConstraints(ctx, docs); err == nil {
		t.Fatal("invalid historical body hidden")
	}
	missing := uuid.New()
	docs = map[uuid.UUID]validation.Document{missing: {ObjectID: missing}}
	if _, err := c.PublishedConstraints(ctx, docs); err == nil {
		t.Fatal("missing identity hidden")
	}
}
