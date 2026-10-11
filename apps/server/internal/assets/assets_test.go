package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

type memoryStorage struct {
	data                       map[string][]byte
	size                       int64
	failOpen, failPut, failURL bool
	puts                       int
}

func (m *memoryStorage) UploadURL(_ context.Context, k string) (string, error) {
	if m.failURL {
		return "", errors.New("sign")
	}
	return "https://storage.invalid/" + k, nil
}
func (m *memoryStorage) Open(_ context.Context, k string) (io.ReadCloser, int64, error) {
	if m.failOpen {
		return nil, 0, errors.New("open")
	}
	b, ok := m.data[k]
	if !ok {
		return nil, 0, errors.New("missing")
	}
	n := int64(len(b))
	if m.size != 0 {
		n = m.size
	}
	return io.NopCloser(bytes.NewReader(b)), n, nil
}
func (m *memoryStorage) Put(_ context.Context, k string, r io.Reader, _ int64, _ string) error {
	if m.failPut {
		return errors.New("put")
	}
	b, err := io.ReadAll(r)
	m.data[k] = b
	m.puts++
	return err
}
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	im.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func begin(t *testing.T, e *cmstest.Env, s *Service, m *memoryStorage, raw []byte, mime string) Upload {
	t.Helper()
	hash := sha256.Sum256(raw)
	var u Upload
	e.Must(e.Admin, "create-asset-upload", Create{Filename: "file", MIME: mime, Size: int64(len(raw)), SHA256: hex.EncodeToString(hash[:])}, &u)
	var k string
	if err := e.Pool.QueryRow(context.Background(), `SELECT storage_key FROM asset_uploads WHERE id=$1`, u.AssetID).Scan(&k); err != nil {
		t.Fatal(err)
	}
	m.data[k] = raw
	return u
}
func TestCNT041LimitsAndInput(t *testing.T) {
	for _, mime := range []string{"image/jpeg", "image/png", "image/webp", "image/avif", "image/gif", "image/svg+xml", "video/mp4", "video/webm", "application/pdf"} {
		p := Create{Filename: "f", MIME: mime, Size: Limit(mime), SHA256: strings.Repeat("a", 64)}
		if validate(p) != nil {
			t.Fatal(mime)
		}
		p.Size++
		if validate(p) == nil {
			t.Fatal("limit", mime)
		}
		p.Size = 0
		if validate(p) == nil {
			t.Fatal("zero")
		}
	}
	for _, p := range []Create{{Filename: "", MIME: "image/png", Size: 1, SHA256: strings.Repeat("a", 64)}, {Filename: "f", MIME: "text/html", Size: 1, SHA256: strings.Repeat("a", 64)}, {Filename: "f", MIME: "image/png", Size: 1, SHA256: strings.Repeat("A", 64)}, {Filename: "f", MIME: "image/png", Size: 1, SHA256: "xx"}} {
		if validate(p) == nil {
			t.Fatal(p)
		}
	}
}
func TestCNT042SVGAllowlist(t *testing.T) {
	raw := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="20"><script>alert(1)</script><foreignObject><div>bad</div></foreignObject><g onload="bad()" style="fill:url(https://x)"><use href="https://x"/><use href="#safe"/><rect fill="url(#g)"/><rect fill="url(https://x)"/><text>&lt;safe&gt;</text></g></svg>`
	b, err := sanitizeSVG([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, unsafe := range []string{"script", "foreignObject", "bad", "onload", "style", "https"} {
		if strings.Contains(text, unsafe) {
			t.Fatal(text)
		}
	}
	if !strings.Contains(text, `href="#safe"`) || !strings.Contains(text, `fill="url(#g)"`) {
		t.Fatal(text)
	}
	w, h := svgDimensions(b)
	if w != 100 || h != 20 {
		t.Fatal(w, h)
	}
	for _, bad := range []string{"", `<svg>`, `<html/>`, `<svg/><svg/>`, `<svg xmlns="urn:evil"/>`, `<!DOCTYPE svg><svg/>`, `<svg id="a" id="b"/>`, `<svg/>tail`, strings.Repeat("<g>", 130), `<svg>` + strings.Repeat("<g>", 130)} {
		if _, err := sanitizeSVG([]byte(bad)); err == nil {
			t.Fatal(bad)
		}
	}
	for _, valid := range []string{`<?xml version="1.0"?><svg/>`, `<svg><x:svg xmlns:x="urn:evil"><script/></x:svg><path d="M0 0"/></svg>`, `<svg><!-- comment --><rect fill="url(#bad other)"/><use href="data:text/html,bad"/><path stroke="\\bad"/></svg>`} {
		if _, err := sanitizeSVG([]byte(valid)); err != nil {
			t.Fatal(valid, err)
		}
	}
}
func FuzzCNT042SVG(f *testing.F) {
	for _, s := range []string{`<svg/>`, `<svg><script/></svg>`, `<!DOCTYPE svg>`, "\x00"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			return
		}
		clean, err := sanitizeSVG(b)
		if err == nil {
			if _, err := sanitizeSVG(clean); err != nil {
				t.Fatal(err)
			}
		}
	})
}
func TestCNT040ProcessingAndDedup(t *testing.T) {
	e := cmstest.New(t)
	m := &memoryStorage{data: map[string][]byte{}}
	s := &Service{Pool: e.Pool, Storage: m}
	s.Register(e.Bus)
	for _, v := range []struct {
		mime string
		raw  []byte
	}{{"image/png", pngBytes(t, 640, 100)}, {"image/png", pngBytes(t, 20, 640)}, {"image/svg+xml", []byte(`<svg width="10" height="20"><script>bad</script><rect width="10"/></svg>`)}, {"application/pdf", []byte("%PDF-1.7\nfile")}} {
		u := begin(t, e, s, m, v.raw, v.mime)
		e.Must(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil)
		if err := s.Process(context.Background(), ProcessArgs{u.AssetID, e.Admin.ProjectID}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(context.Background(), e.Admin, u.AssetID)
		if err != nil || got.Status != "ready" || got.FileHash == nil {
			t.Fatal(got, err)
		}
		n := m.puts
		if err := s.Process(context.Background(), ProcessArgs{u.AssetID, e.Admin.ProjectID}); err != nil || m.puts != n {
			t.Fatal("retry", err)
		}
		next := begin(t, e, s, m, v.raw, v.mime)
		e.Must(e.Admin, "complete-asset-upload", Ref{next.AssetID}, nil)
		if err := s.Process(context.Background(), ProcessArgs{next.AssetID, e.Admin.ProjectID}); err != nil || m.puts != n {
			t.Fatal("dedup", err)
		}
		e.Must(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil)
		var count int
		e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM river_job WHERE kind='asset_process' AND args->>'uploadId'=$1`, u.AssetID.String()).Scan(&count)
		if count != 1 {
			t.Fatal("duplicate job", count)
		}
	}
}
func TestCNT042RejectionRetryAndScopes(t *testing.T) {
	e := cmstest.New(t)
	m := &memoryStorage{data: map[string][]byte{}}
	s := &Service{Pool: e.Pool, Storage: m}
	s.Register(e.Bus)
	ctx := context.Background()
	raw := pngBytes(t, 2, 2)
	for _, v := range []struct{ mutation, code string }{{"size", "ASSET_SIZE_MISMATCH"}, {"hash", "ASSET_HASH_MISMATCH"}, {"mime", "ASSET_MIME_MISMATCH"}, {"image", "ASSET_INVALID_IMAGE"}, {"svg", "ASSET_INVALID_SVG"}, {"stream", "ASSET_SIZE_MISMATCH"}} {
		b := raw
		mime := "image/png"
		if v.mutation == "image" {
			b = []byte("\x89PNG\r\n\x1a\ninvalid")
		}
		if v.mutation == "svg" {
			b = []byte(`<svg>`)
			mime = "image/svg+xml"
		}
		if v.mutation == "mime" {
			mime = "image/jpeg"
		}
		u := begin(t, e, s, m, b, mime)
		e.Must(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil)
		switch v.mutation {
		case "size":
			m.size = int64(len(b)) + 1
		case "hash":
			e.Exec(`UPDATE asset_uploads SET source_sha256=$2 WHERE id=$1`, u.AssetID, make([]byte, 32))
		case "stream":
			m.size = int64(len(b))
			for k := range m.data {
				if strings.HasSuffix(k, u.AssetID.String()) {
					m.data[k] = append(b, 0)
				}
			}
		}
		if err := s.Process(ctx, ProcessArgs{u.AssetID, e.Admin.ProjectID}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, e.Admin, u.AssetID)
		if err != nil || got.Status != "rejected" || got.ErrorCode == nil || *got.ErrorCode != v.code {
			t.Fatal(got, err)
		}
		m.size = 0
	}
	u := begin(t, e, s, m, raw, "image/png")
	other := e.Human("other", auth.AssetWrite)
	reader := e.Human("reader", auth.ContentRead)
	if _, err := s.Get(ctx, other, u.AssetID); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, reader, u.AssetID); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	if cmstest.Code(e.Do(other, "complete-asset-upload", Ref{u.AssetID}, nil)) != "NOT_FOUND" {
		t.Fatal("ownership")
	}
	e.Exec(`UPDATE asset_uploads SET expires_at=now()-interval '1 second' WHERE id=$1`, u.AssetID)
	if cmstest.Code(e.Do(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil)) != "ASSET_UPLOAD_EXPIRED" {
		t.Fatal("expiry")
	}
	u = begin(t, e, s, m, raw, "image/png")
	e.Must(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil)
	for _, which := range []string{"open", "put"} {
		m.failOpen = which == "open"
		m.failPut = which == "put"
		if s.Process(ctx, ProcessArgs{u.AssetID, e.Admin.ProjectID}) == nil {
			t.Fatal("retry error")
		}
		got, _ := s.Get(ctx, e.Admin, u.AssetID)
		if got.Status != "processing" {
			t.Fatal(got)
		}
	}
	m.failOpen = false
	m.failPut = false
	if err := s.Process(ctx, ProcessArgs{u.AssetID, e.Admin.ProjectID}); err != nil {
		t.Fatal(err)
	}
	if err := s.Process(ctx, ProcessArgs{uuid.New(), e.Admin.ProjectID}); err != nil {
		t.Fatal(err)
	}
	m.failURL = true
	if e.Do(e.Admin, "create-asset-upload", Create{Filename: "f", MIME: "image/png", Size: 1, SHA256: strings.Repeat("a", 64)}, nil) == nil {
		t.Fatal("url error")
	}
	s.Storage = nil
	if cmstest.Code(e.Do(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil)) != "ASSET_STORAGE_UNAVAILABLE" {
		t.Fatal("storage")
	}
	if s.Process(ctx, ProcessArgs{}) == nil {
		t.Fatal("storage worker")
	}
	// Missing queue rolls back processing and the idempotency response.
	m.failURL = false
	s.Storage = m
	u = begin(t, e, s, m, raw, "image/png")
	bus := commandbus.New(e.Pool)
	s.Register(bus)
	p := []byte(`{"assetId":"` + u.AssetID.String() + `"}`)
	if _, err := bus.Dispatch(ctx, e.Admin, commandbus.Request{Name: "complete-asset-upload", IdempotencyKey: "no-queue", Payload: p}); err == nil {
		t.Fatal("queue")
	}
	got, _ := s.Get(ctx, e.Admin, u.AssetID)
	if got.Status != "pending" {
		t.Fatal(got)
	}
}
