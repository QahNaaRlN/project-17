package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// workspaceRoutes serves the current actor's workspace through the public Query API.
func workspaceRoutes(r chi.Router, d Deps) {
	for _, route := range []string{"/pages", "/working-drafts"} {
		r.Get(route, func(w http.ResponseWriter, r *http.Request) {
			actor := actorFrom(r.Context())
			if err := commandbus.Require(actor, auth.DesignRead); err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			values := r.URL.Query()
			for key, entries := range values {
				if len(entries) != 1 || (key != "environment" && key != "after" && !(route == "/working-drafts" && key == "documentId")) {
					writeError(w, r, d.Log, badParam(key, "один поддерживаемый параметр"))
					return
				}
			}
			after := uuid.Nil
			if raw, present := values["after"]; present {
				var err error
				after, err = uuid.Parse(raw[0])
				if err != nil {
					writeError(w, r, d.Log, badParam("after", "UUID"))
					return
				}
			}
			q := store.New(d.Pool)
			env, err := q.GetEnvironmentByName(r.Context(), store.GetEnvironmentByNameParams{ProjectID: actor.ProjectID, Name: values.Get("environment")})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && env.Kind != "standard") {
				respond(w, r, d, nil, badParam("environment", "существующее standard окружение"))
				return
			}
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			if route == "/pages" {
				rows, err := q.ListStudioPages(r.Context(), store.ListStudioPagesParams{ProjectID: actor.ProjectID, OwnerID: actor.ID, Environment: env.Name, AfterID: after})
				items := make([]workspacePage, 0, len(rows))
				for _, row := range rows {
					items = append(items, workspacePage{ID: row.ID, Path: row.Path, Title: row.Title, HasHead: row.HasHead})
				}
				items, next := workspacePageWindow(items, func(p workspacePage) uuid.UUID { return p.ID })
				respond(w, r, d, map[string]any{"items": items, "nextCursor": next}, err)
				return
			}
			documentID, err := uuid.Parse(values.Get("documentId"))
			if err != nil {
				writeError(w, r, d.Log, badParam("documentId", "UUID"))
				return
			}
			rows, err := q.ListStudioDrafts(r.Context(), store.ListStudioDraftsParams{ProjectID: actor.ProjectID, OwnerID: actor.ID, Environment: env.Name, DocumentID: documentID, AfterID: after})
			items := make([]changes.Changeset, 0, len(rows))
			for _, row := range rows {
				items = append(items, changes.ToChangeset(row))
			}
			items, next := workspacePageWindow(items, func(c changes.Changeset) uuid.UUID { return c.ID })
			respond(w, r, d, map[string]any{"items": items, "nextCursor": next}, err)
		})
	}
}

type workspacePage struct {
	ID      uuid.UUID `json:"id"`
	Path    string    `json:"path"`
	Title   string    `json:"title"`
	HasHead bool      `json:"hasHead"`
}

func workspacePageWindow[T any](items []T, id func(T) uuid.UUID) ([]T, *uuid.UUID) {
	if len(items) <= 50 {
		return items, nil
	}
	next := id(items[49])
	return items[:50], &next
}
