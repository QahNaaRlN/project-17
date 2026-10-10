package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
)

// publishedSite публикует главную и шаблон товара в staging и выдаёт ключ доставки.
func publishedSite(t *testing.T, e env) (key, product string) {
	t.Helper()
	e.command(t, "set-approval-policy", `{"low":0,"medium":0,"high":0}`)
	cs := e.command(t, "create-changeset", `{"title":"site"}`)["id"].(string)
	res := e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/","root":{"id":"n_root","type":"Box"}}},{"type":"document.create","payload":{"kind":"page","path":"/products/:slug","root":{"id":"n_root","type":"Box"}}}]}`)
	product = res["operations"].([]any)[1].(map[string]any)["target"].(string)
	e.command(t, "submit-changeset", `{"changesetId":"`+cs+`","expectedSeq":2}`)
	e.command(t, "publish", `{"changesetId":"`+cs+`","environment":"staging"}`)
	key = e.command(t, "create-delivery-key", `{"environment":"staging","name":"витрина"}`)["key"].(string)
	return key, product
}

func bearer(key string) map[string]string { return map[string]string{"Authorization": "Bearer " + key} }

func TestDeliveryPage(t *testing.T) {
	e := setup(t)
	key, product := publishedSite(t, e)

	resp, body := e.do(t, "GET", "/delivery/v1/store/staging/page?path=/products/shirt/", "", bearer(key))
	if resp.StatusCode != 200 {
		t.Fatalf("%d %v", resp.StatusCode, body)
	}
	page := body["page"].(map[string]any)
	if page["objectId"] != product || page["path"] != "/products/:slug" || page["params"].(map[string]any)["slug"] != "shirt" {
		t.Errorf("страница: %v", page)
	}
	if body["document"].(map[string]any)["root"] != "n_root" || body["components"] == nil {
		t.Errorf("тело: %v", body)
	}
	etag := resp.Header.Get("ETag")
	if !strings.HasPrefix(etag, `"`) || resp.Header.Get("Cache-Control") != httpapi.PublishedCacheControl || resp.Header.Get("Surrogate-Key") != product {
		t.Errorf("заголовки: %v", resp.Header)
	}
	// Тот же ответ с If-None-Match — 304 без тела.
	h := bearer(key)
	h["If-None-Match"] = `W/"other", ` + etag
	resp, body = e.do(t, "GET", "/delivery/v1/store/staging/page?path=/products/shirt", "", h)
	if resp.StatusCode != http.StatusNotModified || body != nil || resp.Header.Get("ETag") != etag {
		t.Errorf("304: %d %v", resp.StatusCode, body)
	}
	h["If-None-Match"] = `"stale"`
	if resp, _ = e.do(t, "GET", "/delivery/v1/store/staging/page?path=/products/shirt", "", h); resp.StatusCode != 200 {
		t.Errorf("устаревший ETag: %d", resp.StatusCode)
	}

	resp, body = e.do(t, "GET", "/delivery/v1/store/staging/page?path=/missing", "", bearer(key))
	expectProblem(t, resp, body, 404, "NOT_FOUND")
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("ошибки не кэшируются: %v", resp.Header)
	}
	for _, path := range []string{"", "relative", "/" + strings.Repeat("a", httpapi.MaxDeliveryPath)} {
		resp, body = e.do(t, "GET", "/delivery/v1/store/staging/page?path="+path, "", bearer(key))
		expectProblem(t, resp, body, 400, "PARAM_INVALID")
	}
}

func TestDeliveryDocumentAndRoutes(t *testing.T) {
	e := setup(t)
	key, product := publishedSite(t, e)
	resp, body := e.do(t, "GET", "/delivery/v1/store/staging/document/"+product, "", bearer(key))
	if resp.StatusCode != 200 || body["objectId"] != product || body["path"] != "/products/:slug" || resp.Header.Get("Surrogate-Key") != product {
		t.Errorf("документ: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(t, "GET", "/delivery/v1/store/staging/document/0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11", "", bearer(key))
	expectProblem(t, resp, body, 404, "NOT_FOUND")
	resp, body = e.do(t, "GET", "/delivery/v1/store/staging/document/x", "", bearer(key))
	expectProblem(t, resp, body, 400, "PARAM_INVALID")

	resp, body = e.do(t, "GET", "/delivery/v1/store/staging/routes", "", bearer(key))
	items := body["items"].([]any)
	if resp.StatusCode != 200 || len(items) != 2 || items[0].(map[string]any)["path"] != "/" || !strings.HasPrefix(resp.Header.Get("Surrogate-Key"), "routes ") {
		t.Errorf("маршруты: %d %v %v", resp.StatusCode, body, resp.Header)
	}
}

func TestDeliveryAuth(t *testing.T) {
	e := setup(t)
	key, _ := publishedSite(t, e)
	for name, h := range map[string]map[string]string{
		"без ключа":        nil,
		"сервисный токен":  {"Authorization": "Bearer " + e.token},
		"неизвестный ключ": bearer("cms_pub_unknown"),
	} {
		t.Run(name, func(t *testing.T) {
			resp, body := e.do(t, "GET", "/delivery/v1/store/staging/routes", "", h)
			expectProblem(t, resp, body, 401, "UNAUTHENTICATED")
			if resp.Header.Get("WWW-Authenticate") == "" {
				t.Error("WWW-Authenticate")
			}
		})
	}
	for _, path := range []string{"/delivery/v1/store/production/routes", "/delivery/v1/other/staging/routes"} {
		resp, body := e.do(t, "GET", path, "", bearer(key))
		expectProblem(t, resp, body, 403, "FORBIDDEN")
	}
}

func TestDeliveryKeysRoute(t *testing.T) {
	e := setup(t)
	e.command(t, "create-delivery-key", `{"environment":"production","name":"prod"}`)
	resp, body := e.do(t, "GET", "/api/v1/delivery-keys", "", e.authed(nil))
	items := body["items"].([]any)
	if resp.StatusCode != 200 || len(items) != 1 || items[0].(map[string]any)["key"] != nil {
		t.Errorf("ключи: %d %v", resp.StatusCode, body)
	}
	if _, err := e.pool.Exec(t.Context(), "UPDATE roles SET capabilities = ARRAY['content.read']"); err != nil {
		t.Fatal(err)
	}
	resp, body = e.do(t, "GET", "/api/v1/delivery-keys", "", e.authed(nil))
	expectProblem(t, resp, body, 403, "FORBIDDEN")
}
