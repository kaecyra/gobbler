package backup

import (
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/kaecyra/gobbler/internal/config"
)

// S3Store is a Store over any S3-compatible endpoint: AWS S3, Backblaze B2,
// Cloudflare R2 or MinIO.
type S3Store struct {
	client *minio.Client
	bucket string
}

// NewS3Store builds a client from cfg. The endpoint may carry an http:// or
// https:// scheme; without one TLS is used. It makes no network call.
func NewS3Store(cfg config.S3) (*S3Store, error) {
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
	return &S3Store{client: c, bucket: cfg.Bucket}, nil
}

// Put uploads the file at path to key.
func (s *S3Store) Put(ctx context.Context, key, path string) error {
	_, err := s.client.FPutObject(ctx, s.bucket, key, path, minio.PutObjectOptions{ContentType: "application/vnd.sqlite3"})
	return err
}

// List returns the keys under prefix.
func (s *S3Store) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

// Delete removes key.
func (s *S3Store) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
