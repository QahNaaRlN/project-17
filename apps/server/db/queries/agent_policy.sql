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
UPDATE changesets SET policy_approval = ARRAY(SELECT DISTINCT unnest(policy_approval || sqlc.arg(rules)::text[]) ORDER BY 1) WHERE id = sqlc.arg(id);

-- name: ChangesetPolicyOperations :many
SELECT o.id, o.actor_id, COALESCE(a.display_name, '')::text AS actor_name, o.source, o.type, o.target_object_id, COALESCE(o.target_schema_name, ob.schema_name, '')::text AS schema_name, o.payload
FROM operations o JOIN actors a ON a.id = o.actor_id LEFT JOIN objects ob ON ob.id = o.target_object_id
WHERE o.changeset_id = $1 AND o.status = 'applied' ORDER BY o.seq;

-- name: PolicySchemaNames :many
SELECT DISTINCT schema_name FROM schema_versions WHERE project_id = $1 ORDER BY schema_name;

-- name: ReviewedAgentChangesets :many
SELECT c.id FROM changesets c
WHERE c.project_id = $1 AND c.state IN ('in_review', 'approved')
  AND EXISTS (SELECT 1 FROM operations o WHERE o.changeset_id = c.id AND o.source = 'agent' AND o.status = 'applied')
ORDER BY c.id;

-- name: PolicyAgentNames :many
SELECT a.display_name FROM agent_definitions d JOIN actors a ON a.id = d.actor_id
WHERE d.project_id = $1 AND a.disabled_at IS NULL ORDER BY a.display_name LIMIT 200;
