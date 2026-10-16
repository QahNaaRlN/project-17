package changes_test

import (
	"context"
	"encoding/json"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"strings"
	"testing"
)

// STU-243/244/251: separate rights, inverses and current primary-content author gate.
func TestBehaviorRightsHistorySTU243251(t *testing.T) {
	for _, operation := range []string{"node.setBehavior", "node.setCondition"} {
		t.Run(operation, func(t *testing.T) {
			e := cntSetup(t)
			entity := e.ContentFixture("entity", "Article", cntArticle("behavior"), "staging")
			initial := cntCS(e)
			made, err := cntOp(e, initial, "document.create", nil, map[string]any{
				"kind": "page", "path": "/behavior", "content": map[string]any{"schema": "Article", "resolve": map[string]any{"by": "entity", "entityId": entity}},
				"root": map[string]any{"id": "n_root", "type": "Button", "bindings": map[string]any{"label": "$content.title"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			page := made.Operations[0].Target
			cntPublish(e, initial)
			author := e.Human("behavior-editor", auth.DesignRead, auth.DesignCompose, auth.BehaviorUse, auth.ContentRead, auth.ContentPublish)
			cs := e.Edit(author, page, "behavior")
			payload := map[string]any{"nodeId": "n_root", "when": map[string]any{"op": "exists", "arg": "$content.title"}}
			field := "when"
			if operation == "node.setBehavior" {
				field = "on"
				payload = map[string]any{"nodeId": "n_root", "event": "click", "handlers": []any{map[string]any{"action": "scrollTo", "args": map[string]any{"target": map[string]any{"lit": "n_root"}}}}}
			}
			command := func(p map[string]any) map[string]any {
				return map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs), "operations": []any{map[string]any{"type": operation, "target": page, "payload": p}}}
			}
			noRead := author
			noRead.Rights = auth.RightSet{auth.DesignRead: true, auth.DesignCompose: true, auth.BehaviorUse: true}
			if err := e.Do(noRead, "apply-operations", command(payload), nil); cmstest.Code(err) != "FORBIDDEN" {
				t.Fatal("primary read", err)
			}
			behaviorOnly := author
			behaviorOnly.Rights = auth.RightSet{auth.DesignRead: true, auth.BehaviorUse: true, auth.ContentRead: true}
			if operation == "node.setCondition" {
				if err := e.Do(behaviorOnly, "apply-operations", command(payload), nil); cmstest.Code(err) != "FORBIDDEN" {
					t.Fatal("condition rights", err)
				}
			} else {
				designOnly := author
				designOnly.Rights = auth.RightSet{auth.DesignRead: true, auth.DesignCompose: true, auth.ContentRead: true}
				if err := e.Do(designOnly, "apply-operations", command(payload), nil); cmstest.Code(err) != "FORBIDDEN" {
					t.Fatal("behavior rights", err)
				}
				author = behaviorOnly
			}
			e.Must(author, "apply-operations", command(payload), nil)
			e.Must(author, "undo", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs)}, nil)
			d, err := changes.GetDocument(context.Background(), e.Q, author.ProjectID, page, &cs)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			_ = json.Unmarshal(d.Body, &body)
			if body["nodes"].(map[string]any)["n_root"].(map[string]any)[field] != nil {
				t.Fatal("inverse retained field")
			}
			e.Must(author, "redo", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs)}, nil)
			removal := map[string]any{"nodeId": "n_root", "when": nil}
			if operation == "node.setBehavior" {
				removal = map[string]any{"nodeId": "n_root", "event": "click", "handlers": nil}
			}
			if err := e.Do(noRead, "apply-operations", command(removal), nil); cmstest.Code(err) != "FORBIDDEN" {
				t.Fatal("removal bypass", err)
			}
			e.Exec(`UPDATE roles SET capabilities=array_remove(capabilities,'content.read') WHERE name='role-behavior-editor'`)
			var review workflow.Review
			e.Must(author, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs), "targets": []string{"staging"}}, &review)
			found := false
			for _, check := range review.Checks {
				found = found || check.Stage == "policy" && check.Status == "failed" && strings.Contains(string(check.Details), operation) && strings.Contains(string(check.Details), "content.read")
			}
			if !found {
				t.Fatal("author read revocation not rechecked", review)
			}
		})
	}
}
