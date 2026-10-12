package httpapi

import (
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

func reviewHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := uuidParam(w, r, d, "id")
		if !ok {
			return
		}
		if len(r.URL.Query()) != 0 {
			writeError(w, r, d.Log, badParam("query", "без параметров"))
			return
		}
		tx, err := d.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		defer tx.Rollback(r.Context()) //nolint:errcheck
		result, err := workflow.GetStudioReview(r.Context(), store.New(tx), actorFrom(r.Context()), id)
		if err == nil {
			err = tx.Commit(r.Context())
		}
		respond(w, r, d, result, err)
	}
}
