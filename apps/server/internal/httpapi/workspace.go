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
	studioContentRoutes(r, d)
	for _, route := range []string{"/pages", "/working-drafts", "/work-items"} {
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
			if route == "/work-items" {
				rows, err := q.ListStudioWork(r.Context(), store.ListStudioWorkParams{ProjectID: actor.ProjectID, ActorID: actor.ID, Environment: env.Name, AfterID: after, CanReview: actor.Kind == auth.ActorHuman && actor.Rights.Has(auth.ContentPublish)})
				if err != nil {
					respond(w, r, d, nil, err)
					return
				}
				items := make([]workItem, 0, len(rows))
				for _, row := range rows {
					items = append(items, workItem{ID: row.ID, Title: row.Title, OwnerID: row.OwnerID, OwnerName: row.OwnerName, State: row.State, DocumentID: row.DocumentID, Path: row.Path, Own: row.OwnerID == actor.ID})
				}
				items, next := workspacePageWindow(items, func(i workItem) uuid.UUID { return i.ID })
				respond(w, r, d, map[string]any{"items": items, "nextCursor": next}, nil)
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
type workItem struct {
	ID         uuid.UUID `json:"id"`
	Title      string    `json:"title"`
	OwnerID    uuid.UUID `json:"ownerId"`
	OwnerName  string    `json:"ownerName"`
	State      string    `json:"state"`
	DocumentID uuid.UUID `json:"documentId"`
	Path       string    `json:"path"`
	Own        bool      `json:"own"`
}

func workspacePageWindow[T any](items []T, id func(T) uuid.UUID) ([]T, *uuid.UUID) {
	if len(items) <= 50 {
		return items, nil
	}
	next := id(items[49])
	return items[:50], &next
}
