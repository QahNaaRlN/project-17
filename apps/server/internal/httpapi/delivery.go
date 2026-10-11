package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
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
			page, err := delivery.GetPage(r.Context(), store.New(d.Pool), a, path)
			// Страница зависит и от объекта, и от таблицы маршрутов (смена маршрута меняет ответ).
			deliver(w, r, d, page, err, surrogate(a, page.Page.ObjectID.String()), surrogate(a, "routes"))
		})
		r.Get("/document/{id}", func(w http.ResponseWriter, r *http.Request) {
			id, ok := uuidParam(w, r, d, "id")
			if !ok {
				return
			}
			a := accessFrom(r.Context())
			doc, err := delivery.GetDocument(r.Context(), store.New(d.Pool), a, id)
			deliver(w, r, d, doc, err, surrogate(a, id.String()))
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
				value, err := changes.GetContent(r.Context(), store.New(d.Pool), a.ProjectID, id, a.Environment, cs, a.Draft && cs == nil)
				if err == nil && value.Kind != kind {
					err = commandbus.NewError(404, "NOT_FOUND", "Контент не найден", id.String())
				}
				deliver(w, r, d, value, err, surrogate(a, id.String()), surrogate(a, "manifest"))
			})
		}
		r.Get("/routes", func(w http.ResponseWriter, r *http.Request) {
			a := accessFrom(r.Context())
			routes, err := delivery.GetRoutes(r.Context(), store.New(d.Pool), a)
			keys := []string{surrogate(a, "routes")}
			deliver(w, r, d, map[string]any{"items": routes}, err, keys...)
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
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	sum := sha256.Sum256(buf.Bytes())
	etag := `"` + base64.RawURLEncoding.EncodeToString(sum[:18]) + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", PublishedCacheControl)
	h.Set("Surrogate-Key", strings.Join(surrogateKeys, " "))
	if ifNoneMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json")
	_, _ = w.Write(buf.Bytes())
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
			if chi.URLParam(r, "project") != a.ProjectSlug || chi.URLParam(r, "env") != a.Environment {
				writeError(w, r, d.Log, commandbus.NewError(http.StatusForbidden, "FORBIDDEN", "Ключ выдан для другого окружения",
					fmt.Sprintf("Ключ действует для %s/%s", a.ProjectSlug, a.Environment)))
				return
			}
			if err := checkChangeset(r, a); err != nil {
				writeError(w, r, d.Log, err)
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
