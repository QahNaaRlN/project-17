package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/deliverycache"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// PublishedCacheControl — кэширование опубликованных ответов (API-033).
const PublishedCacheControl = "public, s-maxage=300, stale-while-revalidate=60, stale-if-error=86400"

// MaxDeliveryPath — предел длины пути в /page?path=.
const MaxDeliveryPath = 2048

type accessKey struct{}

func accessFrom(ctx context.Context) delivery.Access {
	a, _ := ctx.Value(accessKey{}).(delivery.Access)
	return a
}

// deliveryRoutes — Delivery API (08 §5): /delivery/v1/{project}/{env}/….
func deliveryRoutes(d Deps) func(chi.Router) {
	return func(r chi.Router) {
		r.Use(authenticateDelivery(d))
		r.Get("/asset/{id}/image", imageModelHandler(d))
		r.Post("/telemetry", telemetryHandler(d))
		r.Get("/page", func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Query().Get("path")
			if !strings.HasPrefix(path, "/") || len(path) > MaxDeliveryPath {
				writeError(w, r, d.Log, badParam("path", fmt.Sprintf("путь страницы, начинающийся с «/», до %d символов", MaxDeliveryPath)))
				return
			}
			if path != "/" {
				path = strings.TrimSuffix(path, "/")
			}
			a := accessFrom(r.Context())
			locale := r.URL.Query().Get("locale")
			// Каждая публикация меняет тег routes, поэтому его версии до чтения БД достаточно (deliverycache).
			cached(w, r, d, "page", path, locale, []string{surrogate(a, "routes")}, func() (any, []string, error) {
				tx, err := d.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
				if err != nil {
					return nil, nil, err
				}
				defer tx.Rollback(r.Context())
				page, err := delivery.GetPage(r.Context(), store.New(tx), a, path, locale)
				if err == nil {
					err = tx.Commit(r.Context())
				}
				// Страница зависит и от объекта, и от таблицы маршрутов (смена маршрута меняет ответ).
				keys := []string{surrogate(a, page.Page.ObjectID.String()), surrogate(a, "routes")}
				if page.ContentContext != nil {
					keys = append(keys, surrogate(a, "manifest"))
					for id := range page.ContentContext.Versions {
						keys = append(keys, surrogate(a, id))
					}
				}
				return page, keys, err
			})
		})
		r.Get("/document/{id}", func(w http.ResponseWriter, r *http.Request) {
			id, ok := uuidParam(w, r, d, "id")
			if !ok {
				return
			}
			a := accessFrom(r.Context())
			keys := []string{surrogate(a, id.String())}
			cached(w, r, d, "document", id.String(), "", keys, func() (any, []string, error) {
				doc, err := delivery.GetDocument(r.Context(), store.New(d.Pool), a, id)
				return doc, keys, err
			})
		})

		for _, kind := range []string{"entity", "asset"} {
			r.Get("/"+kind+"/{id}", func(w http.ResponseWriter, r *http.Request) {
				id, ok := uuidParam(w, r, d, "id")
				if !ok {
					return
				}
				a := accessFrom(r.Context())
				var cs *uuid.UUID
				if a.Draft {
					cs = a.ChangesetID
				}
				keys := []string{surrogate(a, id.String()), surrogate(a, "manifest")}
				cached(w, r, d, kind, id.String(), "", keys, func() (any, []string, error) {
					value, err := changes.GetContent(r.Context(), store.New(d.Pool), a.ProjectID, id, a.Environment, cs, a.Draft && cs == nil)
					if err == nil && value.Kind != kind {
						err = commandbus.NewError(404, "NOT_FOUND", "Контент не найден", id.String())
					}
					return value, keys, err
				})
			})
		}
		r.Get("/routes", func(w http.ResponseWriter, r *http.Request) {
			a := accessFrom(r.Context())
			keys := []string{surrogate(a, "routes")}
			cached(w, r, d, "routes", "", "", keys, func() (any, []string, error) {
				routes, err := delivery.GetRoutes(r.Context(), store.New(d.Pool), a)
				return map[string]any{"items": routes}, keys, err
			})
		})
	}
}

// surrogate — Surrogate-Key объекта в окружении ключа (совпадает с ключами purge после публикации).
func surrogate(a delivery.Access, object string) string {
	return jobs.SurrogateKey(a.ProjectSlug, a.Environment, object)
}

// deliver отдаёт опубликованный ответ с ETag, Cache-Control и Surrogate-Key (API-033)
// и отвечает 304 на совпавший If-None-Match.
func deliver(w http.ResponseWriter, r *http.Request, d Deps, v any, err error, surrogateKeys ...string) {
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		writeError(w, r, d.Log, err)
		return
	}
	if accessFrom(r.Context()).Draft {
		// Черновые ответы не кэшируются (API-040).
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, v)
		return
	}
	body, etag, err := encodePublished(v)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writePublished(w, r, body, etag, surrogateKeys)
}

// cached отдаёт published-ответ через кэш на origin (STU-523…529); черновики и режим без
// Redis идут мимо кэша. pre — теги, известные до чтения БД; load читает БД и возвращает
// значение с полным набором Surrogate-Key.
func cached(w http.ResponseWriter, r *http.Request, d Deps, kind, key, locale string, pre []string, load func() (any, []string, error)) {
	a := accessFrom(r.Context())
	if a.Draft || d.Cache == nil {
		cacheResult(r, kind, "bypass")
		v, keys, err := load()
		deliver(w, r, d, v, err, keys...)
		return
	}
	ctx := r.Context()
	entryKey := deliverycache.Key(a.ProjectSlug, a.Environment, kind, key, locale)
	if e := d.Cache.Get(ctx, entryKey); e != nil {
		w.Header().Set("X-CMS-Cache", "hit")
		cacheResult(r, kind, "hit")
		writePublished(w, r, e.Body, e.ETag, e.SurrogateKeys)
		return
	}
	versions, ok := d.Cache.Versions(ctx, pre)
	v, keys, err := load()
	if err != nil {
		cacheResult(r, kind, "error")
		deliver(w, r, d, nil, err)
		return
	}
	body, etag, err := encodePublished(v)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	status := "bypass"
	if ok && d.Cache.Put(ctx, entryKey, deliverycache.Entry{Body: body, ETag: etag, SurrogateKeys: keys}, versions) {
		status = "miss"
	}
	w.Header().Set("X-CMS-Cache", status)
	cacheResult(r, kind, status)
	writePublished(w, r, body, etag, keys)
}

// encodePublished сериализует ответ; ETag — хэш тела.
func encodePublished(v any) ([]byte, string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), `"` + base64.RawURLEncoding.EncodeToString(sum[:18]) + `"`, nil
}

// writePublished пишет ответ с заголовками API-033 и отвечает 304 на совпавший If-None-Match.
func writePublished(w http.ResponseWriter, r *http.Request, body []byte, etag string, surrogateKeys []string) {
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", PublishedCacheControl)
	h.Set("Surrogate-Key", strings.Join(surrogateKeys, " "))
	if ifNoneMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// limited учитывает запрос и при превышении лимита отвечает 429 RATE_LIMITED (STU-532).
func limited(w http.ResponseWriter, r *http.Request, d Deps, subject, class string, limit int) bool {
	ok, wait := d.Limiter.Allow(r.Context(), subject, class, limit)
	if ok {
		return false
	}
	seconds := int((wait + time.Second - 1) / time.Second)
	w.Header().Set("Retry-After", strconv.Itoa(max(seconds, 1)))
	w.Header().Set("Cache-Control", "no-store")
	writeError(w, r, d.Log, commandbus.NewError(http.StatusTooManyRequests, "RATE_LIMITED", "Слишком много запросов",
		fmt.Sprintf("Лимит — %d в секунду; повторите после Retry-After", limit)))
	return true
}

func ifNoneMatch(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		c := strings.TrimSpace(candidate)
		if c == "*" || strings.TrimPrefix(c, "W/") == etag {
			return true
		}
	}
	return false
}

// authenticateDelivery проверяет ключ доставки и его соответствие проекту и окружению пути.
func authenticateDelivery(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			authorization := r.Header.Get("Authorization")
			authenticate := delivery.Authenticate
			if strings.HasPrefix(authorization, "Preview ") {
				authenticate = delivery.AuthenticatePreview
			}
			a, err := authenticate(r.Context(), store.New(d.Pool), authorization)
			if errors.Is(err, delivery.ErrUnauthenticated) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="cms-delivery", Preview realm="cms-delivery"`)
				writeError(w, r, d.Log, commandbus.NewError(http.StatusUnauthorized, "UNAUTHENTICATED",
					"Требуется ключ доставки или preview-токен",
					"Передайте действующий ключ cms_pub_… (Authorization: Bearer) или preview-токен (Authorization: Preview)"))
				return
			}
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			// A PR deployment runs against its base environment; only its preview
			// token may read the PR environment's drafts there (STU-322).
			base := a.Draft && a.BaseEnvironment != "" && chi.URLParam(r, "env") == a.BaseEnvironment
			if chi.URLParam(r, "project") != a.ProjectSlug || chi.URLParam(r, "env") != a.Environment && !base {
				writeError(w, r, d.Log, commandbus.NewError(http.StatusForbidden, "FORBIDDEN", "Ключ выдан для другого окружения",
					fmt.Sprintf("Ключ действует для %s/%s", a.ProjectSlug, a.Environment)))
				return
			}
			if err := checkChangeset(r, a); err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			limit, class := d.Limits.Delivery, "delivery"
			if a.Draft {
				limit, class = d.Limits.Preview, "preview"
			}
			if limited(w, r, d, a.Subject, class, limit) {
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), accessKey{}, a)))
		})
	}
}

// checkChangeset сверяет параметр changesetId с Change Set preview-токена (08 §5.3):
// черновик читается только по токену, выпущенному для этого Change Set.
func checkChangeset(r *http.Request, a delivery.Access) error {
	param := r.URL.Query().Get("changesetId")
	if param == "" {
		return nil
	}
	if !a.Draft {
		return badParam("changesetId", "только с preview-токеном (Authorization: Preview)")
	}
	if a.ChangesetID == nil || a.ChangesetID.String() != param {
		return commandbus.NewError(http.StatusForbidden, "FORBIDDEN", "Токен выдан для другого Change Set",
			"Параметр changesetId должен совпадать с Change Set preview-токена")
	}
	return nil
}

// cacheRequests — STU-599: Delivery cache outcomes by kind; the hit ratio is computed downstream.
var cacheRequests, _ = telemetry.Meter().Int64Counter("cms.delivery.cache.requests",
	metric.WithDescription("Delivery origin cache outcomes"))

func cacheResult(r *http.Request, kind, result string) {
	cacheRequests.Add(r.Context(), 1, metric.WithAttributes(attribute.String("cms.delivery.kind", kind), attribute.String("cms.cache.result", result)))
}
