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
	for _, suffix := range []string{"", "/{id}", "/{id}/diff", "/{id}/rollback-preview", "/{id}/rollback-preview/diff", "/{id}/promote-preview", "/{id}/promote-preview/diff"} {
		router.Get("/publication-history"+suffix, func(w http.ResponseWriter, r *http.Request) {
			params := r.URL.Query()
			allowed := []string{"environment"}
			detail := suffix == "/{id}/diff" || suffix == "/{id}/rollback-preview/diff" || suffix == "/{id}/promote-preview/diff"
			promote := suffix == "/{id}/promote-preview" || suffix == "/{id}/promote-preview/diff"
			if suffix == "" {
				allowed = append(allowed, "limit", "cursor")
			}
			if detail {
				allowed = append(allowed, "objectId", "limit", "cursor", "snapshot")
			}
			if promote {
				allowed = append(allowed, "toEnvironment")
			}
			for k, v := range params {
				if len(v) != 1 || !slices.Contains(allowed, k) {
					writeError(w, r, d.Log, badParam(k, "one supported parameter"))
					return
				}
			}
			if promote && (params.Get("toEnvironment") == "" || len(params.Get("toEnvironment")) > 100) {
				writeError(w, r, d.Log, badParam("toEnvironment", "environment name"))
				return
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
			// The promote preview evaluates approvals with the command's own code, which takes
			// row locks; its transaction is therefore read-write but always rolled back (STU-462).
			mode := pgx.ReadOnly
			if promote {
				mode = pgx.ReadWrite
			}
			tx, err := d.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: mode})
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
			if promote && detail {
				result, err = publishing.ReadPromotePage(r.Context(), q, a, id, obj, params.Get("toEnvironment"), params.Get("snapshot"), params.Get("cursor"), limit)
			} else if promote {
				result, _, _, err = publishing.LoadPromotePreview(r.Context(), q, a, id, params.Get("toEnvironment"), nil)
			} else if suffix == "" {
				result, err = publishing.ReadHistory(r.Context(), q, a, params.Get("environment"), params.Get("cursor"), limit)
			} else if detail {
				result, err = publishing.ReadHistoryPage(r.Context(), q, a, id, obj, preview, params.Get("snapshot"), params.Get("cursor"), limit)
			} else {
				result, _, _, err = publishing.LoadPublicationHistory(r.Context(), q, a, id, preview, nil)
			}
			if err == nil && !promote {
				err = tx.Commit(r.Context())
			}
			w.Header().Set("Cache-Control", "no-store")
			respond(w, r, d, result, err)
		})
	}
}
