-- name: LockIdempotencyKey :exec
-- Сериализует параллельные запросы с одним ключом до конца транзакции.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(actor_id)::text || ':' || sqlc.arg(key)::text, 0));

-- name: GetIdempotencyKey :one
SELECT * FROM idempotency_keys
WHERE actor_id = $1 AND key = $2 AND created_at > now() - interval '24 hours';

-- name: SaveIdempotencyKey :exec
INSERT INTO idempotency_keys (actor_id, key, command, request_hash, status, response)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (actor_id, key) DO UPDATE
SET command = EXCLUDED.command, request_hash = EXCLUDED.request_hash,
    status = EXCLUDED.status, response = EXCLUDED.response, created_at = now();
