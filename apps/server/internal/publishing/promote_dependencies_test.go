package publishing

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// STU-468: a missing reference points to the source publication that published it.
func TestPromoteDependencies(t *testing.T) {
	entity, orphan, pub := uuid.New(), uuid.New(), uuid.New()
	problems := map[string][]ir.Diagnostic{
		"a": {
			{Code: "CONTENT_REFERENCE_UNPUBLISHED", Params: map[string]any{"target": entity.String()}},
			{Code: "CONTENT_REFERENCE_UNPUBLISHED", Params: map[string]any{"target": entity.String()}},
			{Code: "CONTENT_REFERENCE_UNPUBLISHED", Params: map[string]any{"target": "not-a-uuid"}},
			{Code: "CONTENT_REQUIRED"},
		},
		"b": {{Code: "CONTENT_REFERENCE_UNPUBLISHED", Params: map[string]any{"target": orphan}}},
	}
	loads := 0
	deps, err := promoteDependencies(problems, func() ([]store.PublishedPointer, error) {
		loads++
		return []store.PublishedPointer{{ObjectID: entity, PublicationID: pub}}, nil
	})
	if err != nil || loads != 1 || len(deps) != 2 {
		t.Fatal(deps, loads, err)
	}
	for _, d := range deps {
		if d.ObjectID == entity && (d.PublicationID == nil || *d.PublicationID != pub) || d.ObjectID == orphan && d.PublicationID != nil {
			t.Fatal(deps)
		}
	}
	if deps, err := promoteDependencies(map[string][]ir.Diagnostic{}, nil); err != nil || len(deps) != 0 {
		t.Fatal(deps, err)
	}
	failure := errors.New("storage")
	if _, err := promoteDependencies(problems, func() ([]store.PublishedPointer, error) { return nil, failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}
