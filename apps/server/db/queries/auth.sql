-- name: CreateActor :one
INSERT INTO actors (id, kind, project_id, display_name)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateRole :one
INSERT INTO roles (id, project_id, name, capabilities) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: BindRole :exec
INSERT INTO role_bindings (project_id, actor_id, role_id) VALUES ($1, $2, $3);

-- name: CreateAPIToken :one
INSERT INTO api_tokens (id, project_id, actor_id, token_hash, scopes, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetTokenPrincipal :one
-- Действующий токен вместе с его актором и проектом.
SELECT t.id AS token_id, t.scopes, t.environments, t.expires_at,
       a.id AS actor_id, a.kind AS actor_kind, a.display_name,
       p.id AS project_id, p.slug AS project_slug
FROM api_tokens t
JOIN actors a ON a.id = t.actor_id
JOIN projects p ON p.id = t.project_id
WHERE t.token_hash = $1
  AND t.revoked_at IS NULL
  AND t.expires_at > now()
  AND a.disabled_at IS NULL;

-- name: TouchToken :exec
UPDATE api_tokens SET last_used_at = now() WHERE id = $1;

-- name: ActorCapabilities :many
SELECT DISTINCT unnest(r.capabilities)::text AS capability
FROM role_bindings b
JOIN roles r ON r.id = b.role_id
WHERE b.project_id = $1 AND b.actor_id = $2
ORDER BY 1;
