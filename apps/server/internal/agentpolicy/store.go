package agentpolicy

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Load returns the current version of the project's policy; a project
// without versions has the empty policy (only the ceiling applies).
func Load(ctx context.Context, q *store.Queries, project uuid.UUID) (Document, int32, error) {
	row, err := q.CurrentAgentPolicy(ctx, project)
	if errors.Is(err, pgx.ErrNoRows) {
		return Legacy(nil, nil), 0, nil
	}
	if err != nil {
		return Document{}, 0, err
	}
	d, err := Parse(row.Body)
	return d, row.Version, err
}
