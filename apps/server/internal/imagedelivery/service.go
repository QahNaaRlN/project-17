package imagedelivery

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type Service struct {
	Pool                     *pgxpool.Pool
	cfg                      Config
	key, proxyKey, proxySalt []byte
	Client                   *http.Client
	Now                      func() time.Time
}

func New(c Config) (*Service, error) {
	if _, e := base(c.PublicURL, true); e != nil {
		return nil, e
	}
	if _, e := base(c.ProxyURL, false); e != nil {
		return nil, e
	}
	if c.Bucket == "" || strings.ContainsAny(c.Bucket, "/:?#\\") {
		return nil, invalid("invalid bucket")
	}
	key, e := secret(c.Key, 32)
	if e != nil {
		return nil, e
	}
	pk, e := secret(c.ProxyKey, 32)
	if e != nil {
		return nil, e
	}
	salt, e := secret(c.ProxySalt, 16)
	if e != nil {
		return nil, e
	}
	return &Service{cfg: c, key: key, proxyKey: pk, proxySalt: salt, Now: time.Now, Client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func Unavailable() error {
	return commandbus.NewError(503, "ASSET_IMAGES_UNAVAILABLE", "Выдача изображений недоступна", "Настройте CMS_ASSET_* и CMS_IMGPROXY_*")
}
func denied() error {
	return commandbus.NewError(403, "ASSET_URL_INVALID", "URL изображения недействителен", "Параметры, подпись или срок недопустимы")
}
func missing() error {
	return commandbus.NewError(404, "ASSET_IMAGE_NOT_FOUND", "Изображение не найдено", "Версия или файл недоступны")
}

type Variant struct {
	Width int    `json:"width"`
	URL   string `json:"url"`
}
type Model struct {
	AssetID    uuid.UUID `json:"assetId"`
	VersionID  uuid.UUID `json:"versionId"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	Fit        string    `json:"fit"`
	Format     string    `json:"format"`
	FocalPoint Point     `json:"focalPoint"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Variants   []Variant `json:"variants"`
}
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type imageData struct {
	Hash          string `json:"fileHash"`
	MIME          string `json:"mimeType"`
	Managed       bool   `json:"managedFile"`
	Width, Height int
	Focal         *Point `json:"focalPoint"`
}
type resolved struct {
	content changes.Content
	data    imageData
	key     string
}

func (s *Service) resolve(ctx context.Context, a delivery.Access, id uuid.UUID) (resolved, error) {
	q := store.New(s.Pool)
	c, e := changes.GetContent(ctx, q, a.ProjectID, id, a.Environment, a.ChangesetID, a.Draft && a.ChangesetID == nil)
	if e != nil {
		return resolved{}, e
	}
	var d imageData
	if json.Unmarshal(c.Data, &d) != nil || c.Kind != "asset" || c.Deleted || !d.Managed || !strings.HasPrefix(d.MIME, "image/") || d.Width <= 0 || d.Height <= 0 || int64(d.Width)*int64(d.Height) > 40000000 {
		return resolved{}, missing()
	}
	hash, e := hex.DecodeString(d.Hash)
	if e != nil || len(hash) != 32 || strings.ToLower(d.Hash) != d.Hash {
		return resolved{}, missing()
	}
	f, e := q.ReadyAssetFile(ctx, store.ReadyAssetFileParams{ProjectID: a.ProjectID, Sha256: hash})
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return resolved{}, missing()
		}
		return resolved{}, e
	}
	canonical := "projects/" + a.ProjectID.String() + "/assets/" + d.Hash
	if f.StorageKey != canonical || f.MimeType != d.MIME || f.Width == nil || f.Height == nil || int(*f.Width) != d.Width || int(*f.Height) != d.Height {
		return resolved{}, missing()
	}
	if d.Focal == nil {
		d.Focal = &Point{.5, .5}
	}
	if math.IsNaN(d.Focal.X) || math.IsNaN(d.Focal.Y) || d.Focal.X < 0 || d.Focal.X > 1 || d.Focal.Y < 0 || d.Focal.Y > 1 {
		return resolved{}, missing()
	}
	return resolved{c, d, canonical}, nil
}
func assetPath(env string, id uuid.UUID, hash string) string {
	return "/assets/v1/" + url.PathEscape(env) + "/" + id.String() + "/" + hash
}
func (s *Service) Mint(ctx context.Context, a delivery.Access, id uuid.UUID, o Options) (Model, error) {
	if e := o.Validate(); e != nil {
		return Model{}, e
	}
	r, e := s.resolve(ctx, a, id)
	if e != nil {
		return Model{}, e
	}
	expires := s.Now().Add(TTL).Truncate(time.Second)
	scope, pk := "published", ""
	if a.Draft {
		scope = "head"
		if a.ChangesetID != nil {
			scope = a.ChangesetID.String()
		}
		pk = a.PreviewKeyHash
		if a.ExpiresAt.Before(expires) {
			expires = a.ExpiresAt
		}
		if pk == "" || !expires.After(s.Now()) {
			return Model{}, denied()
		}
	}
	m := Model{AssetID: id, VersionID: r.content.VersionID, Width: r.data.Width, Height: r.data.Height, Fit: o.Fit, Format: o.Format, FocalPoint: *r.data.Focal, ExpiresAt: expires, Variants: []Variant{}}
	if o.Fit == "cover" {
		m.Width = o.Width
		m.Height = o.Height
	}
	for _, w := range Widths {
		h := 0
		actualHeight := int(math.Round(float64(w) * float64(r.data.Height) / float64(r.data.Width)))
		if o.Fit == "cover" {
			h = max(1, int(math.Round(float64(w)*float64(o.Height)/float64(o.Width))))
			actualHeight = h
		}
		if actualHeight > 2560 {
			continue
		}
		path := assetPath(a.Environment, id, r.data.Hash)
		q := url.Values{"project": {a.ProjectSlug}, "pid": {a.ProjectID.String()}, "eid": {a.EnvironmentID.String()}, "v": {r.content.VersionID.String()}, "rev": {fingerprint(r.content.Data)}, "scope": {scope}, "pk": {pk}, "exp": {strconv.FormatInt(expires.Unix(), 10)}, "w": {strconv.Itoa(w)}, "h": {strconv.Itoa(h)}, "fit": {o.Fit}, "fmt": {o.Format}}
		q.Set("sig", digest(s.key, nil, path+"?"+q.Encode()))
		m.Variants = append(m.Variants, Variant{w, strings.TrimRight(s.cfg.PublicURL, "/") + path + "?" + q.Encode()})
	}
	if len(m.Variants) == 0 {
		return Model{}, missing()
	}
	return m, nil
}

type Image struct {
	Body                     []byte
	MIME, ETag, SurrogateKey string
	Draft                    bool
	MaxAge                   int
}

// Fetch authenticates the entire capability before any storage or imgproxy access.
func (s *Service) Fetch(ctx context.Context, u *url.URL) (Image, error) {
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return Image{}, denied()
	}
	keys := []string{"project", "pid", "eid", "v", "rev", "scope", "pk", "exp", "w", "h", "fit", "fmt", "sig"}
	if len(q) != len(keys) {
		return Image{}, denied()
	}
	for _, k := range keys {
		if len(q[k]) != 1 {
			return Image{}, denied()
		}
	}
	sig := q.Get("sig")
	q.Del("sig")
	if !hmac.Equal([]byte(sig), []byte(digest(s.key, nil, u.EscapedPath()+"?"+q.Encode()))) {
		return Image{}, denied()
	}
	expiry, e := strconv.ParseInt(q.Get("exp"), 10, 64)
	if e != nil || expiry <= s.Now().Unix() || expiry > s.Now().Add(TTL).Unix() {
		return Image{}, denied()
	}
	parts := strings.Split(u.Path, "/")
	if len(parts) != 6 || parts[1] != "assets" || parts[2] != "v1" {
		return Image{}, denied()
	}
	return s.fetchVerified(ctx, u, q, expiry)
}
func (s *Service) fetchVerified(ctx context.Context, u *url.URL, q url.Values, expiry int64) (Image, error) {
	parts := strings.Split(u.Path, "/")
	id, e := uuid.Parse(parts[4])
	if e != nil {
		return Image{}, denied()
	}
	if u.EscapedPath() != assetPath(parts[3], id, parts[5]) {
		return Image{}, denied()
	}
	env, e := store.New(s.Pool).GetEnvironmentPreviewKey(ctx, store.GetEnvironmentPreviewKeyParams{Slug: q.Get("project"), Name: parts[3]})
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return Image{}, missing()
		}
		return Image{}, e
	}
	if q.Get("pid") != env.ProjectID.String() || q.Get("eid") != env.ID.String() {
		return Image{}, missing()
	}
	a := delivery.Access{ProjectID: env.ProjectID, ProjectSlug: env.ProjectSlug, EnvironmentID: env.ID, Environment: env.Name}
	scope := q.Get("scope")
	if scope != "published" {
		a.Draft = true
		if q.Get("pk") != fingerprint(env.PreviewKey) {
			return Image{}, denied()
		}
		if scope != "head" {
			cs, e := uuid.Parse(scope)
			if e != nil {
				return Image{}, denied()
			}
			a.ChangesetID = &cs
		}
	} else if q.Get("pk") != "" {
		return Image{}, denied()
	}
	w, e := strconv.Atoi(q.Get("w"))
	if e != nil {
		return Image{}, denied()
	}
	h, e := strconv.Atoi(q.Get("h"))
	if e != nil {
		return Image{}, denied()
	}
	o := Options{w, h, q.Get("fit"), q.Get("fmt")}
	if o.Validate() != nil {
		return Image{}, denied()
	}
	r, e := s.resolve(ctx, a, id)
	if e != nil {
		return Image{}, e
	}
	if r.data.Hash != parts[5] || r.content.VersionID.String() != q.Get("v") || fingerprint(r.content.Data) != q.Get("rev") {
		return Image{}, missing()
	}
	actualHeight := h
	if h == 0 {
		actualHeight = int(math.Round(float64(w) * float64(r.data.Height) / float64(r.data.Width)))
	}
	if actualHeight > 2560 {
		return Image{}, denied()
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.cfg.ProxyURL, "/")+s.ProxyPath(r.key, o, r.data.Focal.X, r.data.Focal.Y), nil)
	if e != nil {
		return Image{}, e
	}
	resp, e := s.Client.Do(req)
	if e != nil {
		return Image{}, upstream()
	}
	defer resp.Body.Close()
	mime := "image/" + o.Format
	if resp.StatusCode != 200 || strings.Split(resp.Header.Get("Content-Type"), ";")[0] != mime || resp.ContentLength > MaxBody {
		return Image{}, upstream()
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if e != nil || len(body) == 0 || len(body) > MaxBody {
		return Image{}, upstream()
	}
	return Image{Body: body, MIME: mime, ETag: `"` + fingerprint(body) + `"`, SurrogateKey: fmt.Sprintf("%s:%s:%s", a.ProjectSlug, a.Environment, id), Draft: a.Draft, MaxAge: min(300, int(expiry-s.Now().Unix()))}, nil
}
func upstream() error {
	return commandbus.NewError(502, "ASSET_IMAGE_UPSTREAM", "Обработка изображения недоступна", "Повторите запрос позже")
}
