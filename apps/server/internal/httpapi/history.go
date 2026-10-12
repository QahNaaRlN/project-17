package httpapi

import (
	"math"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func historyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := uuidParam(w, r, d, "id")
		if !ok {
			return
		}
		before := int32(math.MaxInt32)
		values := r.URL.Query()
		for key, entries := range values {
			if key != "beforeSeq" || len(entries) != 1 {
				writeError(w, r, d.Log, badParam(key, "один beforeSeq"))
				return
			}
		}
		if values.Has("beforeSeq") {
			n, err := strconv.ParseInt(values.Get("beforeSeq"), 10, 32)
			if err != nil || n < 1 {
				writeError(w, r, d.Log, badParam("beforeSeq", "положительное целое"))
				return
			}
			before = int32(n)
		}
		tx, err := d.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		defer tx.Rollback(r.Context()) //nolint:errcheck
		result, err := changes.GetHistory(r.Context(), store.New(tx), actorFrom(r.Context()), id, before)
		if err == nil {
			err = tx.Commit(r.Context())
		}
		respond(w, r, d, result, err)
	}
}
