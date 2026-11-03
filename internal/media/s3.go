package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/kaecyra/gobbler/internal/config"
)

// S3 is a Store over a bucket of any S3-compatible service; the deployment's
// is Garage on the home network (ADR-00033). The bucket is never exposed: the
// app reads the bytes and serves them itself.
type S3 struct {
	client *minio.Client
	bucket string
}

// NewS3 builds a client for bucket from the endpoint and credentials in cfg.
// It makes no network call.
func NewS3(cfg config.S3, bucket string) (*S3, error) {
	host, secure, err := cfg.HostTLS()
	if err != nil {
		return nil, err
	}
	c, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID.Reveal(), cfg.SecretAccessKey.Reveal(), ""),
		Secure: secure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client for %q: %w", host, err)
	}
	return &S3{client: c, bucket: bucket}, nil
}

// Put uploads data to key.
func (s *S3) Put(ctx context.Context, key string, data []byte, contentType string) error {
	if err := validKey(key); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put media %q in bucket %q: %w", key, s.bucket, err)
	}
	return nil
}

// Get downloads key.
func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	if err := validKey(key); err != nil {
		return nil, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get media %q from bucket %q: %w", key, s.bucket, err)
	}
	defer func() { _ = obj.Close() }() // a read-only handle; nothing is lost if closing fails
	b, err := io.ReadAll(obj)
	if err != nil {
		if resp := minio.ToErrorResponse(err); resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey" {
			return nil, fmt.Errorf("media %q: %w", key, ErrNotFound)
		}
		return nil, fmt.Errorf("read media %q from bucket %q: %w", key, s.bucket, err)
	}
	return b, nil
}

// Delete removes key; S3 does not mind a key that is not there.
func (s *S3) Delete(ctx context.Context, key string) error {
	if err := validKey(key); err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete media %q from bucket %q: %w", key, s.bucket, err)
	}
	return nil
}
