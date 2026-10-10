package validation_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"testing"
)

// CNT-011/040: typed nested/localized/list/rich text references resolve exact prospective versions.
func TestContentReferenceContracts(t *testing.T) {
	e := setup(t)
	id := e.ContentFixture("entity", "Article", map[string]any{}, "staging")
	image := e.ContentFixture("asset", "", map[string]any{"assetKind": "image", "mimeType": "image/png"}, "staging")
	c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", nil)
	if err != nil {
		t.Fatal(err)
	}
	ref := map[string]any{"type": "reference", "schema": "Article"}
	value := map[string]any{"entityId": id.String()}
	cases := []struct {
		field map[string]any
		value any
		valid bool
	}{
		{ref, value, true},
		{map[string]any{"type": "reference", "schema": "Other"}, value, false},
		{map[string]any{"type": "reference"}, map[string]any{"entityId": image.String()}, false},
		{map[string]any{"type": "reference"}, map[string]any{"entityId": uuid.NewString()}, false},
		{map[string]any{"type": "list", "of": ref}, []any{value}, true},
		{map[string]any{"type": "object", "fields": map[string]any{"child": ref}}, map[string]any{"child": value}, true},
		{map[string]any{"type": "reference", "schema": "Article", "localized": true}, map[string]any{"ru": value}, true},
		{map[string]any{"type": "asset", "assetKind": "image"}, map[string]any{"assetId": image.String()}, true},
		{map[string]any{"type": "asset", "assetKind": "video"}, map[string]any{"assetId": image.String()}, false},
		{map[string]any{"type": "asset", "mimeTypes": []any{"image/png"}}, map[string]any{"assetId": image.String()}, true},
		{map[string]any{"type": "asset", "mimeTypes": []any{"video/mp4"}}, map[string]any{"assetId": image.String()}, false},
		{map[string]any{"type": "richText"}, map[string]any{"content": []any{map[string]any{"attrs": map[string]any{"assetId": image.String()}}}}, true},
	}
	for _, v := range cases {
		r, err := c.TypedReferences(context.Background(), v.field, v.value, "/field")
		if err != nil || r.Valid != v.valid {
			t.Fatal(v, r, err)
		}
	}
	for _, body := range []any{value, map[string]any{"schema": "Other", "entityId": id.String()}, map[string]any{"assetId": id.String()}, map[string]any{"assetId": image.String()}} {
		r, err := c.References(context.Background(), body)
		if err != nil {
			t.Fatal(r, err)
		}
	}
	o, err := e.Q.ContentIdentity(context.Background(), store.ContentIdentityParams{ProjectID: e.Admin.ProjectID, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.Q.GetVersion(context.Background(), *o.HeadVersionID)
	if err != nil {
		t.Fatal(err)
	}
	c.Overrides[id] = validation.Document{ObjectID: id, VersionID: v.ID, Body: v.Body}
	if r, err := c.References(context.Background(), value); err != nil || !r.Valid {
		t.Fatal(r, err)
	}
	c.Overrides[id] = validation.Document{ObjectID: id, VersionID: uuid.New(), Body: v.Body}
	if r, err := c.References(context.Background(), value); err != nil || r.Valid {
		t.Fatal(r, err)
	}
	c.Overrides[uuid.Nil] = validation.Document{VersionID: v.ID, Body: v.Body}
	if r, err := c.References(context.Background(), map[string]any{"entityId": uuid.Nil.String()}); err != nil || r.Valid {
		t.Fatal(r, err)
	}
}

// L2 database failures must surface instead of being confused with a missing reference.
func TestContentReferenceDBErrors(t *testing.T) {
	for _, table := range []string{"objects", "object_versions", "published_pointers"} {
		t.Run(table, func(t *testing.T) {
			e := setup(t)
			id := e.ContentFixture("entity", "Article", map[string]any{}, "staging")
			c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", nil)
			if err != nil {
				t.Fatal(err)
			}
			e.Break(table, "")
			value := map[string]any{"entityId": id.String()}
			_, err = c.TypedReferences(context.Background(), map[string]any{"type": "reference"}, value, "/ref")
			cmstest.ExpectDBError(t, err)
			_, err = c.References(context.Background(), value)
			cmstest.ExpectDBError(t, err)
			for _, nested := range []any{map[string]any{"nested": value}, []any{value}} {
				_, err := c.References(context.Background(), nested)
				cmstest.ExpectDBError(t, err)
			}

			if _, err := c.TypedReferences(context.Background(), map[string]any{"type": "object", "fields": map[string]any{"ref": map[string]any{"type": "reference"}}}, map[string]any{"ref": value}, ""); err == nil {
				t.Fatal("nested error hidden")
			}
			if _, err := c.TypedReferences(context.Background(), map[string]any{"type": "list", "of": map[string]any{"type": "reference"}}, []any{value}, ""); err == nil {
				t.Fatal("list error hidden")
			}
			if _, err := c.TypedReferences(context.Background(), map[string]any{"type": "reference", "localized": true}, map[string]any{"ru": value}, ""); err == nil {
				t.Fatal("localized error hidden")
			}
		})
	}
}

// Unexpected prospective IDs must fail rather than silently bypass project/head constraints.
func TestContentProspectiveVersionErrors(t *testing.T) {
	e := setup(t)
	id := e.ContentFixture("entity", "Article", map[string]any{}, "staging")
	c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []validation.Document{{ObjectID: uuid.New()}, {ObjectID: id, VersionID: uuid.New()}} {
		if _, err := c.HeadConstraints(context.Background(), []validation.Document{d}); err == nil {
			t.Fatal("unknown prospective version accepted")
		}
	}
	e.Break("object_versions", "")
	if _, err := c.HeadConstraints(context.Background(), nil); err == nil {
		t.Fatal("head storage error hidden")
	}
}
