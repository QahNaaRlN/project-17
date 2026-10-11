// Package assets implements project-scoped uploads and immutable processed files.
package assets

import (
	"context"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Storage never exposes a write URL for a ready content-addressed key.
type Storage interface {
	UploadURL(context.Context, string) (string, error)
	Open(context.Context, string) (io.ReadCloser, int64, error)
	Put(context.Context, string, io.Reader, int64, string) error
}

type S3 struct {
	client *minio.Client
	bucket string
}

func NewS3(endpoint, access, secret, bucket string, secure bool) (*S3, error) {
	c, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: secure})
	if err != nil {
		return nil, err
	}
	return &S3{client: c, bucket: bucket}, nil
}
func (s *S3) UploadURL(ctx context.Context, key string) (string, error) {
	u, err := s.client.PresignedPutObject(ctx, s.bucket, key, 15*time.Minute)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	o, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	info, err := o.Stat()
	if err != nil {
		_ = o.Close()
		return nil, 0, err
	}
	return o, info.Size, nil
}
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: mime})
	return err
}
