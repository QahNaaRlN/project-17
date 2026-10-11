package assets

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/gif"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/gen2brain/avif"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/riverqueue/river"
)

// CNT-041/042: decoders, ISO brands and project narrowing use actual content.
func TestFormatsAndPolicy(t *testing.T) {
	e := cmstest.New(t)
	m := &memoryStorage{data: map[string][]byte{}}
	s := &Service{Pool: e.Pool, Storage: m}
	s.Register(e.Bus)
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for _, format := range []string{"jpeg", "gif", "avif", "avif-compatible"} {
		var b bytes.Buffer
		var err error
		switch format {
		case "jpeg":
			err = jpeg.Encode(&b, im, nil)
		case "gif":
			err = gif.Encode(&b, im, nil)
		case "avif", "avif-compatible":
			err = avif.Encode(&b, im)
		}
		if err != nil {
			t.Fatal(err)
		}
		mime := "image/" + format
		if strings.HasPrefix(format, "avif") {
			mime = "image/avif"
		}
		if format == "avif-compatible" {
			copy(b.Bytes()[8:12], []byte("mif1"))
		}
		u := begin(t, e, s, m, b.Bytes(), mime)
		e.Must(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil)
		if err := (&Worker{Service: s}).Work(context.Background(), &river.Job[ProcessArgs]{Args: ProcessArgs{u.AssetID, e.Admin.ProjectID}}); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Get(context.Background(), e.Admin, u.AssetID)
		if got.Status != "ready" {
			t.Fatal(format, got)
		}
	}
	raw := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'i', 'f', '1', 0, 0, 0, 0, 'a', 'v', 'i', 'f', 0, 0, 0, 0}
	if detect(raw) != "image/avif" {
		t.Fatal("compatible brand")
	}
	raw[0] = 255
	if detect(raw) == "image/avif" {
		t.Fatal("invalid box length")
	}
	e.Exec(`UPDATE projects SET settings='{"assets":{"mimeTypes":["application/pdf"]}}'`)
	if err := e.Do(e.Admin, "create-asset-upload", Create{Filename: "f", MIME: "image/png", Size: 1, SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}, nil); err == nil {
		t.Fatal("project policy")
	}
	e.Exec(`UPDATE projects SET settings='{}'`)
	u := begin(t, e, s, m, pngBytes(t, 1, 1), "image/png")
	e.Must(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil)
	e.Exec(`UPDATE projects SET settings='{"assets":{"mimeTypes":[]}}'`)
	if err := s.Process(context.Background(), ProcessArgs{u.AssetID, e.Admin.ProjectID}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(context.Background(), e.Admin, u.AssetID)
	if got.ErrorCode == nil || *got.ErrorCode != "ASSET_MIME_FORBIDDEN" {
		t.Fatal(got)
	}
}

func TestCNT042PixelBound(t *testing.T) {
	e := cmstest.New(t)
	m := &memoryStorage{data: map[string][]byte{}}
	s := &Service{Pool: e.Pool, Storage: m}
	s.Register(e.Bus)
	for _, width := range []uint32{4000, 4001} {
		raw := pngBytes(t, 1, 1)
		binary.BigEndian.PutUint32(raw[16:20], width)
		binary.BigEndian.PutUint32(raw[20:24], 10000)
		binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
		u := begin(t, e, s, m, raw, "image/png")
		e.Must(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil)
		if err := s.Process(context.Background(), ProcessArgs{u.AssetID, e.Admin.ProjectID}); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Get(context.Background(), e.Admin, u.AssetID)
		want := "ASSET_INVALID_IMAGE"
		if width == 4001 {
			want = "ASSET_IMAGE_TOO_LARGE"
		}
		if got.ErrorCode == nil || *got.ErrorCode != want {
			t.Fatal(got)
		}
	}
}
