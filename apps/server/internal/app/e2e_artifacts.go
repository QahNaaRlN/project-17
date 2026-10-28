//go:build e2e

package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// The §2 stand keeps check artifacts in a directory (CMS_E2E_ARTIFACTS_DIR): published
// MinIO images are unavailable to clean runners (docs/pipeline-visual-lighthouse.md §7).
// Product builds do not compile this file and keep artifacts only in S3.
func init() {
	if dir := os.Getenv("CMS_E2E_ARTIFACTS_DIR"); dir != "" {
		e2eArtifacts = func() workflow.ArtifactStore { return dirArtifacts(dir) }
	}
}

type dirArtifacts string

func (d dirArtifacts) path(key string) string { return filepath.Join(string(d), filepath.FromSlash(key)) }

func (d dirArtifacts) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	p := d.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

func (d dirArtifacts) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	f, err := os.Open(d.path(key))
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

func (d dirArtifacts) Remove(_ context.Context, key string) error {
	if err := os.Remove(d.path(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
