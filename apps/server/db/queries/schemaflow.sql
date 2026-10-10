-- name: InsertSchemaOperation :exec
INSERT INTO operations(id,project_id,changeset_id,seq,actor_id,source,target_schema_name,type,payload,before,after)
VALUES ($1,$2,$3,$4,$5,$6,$7,'schema.apply',$8,$9,$10);

-- name: SchemaOperations :many
SELECT * FROM operations WHERE changeset_id=$1 AND type='schema.apply' AND status='applied' ORDER BY seq;

-- name: ClaimSchemaChangeset :one
UPDATE changesets SET owner_id=$2, updated_at=now() WHERE id=$1 RETURNING *;

-- name: RecordSchemaClaim :exec
INSERT INTO schema_changeset_claims(id,changeset_id,previous_owner_id,owner_id) VALUES($1,$2,$3,$4);

-- name: SchemaOwner :one
SELECT a.kind FROM changesets cs JOIN actors a ON a.id=cs.owner_id WHERE cs.id=$1;

-- name: PublicationManifest :exec
UPDATE publications SET previous_manifest_id=$2, current_manifest_id=$3, manifest_changed=true WHERE id=$1;
