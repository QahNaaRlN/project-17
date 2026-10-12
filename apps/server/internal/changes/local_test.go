package changes_test

import (
	"encoding/json"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"strings"
	"testing"
)

func TestStudioLocalTextPersistence(t *testing.T) {
	// STU-001/003/021, IR-041: public commands persist a locale value and binding atomically.
	e := setup(t)
	cs := e.createCS("Studio text")
	created, err := e.apply(cs.ID, 0, createDoc(`{"id":"n_title","type":"Heading","props":{"level":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	doc := created.Operations[0].Target
	text := nodeOp(doc, "localContent.set", `{"key":"t_title","type":"text","locale":"ru","value":"Текст Studio"}`)
	binding := nodeOp(doc, "node.setBinding", `{"nodeId":"n_title","prop":"text","binding":"$local.t_title"}`)
	writer := e.actor
	writer.Rights = auth.RightSet{auth.DesignCompose: true}
	if _, err := e.dispatch(writer, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 1, "operations": []any{text, binding}}, "Studio"); code(err) != "FORBIDDEN" {
		t.Fatalf("missing content.write: %v", err)
	}
	// A bad second operation rolls back both operations and sequence.
	if _, err := e.apply(cs.ID, 1, text, nodeOp(doc, "node.setBinding", `{"nodeId":"n_missing","prop":"text","binding":"$local.t_title"}`)); err == nil {
		t.Fatal("expected failure")
	}
	if strings.Contains(e.body(doc, cs.ID), "localContent") {
		t.Fatal("failed batch persisted text")
	}
	result, err := e.apply(cs.ID, 1, text, binding)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changeset.Seq != 3 || !strings.Contains(e.body(doc, cs.ID), `"ru":"Текст Studio"`) || !strings.Contains(e.body(doc, cs.ID), `"text":"$local.t_title"`) {
		t.Fatal("draft not persisted")
	}
	if _, err := e.apply(cs.ID, 1, text); code(err) != "CHANGESET_SEQ_CONFLICT" {
		t.Fatal(err)
	}
	// Undo binding then text, preserving immutable history.
	for _, seq := range []int{3, 4} {
		raw, err := e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": seq}, "Studio test")
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if json.Unmarshal(raw, &result) != nil {
			t.Fatal("invalid undo result")
		}
	}
	if strings.Contains(e.body(doc, cs.ID), "localContent") || strings.Contains(e.body(doc, cs.ID), "bindings") {
		t.Fatal("undo did not restore original")
	}
}
