package capabilities

import (
	"context"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func TestDraftCreationRollsBackEveryStorageFailure(t *testing.T) {
	for _, table := range []string{"environments", "changesets", "projects", "manifests", "object_versions", "capability_requests", "capability_request_audit"} {
		t.Run(table, func(t *testing.T) {
			f := setup(t)
			p := Create{Draft: Draft{Title: "New", Description: "A new action", Acceptance: []string{"works"}, ProposedContract: raw(object{"args": object{}})}, Environment: "staging", ExpectedManifestHash: f.hash, ChangesetID: f.cs, ExpectedSeq: 1, DocumentID: f.doc, NodeID: "n_root", Event: "click", Key: "store.new"}
			operation := ""
			if table == "capability_requests" || table == "capability_request_audit" {
				operation = "INSERT"
			}
			f.e.Break(table, operation)
			if _, err := call(f.e, f.a, "create-capability-request", p, ""); err == nil {
				t.Fatal("storage failure reported success")
			}
		})
	}
}

func TestRequestUpdatesRollbackAndRepeatCurrentRights(t *testing.T) {
	for _, stage := range []string{"draft", "submit", "refresh", "resolve"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t)
			var r store.CapabilityRequest
			if stage == "draft" || stage == "submit" {
				r = f.create()
			} else {
				r = f.submit()
			}
			seq := int32(1)
			if stage == "refresh" {
				seq = 2
			}
			if stage == "resolve" {
				r = f.engineering(r)
				f.hash = f.activate(object{"value": object{"type": "string", "required": true}})
				seq = 2
			}
			f.e.Break("capability_requests", "UPDATE")
			var err error
			if stage == "draft" {
				d := Draft{Title: "Changed", Description: "Updated task", Acceptance: []string{"works"}, ProposedContract: r.ProposedContract}
				_, err = call(f.e, f.a, "update-capability-request", Update{ID: r.ID, ExpectedRevision: r.Revision, Draft: &d}, "")
			} else {
				name := map[string]string{"submit": "submit-capability-request", "refresh": "refresh-capability-context", "resolve": "resolve-capability-request"}[stage]
				p := Action{ID: r.ID, ExpectedRevision: r.Revision, ExpectedSeq: seq, ExpectedManifestHash: f.hash}
				if stage == "resolve" {
					p.Handlers = raw([]any{object{"action": r.Key, "args": object{"value": object{"lit": "value"}}}})
				}
				_, err = call(f.e, f.a, name, p, "")
			}
			if err == nil {
				t.Fatal("update failure accepted")
			}
			saved, err := f.e.Q.GetCapabilityRequest(context.Background(), store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: r.ID})
			if err != nil || saved.Status != r.Status || saved.Revision != r.Revision {
				t.Fatal(saved, err)
			}
		})
	}
	f := setup(t)
	r := f.create()
	noBehavior := f.a
	noBehavior.Rights = auth.RightSet{auth.DesignRead: true, auth.CapabilityRequest: true}
	_, err := call(f.e, noBehavior, "submit-capability-request", Action{ID: r.ID, ExpectedRevision: 1, ExpectedSeq: 1, ExpectedManifestHash: f.hash}, "")
	expectCode(t, err, "FORBIDDEN")
	if _, err := Preview(context.Background(), f.e.Q, noBehavior, r.ID); err != nil {
		t.Fatal("read context requires behavior edit", err)
	}
}
