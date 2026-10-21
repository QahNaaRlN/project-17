-- name: CurrentAgentPolicy :one
SELECT * FROM agent_policy_versions WHERE project_id = $1 ORDER BY version DESC LIMIT 1;

-- name: AgentPolicyVersions :many
SELECT v.version, v.body, v.hash, v.author_id, COALESCE(a.display_name, '')::text AS author_name, v.reason, v.created_at
FROM agent_policy_versions v LEFT JOIN actors a ON a.id = v.author_id
WHERE v.project_id = $1 ORDER BY v.version DESC LIMIT 100;

-- name: InsertAgentPolicyVersion :one
INSERT INTO agent_policy_versions (project_id, version, body, hash, author_id, reason)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: AddPolicyApproval :exec
UPDATE changesets SET policy_approval = ARRAY(SELECT DISTINCT unnest(policy_approval || $2::text[]) ORDER BY 1) WHERE id = $1;

-- name: AgentOperations :many
SELECT o.id, o.actor_id, o.agent_session_id, o.type, o.target_object_id, o.payload FROM operations o
WHERE o.changeset_id = $1 AND o.status = 'applied' AND o.source = 'agent' ORDER BY o.seq;

-- name: PolicySchemaNames :many
SELECT DISTINCT schema_name FROM schema_versions WHERE project_id = $1 ORDER BY schema_name;
