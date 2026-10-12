-- name: CreateOIDCLogin :exec
INSERT INTO oidc_logins (state_hash,browser_hash,verifier,nonce,project_slug,expires_at) VALUES ($1,$2,$3,$4,$5,$6);

-- name: ConsumeOIDCLogin :one
DELETE FROM oidc_logins WHERE state_hash=$1 AND browser_hash=$2 AND project_slug=$3 AND expires_at>now() RETURNING *;

-- name: CleanupHumanSessions :exec
DELETE FROM human_sessions WHERE expires_at<=now();

-- name: CleanupOIDCLogins :exec
DELETE FROM oidc_logins WHERE expires_at<=now();

-- name: UpsertOIDCHuman :one
INSERT INTO actors (id,kind,display_name,oidc_issuer,oidc_subject) VALUES ($1,'human',$2,$3,$4)
ON CONFLICT (oidc_issuer,oidc_subject) DO UPDATE SET display_name=EXCLUDED.display_name
WHERE actors.kind='human' AND actors.disabled_at IS NULL RETURNING *;

-- name: CreateHumanSession :exec
INSERT INTO human_sessions (token_hash,actor_id,csrf_token,expires_at) VALUES ($1,$2,$3,$4);

-- name: HumanSessionPrincipal :one
SELECT a.id,a.display_name,s.csrf_token,s.expires_at FROM human_sessions s JOIN actors a ON a.id=s.actor_id
WHERE s.token_hash=$1 AND s.expires_at>now() AND a.kind='human' AND a.disabled_at IS NULL;

-- name: DeleteHumanSession :exec
DELETE FROM human_sessions WHERE token_hash=$1;

-- name: HumanRoleCount :one
SELECT count(*) FROM role_bindings b JOIN roles r ON r.id=b.role_id AND r.project_id=b.project_id
WHERE b.project_id=$1 AND b.actor_id=$2;

-- name: GetActiveHuman :one
SELECT * FROM actors WHERE id=$1 AND kind='human' AND disabled_at IS NULL;

-- name: GetProjectRole :one
SELECT * FROM roles WHERE id=$1 AND project_id=$2;

-- name: BindHumanRole :exec
INSERT INTO role_bindings (project_id,actor_id,role_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING;

-- name: UnbindHumanRole :exec
DELETE FROM role_bindings WHERE project_id=$1 AND actor_id=$2 AND role_id=$3;
