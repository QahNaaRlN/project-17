package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/securityevents"
)

// ErrUserNotFound — no human with this OIDC identity.
var ErrUserNotFound = errors.New("identity: user not found")

// Anonymize — SEC-008, STU-510: the operator removes a person. The name, e-mail and
// OIDC identity are cleared, the actor is disabled, sessions and tokens are revoked
// and role bindings removed; operations, Change Sets, publications and the journal
// keep the actor ID. A repeated call changes nothing and reports false.
func Anonymize(ctx context.Context, pool *pgxpool.Pool, issuer, subject string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	changed := false
	err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		var disabled bool
		err := tx.QueryRow(ctx, `SELECT id, disabled_at IS NOT NULL FROM actors
			WHERE kind = 'human' AND oidc_issuer = $1 AND oidc_subject = $2 FOR UPDATE`, issuer, subject).Scan(&id, &disabled)
		if errors.Is(err, pgx.ErrNoRows) {
			// An anonymised person has no OIDC identity left: the repeat is found by marker.
			sum := sha256.Sum256([]byte(issuer + "\x00" + subject))
			err = tx.QueryRow(ctx, `SELECT id FROM actors WHERE kind = 'human' AND email = $1`, "anonymized:"+hex.EncodeToString(sum[:])).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUserNotFound
			}
			return err
		}
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(issuer + "\x00" + subject))
		short := hex.EncodeToString(id[:4])
		if _, err := tx.Exec(ctx, `UPDATE actors SET display_name = $2, email = $3, oidc_issuer = NULL, oidc_subject = NULL,
			disabled_at = COALESCE(disabled_at, now()) WHERE id = $1`, id, fmt.Sprintf("Удалённый пользователь %s", short),
			"anonymized:"+hex.EncodeToString(sum[:])); err != nil {
			return err
		}
		for _, sql := range []string{
			`DELETE FROM human_sessions WHERE actor_id = $1`,
			`UPDATE api_tokens SET revoked_at = now() WHERE actor_id = $1 AND revoked_at IS NULL`,
			`DELETE FROM role_bindings WHERE actor_id = $1`,
		} {
			if _, err := tx.Exec(ctx, sql, id); err != nil {
				return err
			}
		}
		changed = true
		return securityevents.Record(ctx, tx, securityevents.Event{ActorID: &id, Type: "user.anonymized",
			Data: map[string]any{"alreadyDisabled": disabled}})
	})
	return id, changed, err
}
