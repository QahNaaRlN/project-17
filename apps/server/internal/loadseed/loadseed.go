// Package loadseed заполняет базу данными профиля нагрузки MVP (01 §5) для нагрузочных
// тестов NFR-001…007 (docs/load-tests.md, STU-542).
//
// Проекты создаются как при bootstrap, опубликованный контент загружается через COPY в
// том виде, который оставляет публикация: объект, зафиксированная версия, опубликованный
// указатель и маршрут в staging. Идентификаторы и тексты детерминированы: сценарии k6
// вычисляют ID сущностей и пути страниц по номеру, не читая список из файла.
package loadseed

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Config — объём данных; Profile — значения 01 §5.
type Config struct {
	Projects  int
	Entities  int // на проект
	Documents int // страниц на проект
	Editors   int // токенов виртуальных пользователей Studio в первом проекте
	Seed      uint64
	TokenTTL  time.Duration
}

// Profile — профиль нагрузки MVP (01 §5).
func Profile() Config {
	return Config{Projects: 20, Entities: 100_000, Documents: 10_000, Editors: 50, Seed: 1, TokenTTL: 8 * time.Hour}
}

// Project — проект с ключом доставки и токеном администратора.
type Project struct {
	Slug        string `json:"slug"`
	Index       int    `json:"index"`
	AdminToken  string `json:"adminToken"`
	DeliveryKey string `json:"deliveryKey"`
}

// Result — секреты и объёмы для сценариев k6 (perf/results/seed.private.json).
type Result struct {
	Config   Config    `json:"config"`
	Projects []Project `json:"projects"`
	Editors  []string  `json:"editors"`
}

// Manifest — manifest проектов нагрузки: схема Article для сущностей.
const Manifest = `{"manifestVersion":"1.0","app":{"id":"load","version":"1.0.0","framework":"react"},"irVersions":["1.0"],"breakpoints":{},"tokens":{},` +
	`"schemas":{"Article":{"version":1,"fields":{"title":{"type":"text"},"lead":{"type":"text"},"body":{"type":"text"}}}}}`

// Slug — slug проекта по номеру (с 1).
func Slug(project int) string { return fmt.Sprintf("load-%02d", project) }

// EntityID — ID сущности: проект и номер в последних 12 байтах (k6 строит его так же).
func EntityID(project, i int) uuid.UUID { return objectID(0, project, i) }

// DocumentID — ID страницы.
func DocumentID(project, i int) uuid.UUID { return objectID(1, project, i) }

// PagePath — путь страницы по номеру.
func PagePath(i int) string { return fmt.Sprintf("/p/%d", i) }

func objectID(kind, project, i int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-%04x-4000-8000-%04x%08x", kind, project, i))
}

func versionID(id uuid.UUID) uuid.UUID {
	v := id
	v[0] = 0xff
	return v
}

var words = strings.Fields("лён хлопок рубашка платье лето коллекция цвет размер доставка возврат скидка новинка ткань крой посадка уход стирка подарок сезон стиль")

func text(r *rand.Rand, n int) string {
	out := make([]string, n)
	for i := range out {
		out[i] = words[r.IntN(len(words))]
	}
	return strings.Join(out, " ")
}

// Document — страница IR из 20…300 узлов (распределение смещено к небольшим страницам).
func Document(r *rand.Rand) map[string]any {
	n := 20 + int(280*r.Float64()*r.Float64())
	nodes := map[string]any{}
	children := []string{}
	for len(nodes) < n-1 {
		section := fmt.Sprintf("n_s%d", len(children))
		heading, para := section+"_h", section+"_t"
		nodes[heading] = map[string]any{"id": heading, "type": "Heading", "props": map[string]any{"level": 2, "text": text(r, 4)}}
		nodes[para] = map[string]any{"id": para, "type": "Text", "props": map[string]any{"text": text(r, 30)}}
		nodes[section] = map[string]any{"id": section, "type": "Stack", "props": map[string]any{"direction": "vertical"}, "children": []string{heading, para}}
		children = append(children, section)
	}
	nodes["n_root"] = map[string]any{"id": "n_root", "type": "Stack", "props": map[string]any{"direction": "vertical"}, "children": children}
	return map[string]any{"irVersion": "1.0", "kind": "page", "root": "n_root", "nodes": nodes}
}

// Entity — тело сущности Article.
func Entity(r *rand.Rand) map[string]any {
	return map[string]any{"title": text(r, 5), "lead": text(r, 20), "body": text(r, 80)}
}

// Run создаёт проекты и загружает данные. log получает строки о ходе загрузки.
func Run(ctx context.Context, pool *pgxpool.Pool, cfg Config, log func(string, ...any)) (Result, error) {
	res := Result{Config: cfg}
	for p := 1; p <= cfg.Projects; p++ {
		project, editors, err := seedProject(ctx, pool, cfg, p)
		if err != nil {
			return res, fmt.Errorf("%s: %w", Slug(p), err)
		}
		res.Projects = append(res.Projects, project)
		res.Editors = append(res.Editors, editors...)
		log("seeded", "project", project.Slug, "entities", cfg.Entities, "documents", cfg.Documents)
	}
	return res, nil
}

func seedProject(ctx context.Context, pool *pgxpool.Pool, cfg Config, p int) (Project, []string, error) {
	slug := Slug(p)
	boot, err := projects.Bootstrap(ctx, pool, slug, "Нагрузка "+slug, cfg.TokenTTL)
	if err != nil {
		return Project{}, nil, err
	}
	out := Project{Slug: slug, Index: p, AdminToken: boot.Token}
	var editors []string
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		staging, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: boot.ProjectID, Name: "staging"})
		if err != nil {
			return err
		}
		if err := activateManifest(ctx, tx, boot.ProjectID, boot.ActorID); err != nil {
			return err
		}
		// Stages 8–9 need an application; load publications carry no heavy checks (STU-547).
		if _, err := tx.Exec(ctx, `INSERT INTO project_render_checks (project_id, enabled) VALUES ($1, false)
			ON CONFLICT (project_id) DO UPDATE SET enabled = false`, boot.ProjectID); err != nil {
			return err
		}
		publication := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO publications (id, project_id, environment_id, kind, actor_id, reason) VALUES ($1, $2, $3, 'publish', $4, 'load seed')`,
			publication, boot.ProjectID, staging.ID, boot.ActorID); err != nil {
			return err
		}
		if err := copyContent(ctx, tx, cfg, p, boot.ProjectID, boot.ActorID, staging.ID, publication); err != nil {
			return err
		}
		secret, hash := auth.NewToken(auth.DeliveryKeyPrefix)
		if _, err := q.CreateDeliveryKey(ctx, store.CreateDeliveryKeyParams{ID: uuid.Must(uuid.NewV7()), ProjectID: boot.ProjectID,
			EnvironmentID: staging.ID, Name: "load", TokenHash: hash, CreatedBy: boot.ActorID}); err != nil {
			return err
		}
		out.DeliveryKey = secret
		if p == 1 {
			editors, err = createEditors(ctx, q, cfg, boot.ProjectID)
		}
		return err
	})
	return out, editors, err
}

func activateManifest(ctx context.Context, tx pgx.Tx, projectID, actorID uuid.UUID) error {
	app, err := manifest.ParseJSON([]byte(Manifest))
	if err != nil {
		return err
	}
	hash, err := manifest.Hash(app)
	if err != nil {
		return err
	}
	q := store.New(tx)
	id := uuid.Must(uuid.NewV7())
	if err := q.InsertManifest(ctx, store.InsertManifestParams{ID: id, ProjectID: projectID, Hash: hash, AppVersion: "1.0.0", Body: []byte(Manifest), RegisteredBy: actorID}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE environments SET active_manifest_id = $1 WHERE project_id = $2`, id, projectID)
	return err
}

// createEditors — сервисные токены редакторов Studio (решение §11.6): права на контент
// и композицию без администрирования проекта.
func createEditors(ctx context.Context, q *store.Queries, cfg Config, projectID uuid.UUID) ([]string, error) {
	rights := []string{}
	for _, r := range auth.AllRights {
		if r != auth.CapabilityDeliver && r != auth.ChecksRun && r != auth.ProjectAdmin {
			rights = append(rights, string(r))
		}
	}
	role, err := q.CreateRole(ctx, store.CreateRoleParams{ID: uuid.Must(uuid.NewV7()), ProjectID: projectID, Name: "load-editor", Capabilities: rights})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, cfg.Editors)
	for i := 0; i < cfg.Editors; i++ {
		actor, err := q.CreateActor(ctx, store.CreateActorParams{ID: uuid.Must(uuid.NewV7()), Kind: string(auth.ActorService), ProjectID: &projectID, DisplayName: fmt.Sprintf("load-editor-%02d", i+1)})
		if err != nil {
			return nil, err
		}
		if err := q.BindRole(ctx, store.BindRoleParams{ProjectID: projectID, ActorID: actor.ID, RoleID: role.ID}); err != nil {
			return nil, err
		}
		secret, hash := auth.NewToken(auth.ServiceTokenPrefix)
		if _, err := q.CreateAPIToken(ctx, store.CreateAPITokenParams{ID: uuid.Must(uuid.NewV7()), ProjectID: projectID, ActorID: actor.ID,
			TokenHash: hash, Scopes: []string{auth.ScopeAll}, ExpiresAt: time.Now().Add(cfg.TokenTTL)}); err != nil {
			return nil, err
		}
		out = append(out, secret)
	}
	return out, nil
}

// copyContent загружает сущности и страницы одной транзакцией через COPY.
func copyContent(ctx context.Context, tx pgx.Tx, cfg Config, p int, projectID, actorID, envID, publication uuid.UUID) error {
	r := rand.New(rand.NewPCG(cfg.Seed, uint64(p)))
	now := time.Now()
	total := cfg.Entities + cfg.Documents
	objects := make([][]any, 0, total)
	versions := make([][]any, 0, total)
	pointers := make([][]any, 0, total)
	routes := make([][]any, 0, cfg.Documents)
	schema, irVersion := "Article", "1.0"
	add := func(id uuid.UUID, kind string, schemaName, docKind, path, ir *string, schemaVersion *int32, body map[string]any) {
		raw, _ := json.Marshal(body)
		hash := sha256.Sum256(raw)
		v := versionID(id)
		objects = append(objects, []any{id, projectID, kind, schemaName, docKind, path})
		versions = append(versions, []any{v, projectID, id, int32(1), "committed", schemaVersion, ir, path, raw, hash[:], actorID, now})
		pointers = append(pointers, []any{envID, id, v, publication})
		if path != nil {
			routes = append(routes, []any{envID, *path, id})
		}
	}
	one := int32(1)
	for i := 0; i < cfg.Entities; i++ {
		add(EntityID(p, i), "entity", &schema, nil, nil, nil, &one, Entity(r))
	}
	page := "page"
	for i := 0; i < cfg.Documents; i++ {
		path := PagePath(i)
		add(DocumentID(p, i), "document", nil, &page, &path, &irVersion, nil, Document(r))
	}
	for _, c := range []struct {
		table string
		cols  []string
		rows  [][]any
	}{
		{"objects", []string{"id", "project_id", "kind", "schema_name", "doc_kind", "head_path"}, objects},
		{"object_versions", []string{"id", "project_id", "object_id", "number", "state", "schema_version", "ir_version", "path", "body", "body_hash", "created_by", "committed_at"}, versions},
		{"published_pointers", []string{"environment_id", "object_id", "version_id", "publication_id"}, pointers},
		{"routes", []string{"environment_id", "path", "object_id"}, routes},
	} {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{c.table}, c.cols, pgx.CopyFromRows(c.rows)); err != nil {
			return fmt.Errorf("copy %s: %w", c.table, err)
		}
	}
	_, err := tx.Exec(ctx, `UPDATE objects o SET head_version_id = v.id FROM object_versions v
		WHERE v.object_id = o.id AND o.project_id = $1 AND v.project_id = $1`, projectID)
	return err
}
