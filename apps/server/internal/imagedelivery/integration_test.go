package imagedelivery

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gen2brain/avif"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/qahnaarln/project-17/apps/server/internal/assets"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// CNT-050/051: actual imgproxy reads a private S3 object, checks its signature and outputs all four formats.
func TestRealImgproxyPrivateS3(t *testing.T) {
	ctx := context.Background()
	netw, e := network.New(ctx)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = netw.Remove(ctx) })
	_, source, _, _ := runtime.Caller(0)
	var buildLog bytes.Buffer
	minioContainer, e := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{FromDockerfile: testcontainers.FromDockerfile{Context: filepath.Join(filepath.Dir(source), "..", "assets", "testdata", "minio"), Repo: "project17-minio-test", Tag: "2025-04-22", KeepImage: true, BuildLogWriter: &buildLog}, Networks: []string{netw.Name}, NetworkAliases: map[string][]string{netw.Name: {"minio"}}, ExposedPorts: []string{"9000/tcp"}, Env: map[string]string{"MINIO_ROOT_USER": "test-access", "MINIO_ROOT_PASSWORD": "test-secret"}, Cmd: []string{"server", "/data"}, WaitingFor: wait.ForHTTP("/minio/health/ready").WithPort("9000/tcp").WithStartupTimeout(time.Minute)}, Started: true})
	if e != nil {
		t.Fatalf("MinIO: %v\n%s", e, buildLog.String())
	}
	t.Cleanup(func() { _ = minioContainer.Terminate(ctx) })
	host, _ := minioContainer.Host(ctx)
	port, _ := minioContainer.MappedPort(ctx, "9000/tcp")
	endpoint := host + ":" + port.Port()
	// Production adapter credentials are used for both PUT and private object reads.
	s3, e := assets.NewS3(endpoint, "test-access", "test-secret", "assets", false)
	if e != nil {
		t.Fatal(e)
	}
	admin, e := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4("test-access", "test-secret", ""), Secure: false})
	if e != nil {
		t.Fatal(e)
	}
	if e = admin.MakeBucket(ctx, "assets", minio.MakeBucketOptions{}); e != nil {
		t.Fatal(e)
	}
	f := setup(t)
	im := image.NewNRGBA(image.Rect(0, 0, 320, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 320; x++ {
			c := color.NRGBA{R: 255, A: 255}
			if x > 160 {
				c = color.NRGBA{B: 255, A: 255}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	var raw bytes.Buffer
	_ = png.Encode(&raw, im)
	hash := fingerprint(raw.Bytes())
	key := "projects/" + f.a.ProjectID.String() + "/assets/" + hash
	if e = s3.Put(ctx, key, bytes.NewReader(raw.Bytes()), int64(raw.Len()), "image/png"); e != nil {
		t.Fatal(e)
	}
	f.e.Exec(`UPDATE asset_files SET sha256=decode($1,'hex'),storage_key=$2,size_bytes=$3`, hash, key, raw.Len())
	f.e.Exec(`UPDATE object_versions SET body=jsonb_set(jsonb_set(body,'{fileHash}',to_jsonb($1::text)),'{size}',to_jsonb($3::int)) WHERE object_id=$2`, hash, f.id, raw.Len())
	c := config("")
	proxy, e := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: "ghcr.io/imgproxy/imgproxy:v4.0.17@sha256:db0b4b9cd690c8b3590203dea300fb759a18c4ec2af7b37424f0bdef23ce317d", Networks: []string{netw.Name}, ExposedPorts: []string{"8080/tcp"}, Env: map[string]string{"IMGPROXY_KEY": c.ProxyKey, "IMGPROXY_SALT": c.ProxySalt, "IMGPROXY_USE_S3": "true", "IMGPROXY_S3_ENDPOINT": "http://minio:9000", "IMGPROXY_S3_REGION": "us-east-1", "AWS_ACCESS_KEY_ID": "test-access", "AWS_SECRET_ACCESS_KEY": "test-secret", "IMGPROXY_ALLOWED_SOURCES": "s3://assets/projects/", "IMGPROXY_MAX_SRC_RESOLUTION": "40", "IMGPROXY_MAX_SRC_FILE_SIZE": "26214400", "IMGPROXY_MAX_RESULT_DIMENSION": "2560", "IMGPROXY_MAX_ANIMATION_FRAMES": "1", "IMGPROXY_STRIP_METADATA": "true"}, WaitingFor: wait.ForHTTP("/health").WithPort("8080/tcp").WithStartupTimeout(time.Minute)}, Started: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = proxy.Terminate(ctx) })
	phost, _ := proxy.Host(ctx)
	pport, _ := proxy.MappedPort(ctx, "8080/tcp")
	c.ProxyURL = "http://" + phost + ":" + pport.Port()
	f.s, e = New(c)
	if e != nil {
		t.Fatal(e)
	}
	f.s.Pool = f.e.Pool
	m := f.mint(t, Options{320, 200, "cover", "png"})
	for _, v := range m.Variants {
		got, e := f.s.Fetch(ctx, parse(t, v.URL))
		if e != nil {
			t.Fatal(v.Width, e)
		}
		cfg, _, e := image.DecodeConfig(bytes.NewReader(got.Body))
		if e != nil || cfg.Width != v.Width || cfg.Height != v.Width*200/320 {
			t.Fatal(v.Width, cfg, e)
		}
	}
	for _, format := range []string{"png", "jpeg", "webp", "avif"} {
		m := f.mint(t, Options{320, 200, "cover", format})
		got, e := f.s.Fetch(ctx, parse(t, m.Variants[0].URL))
		if e != nil {
			t.Fatal(format, e)
		}
		var cfg image.Config
		if format == "avif" {
			cfg, e = avif.DecodeConfig(bytes.NewReader(got.Body))
		} else {
			cfg, _, e = image.DecodeConfig(bytes.NewReader(got.Body))
		}
		if e != nil || cfg.Width != 320 || cfg.Height != 200 {
			t.Fatal(format, cfg, e)
		}
	}
	// Left vs right focus changes the crop, while original remains inaccessible anonymously.
	left, e := http.Get(c.ProxyURL + f.s.ProxyPath(key, Options{320, 320, "cover", "png"}, 0, 0.5))
	if e != nil {
		t.Fatal(e)
	}
	defer left.Body.Close()
	right, e := http.Get(c.ProxyURL + f.s.ProxyPath(key, Options{320, 320, "cover", "png"}, 1, 0.5))
	if e != nil {
		t.Fatal(e)
	}
	defer right.Body.Close()
	lb, _ := io.ReadAll(left.Body)
	rb, _ := io.ReadAll(right.Body)
	if left.StatusCode != 200 || right.StatusCode != 200 || bytes.Equal(lb, rb) {
		t.Fatal("focal crop")
	}
	resp, e := http.Get("http://" + endpoint + "/assets/" + key)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("public bucket")
	}
	signed := f.s.ProxyPath(key, Options{320, 0, "contain", "png"}, .5, .5)
	resp, e = http.Get(c.ProxyURL + "/tampered/" + strings.Join(strings.Split(signed, "/")[2:], "/"))
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("imgproxy accepted invalid signature", resp.StatusCode)
	}
	// SVG and animated GIF sources are delivered as raster images; animation stays bounded to one frame.
	var gifBytes bytes.Buffer
	frame := image.NewPaletted(image.Rect(0, 0, 320, 200), color.Palette{color.Black, color.White})
	if e = gif.EncodeAll(&gifBytes, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}}); e != nil {
		t.Fatal(e)
	}
	for _, source := range []struct {
		mime string
		body []byte
	}{{"image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="320" height="200"><rect width="320" height="200" fill="red"/></svg>`)}, {"image/gif", gifBytes.Bytes()}} {
		hash := fingerprint(source.body)
		key := "projects/" + f.a.ProjectID.String() + "/assets/" + hash
		if e = s3.Put(ctx, key, bytes.NewReader(source.body), int64(len(source.body)), source.mime); e != nil {
			t.Fatal(e)
		}
		f.e.Exec(`UPDATE asset_files SET sha256=decode($1,'hex'),storage_key=$2,mime_type=$3,size_bytes=$4`, hash, key, source.mime, len(source.body))
		f.e.Exec(`UPDATE object_versions SET body=jsonb_set(jsonb_set(jsonb_set(body,'{fileHash}',to_jsonb($1::text)),'{mimeType}',to_jsonb($3::text)),'{size}',to_jsonb($4::int)) WHERE object_id=$2`, hash, f.id, source.mime, len(source.body))
		m := f.mint(t, Options{320, 0, "contain", "webp"})
		got, e := f.s.Fetch(ctx, parse(t, m.Variants[0].URL))
		if e != nil {
			t.Fatal(source.mime, e)
		}
		cfg, _, e := image.DecodeConfig(bytes.NewReader(got.Body))
		if e != nil || cfg.Width != 320 || cfg.Height != 200 || bytes.Contains(got.Body, []byte("ANIM")) {
			t.Fatal(source.mime, cfg, e)
		}
	}
}
