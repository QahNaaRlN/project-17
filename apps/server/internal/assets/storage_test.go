package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// CNT-040/042: real S3 signing, PUT, private reads and canonical processing.
func TestS3PrivateStorage(t *testing.T) {
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: "minio/minio:RELEASE.2025-04-22T22-12-26Z", ExposedPorts: []string{"9000/tcp"}, Env: map[string]string{"MINIO_ROOT_USER": "test-access", "MINIO_ROOT_PASSWORD": "test-secret"}, Cmd: []string{"server", "/data"}, WaitingFor: wait.ForHTTP("/minio/health/ready").WithPort("9000/tcp").WithStartupTimeout(time.Minute)}, Started: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Terminate(ctx); err != nil {
			t.Error(err)
		}
	})
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := c.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewS3(host+":"+port.Port(), "test-access", "test-secret", "assets", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.client.MakeBucket(ctx, "assets", minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	signed, err := s.UploadURL(ctx, "projects/p/uploads/u")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(signed)
	if u.Query().Get("X-Amz-Expires") != "900" {
		t.Fatal("expiry")
	}
	req, _ := http.NewRequest(http.MethodPut, signed, strings.NewReader("%PDF-1.7\n"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.Status)
	}
	r, n, err := s.Open(ctx, "projects/p/uploads/u")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(r)
	r.Close()
	if err != nil || n != 9 || string(body) != "%PDF-1.7\n" {
		t.Fatal(n, string(body), err)
	}
	if err := s.Put(ctx, "projects/p/assets/hash", bytes.NewReader(body), int64(len(body)), "application/pdf"); err != nil {
		t.Fatal(err)
	}
	resp, err = http.Get("http://" + host + ":" + port.Port() + "/assets/projects/p/assets/hash")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("bucket public", resp.Status)
	}
	// Run the command/worker path against actual MinIO, then overwrite staging:
	// the old PUT capability must never alter the immutable ready representation.
	e := cmstest.New(t)
	service := &Service{Pool: e.Pool, Storage: s}
	service.Register(e.Bus)
	imageBytes := pngBytes(t, 2, 3)
	hash := sha256.Sum256(imageBytes)
	var upload Upload
	e.Must(e.Admin, "create-asset-upload", Create{Filename: "photo.png", MIME: "image/png", Size: int64(len(imageBytes)), SHA256: hex.EncodeToString(hash[:])}, &upload)
	put := func(data []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, upload.UploadURL, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal(resp.Status)
		}
	}
	put(imageBytes)
	e.Must(e.Admin, "complete-asset-upload", Ref{upload.AssetID}, nil)
	if err := service.Process(ctx, ProcessArgs{upload.AssetID, e.Admin.ProjectID}); err != nil {
		t.Fatal(err)
	}
	processed, err := service.Get(ctx, e.Admin, upload.AssetID)
	if err != nil || processed.Status != "ready" || processed.FileHash == nil || *processed.FileHash != hex.EncodeToString(hash[:]) {
		t.Fatal(processed, err)
	}
	put([]byte("changed staging"))
	if err := service.Process(ctx, ProcessArgs{upload.AssetID, e.Admin.ProjectID}); err != nil {
		t.Fatal(err)
	}
	r, _, err = s.Open(ctx, "projects/"+e.Admin.ProjectID.String()+"/assets/"+*processed.FileHash)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := io.ReadAll(r)
	r.Close()
	if err != nil || !bytes.Equal(ready, imageBytes) {
		t.Fatal("ready file changed", err)
	}
	if _, _, err := s.Open(ctx, "missing"); err == nil {
		t.Fatal("missing object")
	}
	if _, err := NewS3("https://invalid/path", "a", "b", "assets", true); err == nil {
		t.Fatal("endpoint")
	}
	broken, _ := NewS3("127.0.0.1:1", "a", "b", "assets", false)
	if err := broken.Put(ctx, "key", strings.NewReader("x"), 1, "text/plain"); err == nil {
		t.Fatal("network")
	}
}
