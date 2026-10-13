package agents

import (
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"testing"
)

func TestAgentContentOnlyChangeRaisesLowRisk(t *testing.T) { // AGT-002/031: authored content still needs a human reviewer.
	e, s, h, parent, doc := setupAgent(t)
	// Fixture represents already committed content. The new CS has no human
	// document.create operation that could otherwise raise its risk.
	e.Exec(`UPDATE object_versions SET state='committed',number=1,committed_at=now() WHERE id IN (SELECT working_version_id FROM changeset_objects WHERE changeset_id=$1)`, parent)
	e.Exec(`UPDATE objects SET head_version_id=(SELECT working_version_id FROM changeset_objects WHERE changeset_id=$1 AND object_id=$2) WHERE id=$2`, parent, doc)
	e.Exec(`UPDATE projects SET settings='{"approvals":{"low":0,"medium":0,"high":0}}' WHERE id=$1`, h.ProjectID)
	var cs changes.Changeset
	e.Must(h, "create-changeset", map[string]any{"title": "Agent content only", "targets": []string{"staging"}}, &cs)
	id := registered(t, e)
	credential, hash := auth.NewToken(auth.AgentTokenPrefix)
	var session Session
	e.Must(h, "start-agent-session", sessionPayload{TokenHash: hash, AgentID: id, Changesets: []uuid.UUID{cs.ID}, Rights: []string{string(auth.ContentWrite), string(auth.DesignRead)}, TTLSeconds: 60, OpsQuota: 2}, &session)
	session.Token = credential
	actor := agentActor(t, e, session)
	if _, err := s.Call(ctx, actor, "set_local_content", mustJSON(map[string]any{"changesetId": cs.ID, "documentId": doc, "expectedSeq": 0, "reason": "Edit text only", "payload": map[string]any{"key": "t_agent", "type": "text", "locale": "ru", "value": "Reviewed text"}}), "content-only", nil); err != nil {
		t.Fatal(err)
	}
	var review workflow.Review
	e.Must(h, "submit-changeset", map[string]any{"changesetId": cs.ID, "expectedSeq": 1}, &review)
	if review.Risk == nil || *review.Risk != workflow.RiskMedium || review.RequiredApprovals != 1 || review.Changeset.State != "in_review" {
		t.Fatal(review)
	}
}
