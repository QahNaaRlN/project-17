package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Studio reads the same working/head base that content commands edit.
func studioContentRoutes(router chi.Router, d Deps) {
	for _, route := range []string{"/content-catalog", "/content/{id}"} {
		router.Get(route, func(w http.ResponseWriter, r *http.Request) {
			actor := actorFrom(r.Context())
			if err := commandbus.Require(actor, auth.ContentRead); err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			values := r.URL.Query()
			allowed := []string{"environment", "changesetId"}
			if route == "/content-catalog" {
				allowed = append(allowed, "kind", "schema", "after", "q")
			}
			for key, entries := range values {
				if len(entries) != 1 || !slices.Contains(allowed, key) {
					writeError(w, r, d.Log, badParam(key, "один поддерживаемый параметр"))
					return
				}
			}
			cs, err := uuid.Parse(values.Get("changesetId"))
			if err != nil {
				writeError(w, r, d.Log, badParam("changesetId", "UUID"))
				return
			}
			q := store.New(d.Pool)
			if err := authorizeWorkRead(r.Context(), q, actor, cs); err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			change, err := q.GetChangeset(r.Context(), store.GetChangesetParams{ProjectID: actor.ProjectID, ID: cs})
			if errors.Is(err, pgx.ErrNoRows) {
				respond(w, r, d, nil, commandbus.NewError(404, "NOT_FOUND", "Черновик не найден", ""))
				return
			}
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			env, err := q.GetEnvironmentByName(r.Context(), store.GetEnvironmentByNameParams{ProjectID: actor.ProjectID, Name: values.Get("environment")})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && (env.Kind != "standard" || change.Kind != "standard" || !slices.Contains(change.Targets, env.Name))) {
				respond(w, r, d, nil, badParam("environment", "standard target текущего CS"))
				return
			}
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			if route != "/content-catalog" {
				id, ok := uuidParam(w, r, d, "id")
				if !ok {
					return
				}
				value, err := changes.GetContent(r.Context(), q, actor.ProjectID, id, env.Name, &cs, true)
				respond(w, r, d, value, err)
				return
			}
			kind, schema := values.Get("kind"), values.Get("schema")
			if !slices.Contains([]string{"entity", "asset"}, kind) || (kind == "asset" && schema != "") {
				respond(w, r, d, nil, badParam("kind", "entity/asset и схема только для entity"))
				return
			}
			after := uuid.Nil
			if raw, ok := values["after"]; ok {
				after, err = uuid.Parse(raw[0])
				if err != nil {
					respond(w, r, d, nil, badParam("after", "UUID"))
					return
				}
			}
			if len([]rune(values.Get("q"))) > 200 {
				writeError(w, r, d.Log, badParam("q", "до 200 символов"))
				return
			}
			rows, err := q.ListStudioContent(r.Context(), store.ListStudioContentParams{ProjectID: actor.ProjectID, ChangesetID: cs, Kind: kind, SchemaName: schema, AfterID: after, Search: values.Get("q")})
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			items := make([]studioContentItem, 0, len(rows))
			for _, row := range rows {
				items = append(items, studioContentItem{ID: row.ID, Kind: row.Kind, Schema: row.SchemaName, Title: row.Title, Deleted: row.Deleted, AssetKind: row.AssetKind, MIME: row.MimeType})
			}
			items, next := workspacePageWindow(items, func(item studioContentItem) uuid.UUID { return item.ID })
			settings, err := q.GetProjectSettings(r.Context(), actor.ProjectID)
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			locales := struct {
				Locales       []string `json:"locales"`
				DefaultLocale string   `json:"defaultLocale"`
			}{Locales: []string{}, DefaultLocale: "ru"}
			if err := json.Unmarshal(settings, &locales); err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			if locales.Locales == nil {
				locales.Locales = []string{}
			}
			if locales.DefaultLocale == "" {
				locales.DefaultLocale = "ru"
			}
			if len(locales.Locales) > 0 && !slices.Contains(locales.Locales, locales.DefaultLocale) {
				locales.DefaultLocale = locales.Locales[0]
			}
			respond(w, r, d, map[string]any{"items": items, "nextCursor": next, "locales": locales.Locales, "defaultLocale": locales.DefaultLocale}, nil)
		})
	}
}

type studioContentItem struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	Schema    *string   `json:"schema,omitempty"`
	Title     string    `json:"title"`
	Deleted   bool      `json:"deleted"`
	AssetKind string    `json:"assetKind,omitempty"`
	MIME      string    `json:"mimeType,omitempty"`
}
