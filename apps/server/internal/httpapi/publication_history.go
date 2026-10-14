package httpapi

import (
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"net/http"
	"slices"
	"strconv"
)

func publicationHistoryRoutes(router chi.Router, d Deps) {
	for _, suffix := range []string{"", "/{id}", "/{id}/diff", "/{id}/rollback-preview", "/{id}/rollback-preview/diff"} {
		router.Get("/publication-history"+suffix, func(w http.ResponseWriter, r *http.Request) {
			params := r.URL.Query()
			allowed := []string{"environment"}
			detail := suffix == "/{id}/diff" || suffix == "/{id}/rollback-preview/diff"
			if suffix == "" {
				allowed = append(allowed, "limit", "cursor")
			}
			if detail {
				allowed = append(allowed, "objectId", "limit", "cursor", "snapshot")
			}
			for k, v := range params {
				if len(v) != 1 || !slices.Contains(allowed, k) {
					writeError(w, r, d.Log, badParam(k, "one supported parameter"))
					return
				}
			}
			if len(params.Get("cursor")) > 2048 {
				writeError(w, r, d.Log, badParam("cursor", "bounded cursor"))
				return
			}
			var id, obj uuid.UUID
			var ok bool
			if suffix != "" {
				id, ok = uuidParam(w, r, d, "id")
				if !ok {
					return
				}
			}
			if detail {
				var err error
				obj, err = uuid.Parse(params.Get("objectId"))
				if err != nil {
					writeError(w, r, d.Log, badParam("objectId", "UUID"))
					return
				}
			}
			limit := 50
			if detail {
				limit = 100
			}
			if params.Has("limit") {
				var err error
				limit, err = strconv.Atoi(params.Get("limit"))
				if err != nil || limit < 1 || limit > 100 {
					writeError(w, r, d.Log, badParam("limit", "1..100"))
					return
				}
			}
			tx, err := d.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			defer tx.Rollback(r.Context()) //nolint:errcheck
			q := store.New(tx)
			a := actorFrom(r.Context())
			var result any
			if suffix != "" && params.Has("environment") {
				p, e := q.GetPublication(r.Context(), store.GetPublicationParams{ProjectID: a.ProjectID, ID: id})
				if e != nil || p.EnvironmentName != params.Get("environment") {
					writeError(w, r, d.Log, commandbus.NewError(404, "NOT_FOUND", "Публикация недоступна", ""))
					return
				}
			}
			preview := suffix == "/{id}/rollback-preview" || suffix == "/{id}/rollback-preview/diff"
			if suffix == "" {
				result, err = publishing.ReadHistory(r.Context(), q, a, params.Get("environment"), params.Get("cursor"), limit)
			} else if detail {
				result, err = publishing.ReadHistoryPage(r.Context(), q, a, id, obj, preview, params.Get("snapshot"), params.Get("cursor"), limit)
			} else {
				result, _, _, err = publishing.LoadPublicationHistory(r.Context(), q, a, id, preview, nil)
			}
			if err == nil {
				err = tx.Commit(r.Context())
			}
			w.Header().Set("Cache-Control", "no-store")
			respond(w, r, d, result, err)
		})
	}
}
