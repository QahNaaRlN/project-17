package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Match находит маршрут для пути запроса. Литеральный сегмент сильнее параметра: при
// '/a/b' и '/a/:x' путь '/a/b' ведёт на первый. Формы маршрутов уникальны, поэтому
// лучший кандидат один.
func Match(routes []publishing.Route, path string) (publishing.Route, map[string]string, bool) {
	want := segments(path)
	var best publishing.Route
	var bestParams map[string]string
	var bestRank []bool
	for _, r := range routes {
		params, rank, ok := matchSegments(segments(r.Path), want)
		if ok && (bestRank == nil || stronger(rank, bestRank)) {
			best, bestParams, bestRank = r, params, rank
		}
	}
	return best, bestParams, bestRank != nil
}

// matchSegments сопоставляет сегменты шаблона с сегментами пути; rank[i] — сегмент литеральный.
func matchSegments(pattern, want []string) (map[string]string, []bool, bool) {
	if len(pattern) != len(want) {
		return nil, nil, false
	}
	params := map[string]string{}
	rank := make([]bool, len(pattern))
	for i, seg := range pattern {
		name, isParam := strings.CutPrefix(seg, ":")
		if isParam && want[i] != "" {
			params[name] = want[i]
		} else if isParam || seg != want[i] {
			return nil, nil, false
		} else {
			rank[i] = true
		}
	}
	return params, rank, true
}

// stronger — первый различающийся сегмент у a литерал, а у b параметр.
func stronger(a, b []bool) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i]
		}
	}
	return false
}

func segments(path string) []string {
	if path == "/" {
		return []string{}
	}
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

// PageRef — найденная страница.
type PageRef struct {
	ObjectID  uuid.UUID         `json:"objectId"`
	VersionID uuid.UUID         `json:"versionId"`
	Path      string            `json:"path"` // шаблон маршрута
	Params    map[string]string `json:"params"`
}

// Page — ответ /page (08 §5.1). Компоненты, данные и источники появятся вместе с
// Composed-компонентами и контентом; пока они пустые.
type Page struct {
	Page        PageRef         `json:"page"`
	Document    json.RawMessage `json:"document"`
	Components  map[string]any  `json:"components"`
	Data        map[string]any  `json:"data"`
	DataSources map[string]any  `json:"dataSources"`
	// Diagnostics — диагностика документа в черновом режиме (API-042).
	Diagnostics []ir.Diagnostic `json:"diagnostics,omitzero"`
}

// Document — ответ /document/{id}.
type Document struct {
	ObjectID  uuid.UUID       `json:"objectId"`
	VersionID uuid.UUID       `json:"versionId"`
	Path      *string         `json:"path"`
	Document  json.RawMessage `json:"document"`
	// Diagnostics — диагностика документа в черновом режиме (API-042).
	Diagnostics []ir.Diagnostic `json:"diagnostics,omitzero"`
}

func notFound(detail string) error {
	return commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Не найдено", detail)
}

// GetPage возвращает опубликованную страницу по пути запроса.
func GetPage(ctx context.Context, q *store.Queries, a Access, path string) (Page, error) {
	routes, err := GetRoutes(ctx, q, a)
	if err != nil {
		return Page{}, err
	}
	route, params, ok := Match(routes, path)
	if !ok {
		return Page{}, notFound(fmt.Sprintf("Страница по пути %q не опубликована в окружении %q", path, a.Environment))
	}
	d, err := GetDocument(ctx, q, a, route.ObjectID)
	if err != nil {
		return Page{}, err
	}
	return Page{
		Page:     PageRef{ObjectID: d.ObjectID, VersionID: d.VersionID, Path: route.Path, Params: params},
		Document: d.Document, Components: map[string]any{}, Data: map[string]any{}, DataSources: map[string]any{},
		Diagnostics: d.Diagnostics,
	}, nil
}

// GetDocument возвращает опубликованный в окружении документ.
func GetDocument(ctx context.Context, q *store.Queries, a Access, id uuid.UUID) (Document, error) {
	if a.Draft {
		return draftDocument(ctx, q, a, id)
	}
	d, err := q.GetDeliveredDocument(ctx, store.GetDeliveredDocumentParams{EnvironmentID: a.EnvironmentID, ObjectID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, notFound(fmt.Sprintf("Документ %s не опубликован в окружении %q", id, a.Environment))
	}
	if err != nil {
		return Document{}, err
	}
	return Document{ObjectID: d.ID, VersionID: d.VersionID, Path: d.Path, Document: d.Body}, nil
}

// GetRoutes — таблица маршрутов окружения (08 §5.2).
func GetRoutes(ctx context.Context, q *store.Queries, a Access) ([]publishing.Route, error) {
	if !a.Draft {
		return publishing.ListRoutes(ctx, q, a.ProjectID, a.Environment)
	}
	rows, err := q.ListDraftRoutes(ctx, store.ListDraftRoutesParams{ChangesetID: a.ChangesetID, ProjectID: a.ProjectID})
	if err != nil {
		return nil, err
	}
	out := make([]publishing.Route, len(rows))
	for i, r := range rows {
		out[i] = publishing.Route{Path: r.Path, ObjectID: r.ObjectID, VersionID: r.VersionID}
	}
	return out, nil
}

// draftDocument — документ чернового режима: рабочая версия в Change Set или head (API-040)
// с диагностикой валидации (API-042).
func draftDocument(ctx context.Context, q *store.Queries, a Access, id uuid.UUID) (Document, error) {
	d, err := changes.GetDocument(ctx, q, a.ProjectID, id, a.ChangesetID)
	if err != nil {
		return Document{}, err
	}
	var body any
	_ = json.Unmarshal(d.Body, &body) // тело версии записывает сервер
	diagnostics := ir.ValidateDocument(body).Diagnostics
	if diagnostics == nil {
		diagnostics = []ir.Diagnostic{}
	}
	return Document{ObjectID: d.ID, VersionID: d.VersionID, Path: d.Path, Document: d.Body, Diagnostics: diagnostics}, nil
}
