package reviewdiff

import (
	"context"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// HistoricalImage deliberately accepts only committed versions of this project/object.
// Callers must first authorize a publication and constrain IDs to its immutable items.
func HistoricalImage(ctx context.Context, q *store.Queries, project, obj uuid.UUID, id *uuid.UUID, schema *string, kind string) (*Image, error) {
	return image(ctx, q, project, obj, id, schema, nil, kind)
}
func HistoricalTitle(kind string, img *Image, path *string, id uuid.UUID) string {
	return title(kind, img, path, id)
}
func Snapshot(v any) string                                     { return hash(v) }
func SignCursor(context string, n int, key []byte) string       { return cursor(context, n, key) }
func CursorOffset(raw, context string, key []byte) (int, error) { return offset(raw, context, key) }
