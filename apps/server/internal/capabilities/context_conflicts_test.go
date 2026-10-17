package capabilities

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func TestContextDriftAndAnchorConflictsDoNotMutate(t *testing.T) {
	f := setup(t)
	r := f.create()
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		change     func(*store.CapabilityRequest)
		seq        int32
		hash, code string
	}{
		{"seq", func(*store.CapabilityRequest) {}, 0, f.hash, "CAPABILITY_CONTEXT_STALE"},
		{"manifest", func(*store.CapabilityRequest) {}, 1, "sha256:" + strings.Repeat("b", 64), "CAPABILITY_CONTEXT_STALE"},
		{"document", func(r *store.CapabilityRequest) { r.DocumentID = uuid.New() }, 1, f.hash, "CAPABILITY_ANCHOR_CHANGED"},
		{"node", func(r *store.CapabilityRequest) { r.NodeID = "n_absent" }, 1, f.hash, "CAPABILITY_ANCHOR_CHANGED"},
		{"event", func(r *store.CapabilityRequest) { r.Event = "absent" }, 1, f.hash, "CAPABILITY_EVENT_UNAVAILABLE"},
		{"environment", func(r *store.CapabilityRequest) { r.Environment = "production" }, 1, f.hash, "CAPABILITY_ENVIRONMENT_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := r
			tc.change(&candidate)
			_, err := loadContext(ctx, f.e.Q, f.a, candidate, tc.seq, tc.hash, true)
			expectCode(t, err, tc.code)
		})
	}
	if _, err := Preview(ctx, f.e.Q, f.engineer, r.ID); err == nil {
		t.Fatal("engineer previewed an unshared draft")
	}
	// Duplicate open keys cannot create a second draft or bump the CS sequence.
	p := Create{Draft: Draft{Title: r.Title, Description: r.Description, Acceptance: []string{"works"}, ProposedContract: r.ProposedContract}, Environment: r.Environment, ExpectedManifestHash: f.hash, ChangesetID: f.cs, ExpectedSeq: 1, DocumentID: f.doc, NodeID: r.NodeID, Event: r.Event, Key: r.Key}
	_, err := call(f.e, f.a, "create-capability-request", p, "")
	expectCode(t, err, "CAPABILITY_KEY_TAKEN")
	bad := p
	bad.Key = "Bad"
	_, err = call(f.e, f.a, "create-capability-request", bad, "")
	expectCode(t, err, "CAPABILITY_REQUEST_INVALID")
	f.e.Exec(`UPDATE projects SET settings=settings || '{"defaultMode":"SYSTEM","maxMode":"SYSTEM"}' WHERE id=$1`, f.a.ProjectID)
	_, err = Preview(ctx, f.e.Q, f.a, r.ID)
	expectCode(t, err, "CAPABILITY_CODE_REQUIRED")
	f.e.Exec(`UPDATE projects SET settings=settings || '{"defaultMode":"CODE","maxMode":"CODE"}' WHERE id=$1`, f.a.ProjectID)
	f.e.Exec(`UPDATE object_versions SET deleted=true WHERE object_id=$1 AND changeset_id=$2`, f.doc, f.cs)
	_, err = Preview(ctx, f.e.Q, f.a, r.ID)
	expectCode(t, err, "CAPABILITY_ANCHOR_CHANGED")
	view, err := Get(ctx, f.e.Q, f.a, r.ID)
	if err != nil || view["anchorChanged"] != true {
		t.Fatal(view, err)
	}
	f.e.Exec(`UPDATE object_versions SET deleted=false WHERE object_id=$1 AND changeset_id=$2`, f.doc, f.cs)
	f.e.Exec(`UPDATE changesets SET state='abandoned' WHERE id=$1`, f.cs)
	_, err = Preview(ctx, f.e.Q, f.a, r.ID)
	expectCode(t, err, "CAPABILITY_STATE_INVALID")
	draft := p.Draft
	_, err = call(f.e, f.a, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 1, Draft: &draft}, "")
	expectCode(t, err, "CAPABILITY_STATE_INVALID")
}

func TestDraftCannotTargetAnAlreadyAvailableAction(t *testing.T) {
	f := setup(t)
	r := f.create()
	f.hash = f.activate(object{"value": object{"type": "string", "required": true}})
	_, err := loadContext(context.Background(), f.e.Q, f.a, r, 1, f.hash, true)
	expectCode(t, err, "CAPABILITY_ALREADY_AVAILABLE")
	// A missing active manifest is a stale context, never an empty context pack.
	f.e.Exec(`UPDATE environments SET active_manifest_id=NULL WHERE project_id=$1 AND name='staging'`, f.a.ProjectID)
	_, err = Preview(context.Background(), f.e.Q, f.a, r.ID)
	expectCode(t, err, "CAPABILITY_CONTEXT_STALE")
}

func TestReplacingExistingChainNeedsExplicitConfirmation(t *testing.T) {
	f := setup(t)
	r := f.create()
	f.e.Must(f.a, "apply-operations", object{"changesetId": f.cs, "expectedSeq": 1, "operations": []any{object{"type": "node.setBehavior", "target": f.doc, "payload": object{"nodeId": r.NodeID, "event": r.Event, "handlers": []any{object{"action": "scrollTo", "args": object{"target": object{"lit": r.NodeID}}}}}}}}, nil)
	p := Action{ID: r.ID, ExpectedRevision: 1, ExpectedSeq: 2, ExpectedManifestHash: f.hash}
	_, err := call(f.e, f.a, "submit-capability-request", p, "")
	expectCode(t, err, "CAPABILITY_REPLACE_CONFIRM_REQUIRED")
	p.ConfirmReplaceExisting = true
	must(f.e, f.a, "submit-capability-request", p)
	f.e.Must(f.a, "undo", object{"changesetId": f.cs, "expectedSeq": 3}, nil)
	view, err := Get(context.Background(), f.e.Q, f.a, r.ID)
	if err != nil || view["status"] != "submitted" || view["anchorChanged"] != true {
		t.Fatal(view, err)
	}
	_, err = call(f.e, f.a, "refresh-capability-context", Action{ID: r.ID, ExpectedRevision: 2, ExpectedSeq: 4, ExpectedManifestHash: f.hash}, "")
	expectCode(t, err, "CAPABILITY_ANCHOR_CHANGED")
}

// STU-260: neither valid IR event representation may bypass replacement consent.
func TestReplacingSingleHandlerNeedsExplicitConfirmation(t *testing.T) {
	f := setup(t)
	r := f.create()
	before := object{"action": "scrollTo", "args": object{"target": object{"lit": r.NodeID}}}
	f.e.Must(f.a, "apply-operations", object{"changesetId": f.cs, "expectedSeq": 1, "operations": []any{object{"type": "node.setBehavior", "target": f.doc, "payload": object{"nodeId": r.NodeID, "event": r.Event, "handlers": before}}}}, nil)
	p := Action{ID: r.ID, ExpectedRevision: 1, ExpectedSeq: 2, ExpectedManifestHash: f.hash}
	_, err := call(f.e, f.a, "submit-capability-request", p, "")
	expectCode(t, err, "CAPABILITY_REPLACE_CONFIRM_REQUIRED")
	p.ConfirmReplaceExisting = true
	must(f.e, f.a, "submit-capability-request", p)
	f.e.Must(f.a, "undo", object{"changesetId": f.cs, "expectedSeq": 3}, nil)
	s, err := loadContext(context.Background(), f.e.Q, f.a, r, 4, f.hash, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw(obj(obj(obj(s.doc["nodes"])[r.NodeID])["on"])[r.Event])) != string(raw(before)) {
		t.Fatal("undo did not restore the single handler")
	}
}
