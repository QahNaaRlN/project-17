package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilities"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func capabilityRoutes(r chi.Router, d Deps) {
	for _, path := range []string{"/capability-requests", "/capability-requests/{id}", "/capability-requests/{id}/context-preview", "/capability-requests/{id}/context-pack"} {
		r.Get(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			tx, e := d.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if e != nil {
				writeError(w, r, d.Log, e)
				return
			}
			defer func() { _ = tx.Rollback(r.Context()) }()
			q := store.New(tx)
			a := actorFrom(r.Context())
			var result any
			allowed := map[string]bool{"environment": true}
			if chi.URLParam(r, "id") == "" {
				for _, key := range []string{"status", "kind", "key", "cursor", "limit"} {
					allowed[key] = true
				}
			} else if strings.HasSuffix(r.URL.Path, "/context-pack") {
				allowed["version"] = true
			}
			for key, values := range r.URL.Query() {
				if !allowed[key] || len(values) != 1 || len(values[0]) > 4096 {
					writeError(w, r, d.Log, commandbus.Validation(map[string]string{"query": "invalid filter"}))
					return
				}
			}
			if chi.URLParam(r, "id") == "" {
				v := r.URL.Query()
				limit := 0
				if s := v.Get("limit"); s != "" {
					limit, e = strconv.Atoi(s)
					if e != nil {
						e = commandbus.Validation(map[string]string{"limit": "1…100"})
					}
				}
				if e == nil {
					result, e = capabilities.List(r.Context(), q, a, capabilities.Filter{Environment: v.Get("environment"), Status: v.Get("status"), Kind: v.Get("kind"), Key: v.Get("key"), Cursor: v.Get("cursor"), Limit: limit})
				}
			} else {
				id, ok := uuidParam(w, r, d, "id")
				if !ok {
					return
				}
				if environment := r.URL.Query().Get("environment"); environment != "" {
					view, err := capabilities.Get(r.Context(), q, a, id)
					if err != nil {
						writeError(w, r, d.Log, err)
						return
					}
					if view["environment"] != environment {
						writeError(w, r, d.Log, commandbus.NewError(404, "NOT_FOUND", "Запрос не найден", "Запрос недоступен в этом окружении"))
						return
					}
				}
				if strings.HasSuffix(r.URL.Path, "/context-preview") {
					var p capabilities.Pack
					p, e = capabilities.Preview(r.Context(), q, a, id)
					if e == nil {
						result = map[string]any{"hash": p.Hash, "pack": json.RawMessage(p.Body)}
					}
				} else if strings.HasSuffix(r.URL.Path, "/context-pack") {
					version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
					if err != nil || version < 1 {
						e = commandbus.Validation(map[string]string{"version": "positive revision required"})
					} else {
						p, err := capabilities.Download(r.Context(), q, a, id, version)
						e = err
						if e == nil {
							w.Header().Set("Content-Disposition", `attachment; filename="`+capabilities.Filename(id, version)+`"`)
							w.Header().Set("Content-Type", "application/json")
							w.Header().Set("X-Context-Hash", p.Hash)
							w.WriteHeader(http.StatusOK)
							_, _ = w.Write(p.Body)
							return
						}
					}
				} else {
					result, e = capabilities.Get(r.Context(), q, a, id)
				}
			}
			respond(w, r, d, result, e)
		})
	}
}
