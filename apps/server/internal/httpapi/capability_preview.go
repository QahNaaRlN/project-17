package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilities"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// capabilityPreviewRoutes — Query API preview/<ref> (STU-318…323). Reads never
// call GitHub and are not cached.
func capabilityPreviewRoutes(router chi.Router, d Deps) {
	s := d.CapabilityGithub
	if s == nil {
		s = &capabilities.GithubService{Pool: d.Pool}
	}
	for _, path := range []string{"/capability-requests/{id}/preview", "/changesets/{id}/preview"} {
		router.Get(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			request := strings.Contains(r.URL.Path, "/capability-requests/")
			for k, v := range r.URL.Query() {
				if k != "environment" || len(v) != 1 || len(v[0]) > 128 {
					writeError(w, r, d.Log, commandbus.Validation(map[string]string{"query": "invalid filter"}))
					return
				}
			}
			env := r.URL.Query().Get("environment")
			if !request && env == "" {
				writeError(w, r, d.Log, commandbus.Validation(map[string]string{"environment": "required"}))
				return
			}
			id, ok := uuidParam(w, r, d, "id")
			if !ok {
				return
			}
			tx, e := d.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if e != nil {
				writeError(w, r, d.Log, e)
				return
			}
			defer tx.Rollback(r.Context())
			q := store.New(tx)
			a := actorFrom(r.Context())
			var result any
			if request {
				v, err := capabilities.Get(r.Context(), q, a, id)
				if err == nil && env != "" && v["environment"] != env {
					err = commandbus.NewError(404, "NOT_FOUND", "Запрос не найден", "Окружение не совпадает")
				}
				if err != nil {
					writeError(w, r, d.Log, err)
					return
				}
				result, e = s.PreviewGet(r.Context(), q, a, id)
			} else {
				result, e = capabilities.ChangesetPreview(r.Context(), q, a, id, env)
			}
			respond(w, r, d, result, e)
		})
	}
}
