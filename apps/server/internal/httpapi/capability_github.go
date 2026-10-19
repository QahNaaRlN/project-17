package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilities"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func capabilityGithubRoutes(router chi.Router, d Deps) {
	s := d.CapabilityGithub
	if s == nil {
		s = &capabilities.GithubService{Pool: d.Pool}
	}
	for _, path := range []string{"/capability-github-config", "/capability-requests/{id}/github", "/capability-requests/{id}/github-observations"} {
		router.Get(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			history := strings.HasSuffix(r.URL.Path, "/github-observations")
			allowed := map[string]bool{"environment": true}
			if history {
				allowed["limit"] = true
				allowed["cursor"] = true
			}
			for k, v := range r.URL.Query() {
				if !allowed[k] || len(v) != 1 || len(v[0]) > 4096 {
					writeError(w, r, d.Log, commandbus.Validation(map[string]string{"query": "invalid filter"}))
					return
				}
			}
			tx, e := d.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if e != nil {
				writeError(w, r, d.Log, e)
				return
			}
			defer tx.Rollback(r.Context())
			q := store.New(tx)
			a := actorFrom(r.Context())
			env := r.URL.Query().Get("environment")
			var result any
			if chi.URLParam(r, "id") == "" {
				if env == "" {
					e = commandbus.Validation(map[string]string{"environment": "required"})
				} else {
					result, e = s.GithubConfig(r.Context(), q, a, env)
				}
			} else {
				id, ok := uuidParam(w, r, d, "id")
				if !ok {
					return
				}
				v, err := capabilities.Get(r.Context(), q, a, id)
				if err != nil {
					writeError(w, r, d.Log, err)
					return
				}
				if env != "" && v["environment"] != env {
					writeError(w, r, d.Log, commandbus.NewError(404, "NOT_FOUND", "Запрос не найден", "Окружение не совпадает"))
					return
				}
				if history {
					limit := 0
					if value := r.URL.Query().Get("limit"); value != "" {
						limit, e = strconv.Atoi(value)
						if e != nil {
							e = commandbus.Validation(map[string]string{"limit": "1..100"})
						}
					}
					if e == nil {
						result, e = s.GithubHistory(r.Context(), q, a, id, limit, r.URL.Query().Get("cursor"))
					}
				} else {
					result, e = s.GithubGet(r.Context(), q, a, id)
				}
			}
			respond(w, r, d, result, e)
		})
	}
}
