package httpapi

import (
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/reviewdiff"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"net/http"
	"slices"
	"strconv"
)

func reviewDiffRoutes(router chi.Router, d Deps) {
	for _, route := range []string{"/changesets/{id}/diff", "/changesets/{id}/diff/{objectId}"} {
		router.Get(route, func(w http.ResponseWriter, r *http.Request) {
			id, ok := uuidParam(w, r, d, "id")
			if !ok {
				return
			}
			params := r.URL.Query()
			detail := route != "/changesets/{id}/diff"
			allowed := []string{"environment"}
			if detail {
				allowed = append(allowed, "baseline", "snapshot", "after", "limit")
			}
			for k, v := range params {
				if len(v) != 1 || !slices.Contains(allowed, k) {
					writeError(w, r, d.Log, badParam(k, "one supported parameter"))
					return
				}
			}
			if params.Get("environment") == "" {
				writeError(w, r, d.Log, badParam("environment", "required"))
				return
			}
			var obj uuid.UUID
			limit := 100
			var err error
			if detail {
				obj, err = uuid.Parse(chi.URLParam(r, "objectId"))
				if err == nil && params.Has("limit") {
					limit, err = strconv.Atoi(params.Get("limit"))
				}
				if err != nil || len(params.Get("after")) > 2048 {
					writeError(w, r, d.Log, badParam("query", "valid UUID and cursor"))
					return
				}
			}
			tx, err := d.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			defer tx.Rollback(r.Context()) //nolint:errcheck
			var result any
			q := store.New(tx)
			actor := actorFrom(r.Context())
			if detail {
				result, err = reviewdiff.ReadPage(r.Context(), q, actor, id, obj, params.Get("environment"), params.Get("baseline"), params.Get("snapshot"), params.Get("after"), limit)
			} else {
				result, err = reviewdiff.ReadIndex(r.Context(), q, actor, id, params.Get("environment"))
			}
			if err == nil {
				err = tx.Commit(r.Context())
			}
			w.Header().Set("Cache-Control", "no-store")
			respond(w, r, d, result, err)
		})
	}
}
