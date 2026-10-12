package changes_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
)

func TestStudioStructureHistory(t *testing.T) {
	// STU-060/064: palette text + insertion is one atomic action; all structural operations round-trip.
	e := setup(t)
	cs := e.createCS("structure")
	created, err := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Stack","children":[{"id":"n_tail","type":"Text"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	doc := created.Operations[0].Target
	seq := 1
	actions := [][]map[string]any{
		{nodeOp(doc, "localContent.set", `{"key":"t_new","type":"text","value":{"ru":"Новый","en":"New"}}`), nodeOp(doc, "node.insert", `{"parentId":"n_root","index":0,"subtree":{"id":"n_new","type":"Heading","bindings":{"text":"$local.t_new"}}}`)},
		{nodeOp(doc, "node.move", `{"nodeId":"n_new","parentId":"n_root","index":1}`)},
		{nodeOp(doc, "node.insert", `{"parentId":"n_root","index":2,"subtree":{"id":"n_copy","type":"Heading","bindings":{"text":"$local.t_new"}}}`)},
		{nodeOp(doc, "node.remove", `{"nodeId":"n_copy"}`)},
	}
	for _, ops := range actions {
		before := e.body(doc, cs.ID)
		edit := e.group(cs.ID, seq, ops...)
		seq = int(edit.Changeset.Seq)
		after := e.body(doc, cs.ID)
		undo := e.historyCommand(cs.ID, seq, "undo")
		seq = int(undo.Changeset.Seq)
		if e.body(doc, cs.ID) != before || len(undo.Operations) != len(ops) {
			t.Fatal("partial structure undo")
		}
		redo := e.historyCommand(cs.ID, seq, "redo")
		seq = int(redo.Changeset.Seq)
		if e.body(doc, cs.ID) != after || len(redo.Operations) != len(ops) {
			t.Fatal("structure redo differs")
		}
	}
	h, err := changes.GetHistory(context.Background(), e.q, e.actor, cs.ID, math.MaxInt32)
	if err != nil || !h.CanUndo || h.CanRedo {
		t.Fatal(h, err)
	}
	// A failing insert after localContent must not leave an orphan content value or audit row.
	before := e.body(doc, cs.ID)
	_, err = e.dispatch(e.actor, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": seq, "group": true, "operations": []any{nodeOp(doc, "localContent.set", `{"key":"t_fail","type":"text","value":{"ru":"No"}}`), nodeOp(doc, "node.insert", `{"parentId":"n_root","subtree":{"id":"n_tail","type":"Text"}}`)}}, "failure")
	var detail *commandbus.Error
	if !errors.As(err, &detail) || detail.Code != "OPERATION_INVALID" || detail.Params["operationCode"] != "NODE_ID_DUPLICATE" || e.body(doc, cs.ID) != before {
		t.Fatal("non atomic insertion", err)
	}
	h, err = changes.GetHistory(context.Background(), e.q, e.actor, cs.ID, math.MaxInt32)
	if err != nil || int(h.Seq) != seq {
		t.Fatal("seq changed", h, err)
	}
}

func TestStudioStructurePolicyAndRights(t *testing.T) {
	// STU-061/064: server authority rejects policy violation/lock and stale seq without mutation.
	e := setup(t)
	cs := e.createCS("policy")
	created, err := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Stack","zone":{"id":"root","mode":"SYSTEM","allowedTypes":["Stack","Text"]},"children":[{"id":"n_text","type":"Text","locked":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	doc := created.Operations[0].Target
	before := e.body(doc, cs.ID)
	actor := e.actor
	actor.Rights = auth.RightSet{auth.DesignCompose: true, auth.DesignRead: true}
	for _, scenario := range []struct {
		payload map[string]any
		code    string
	}{
		{map[string]any{"changesetId": cs.ID, "expectedSeq": 1, "group": true, "operations": []any{nodeOp(doc, "node.remove", `{"nodeId":"n_text"}`)}}, "OPERATION_INVALID"},
		{map[string]any{"changesetId": cs.ID, "expectedSeq": 0, "group": true, "operations": []any{nodeOp(doc, "node.insert", `{"parentId":"n_root","subtree":{"id":"n_new","type":"Text"}}`)}}, "CHANGESET_SEQ_CONFLICT"},
	} {
		_, err := e.dispatch(actor, "apply-operations", scenario.payload, "guard")
		if code(err) != scenario.code || e.body(doc, cs.ID) != before {
			t.Fatal(err)
		}
	}
	// Policy applies even to an administrator with zones.manage.
	if _, err := e.apply(cs.ID, 1, nodeOp(doc, "node.insert", `{"parentId":"n_root","subtree":{"id":"n_new","type":"Heading"}}`)); err == nil || e.body(doc, cs.ID) != before {
		t.Fatal("policy bypass", err)
	}
}
