-- name: AgentByToken :one
SELECT s.* FROM agent_sessions s JOIN actors a ON a.id=s.agent_id
WHERE s.token_hash=$1 AND a.kind='agent' AND a.project_id=s.project_id AND a.disabled_at IS NULL
AND s.revoked_at IS NULL AND s.expires_at>now();

-- name: GetAgentSession :one
SELECT * FROM agent_sessions WHERE id=$1 AND project_id=$2;

-- name: LockAgentSession :one
SELECT * FROM agent_sessions WHERE id=$1 AND project_id=$2 FOR UPDATE;

-- name: AgentChangesets :many
SELECT changeset_id FROM agent_session_changesets WHERE session_id=$1 ORDER BY changeset_id;

-- name: AgentPrincipal :one
SELECT a.id,a.display_name,a.disabled_at,p.slug FROM actors a JOIN projects p ON p.id=a.project_id
WHERE a.id=$1 AND a.kind='agent' AND a.project_id=$2;

-- name: AgentProjectSettings :one
SELECT settings FROM projects WHERE id=$1;

-- name: AgentDelegator :one
SELECT id,kind,disabled_at FROM actors WHERE id=$1;

-- name: ChargeAgentOperations :exec
UPDATE agent_sessions SET ops_used=ops_used+$2 WHERE id=$1;

-- name: AddAgentChangeset :exec
INSERT INTO agent_session_changesets(session_id,changeset_id) VALUES($1,$2) ON CONFLICT DO NOTHING;
