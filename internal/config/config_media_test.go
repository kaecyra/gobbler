package config

import (
	"strings"
	"testing"
)

func TestMediaBucketIsReadBesideTheS3Settings(t *testing.T) {
	full := with(EnvS3Endpoint, "https://garage.lan:3900", EnvS3Region, "garage", EnvS3Bucket, "backups",
		EnvS3AccessKeyID, "id", EnvS3SecretAccessKey, "sk", EnvS3MediaBucket, "media")
	c, err := loadFrom(env(full))
	if err != nil {
		t.Fatal(err)
	}
	if c.Backup.S3.MediaBucket != "media" || c.Backup.S3.Bucket != "backups" {
		t.Errorf("S3 = %+v, want separate backup and media buckets", c.Backup.S3)
	}
	// Without S3 the bucket is empty and media stays on disk.
	if c, err := loadFrom(env(base())); err != nil || c.Backup.S3.MediaBucket != "" {
		t.Errorf("no S3: %+v, %v", c.Backup.S3, err)
	}
	// The media bucket alone is a half-configured S3, named in the error.
	_, err = loadFrom(env(with(EnvS3MediaBucket, "media")))
	if err == nil || !strings.Contains(err.Error(), EnvS3Endpoint) {
		t.Errorf("media bucket alone: %v, want it to name %s", err, EnvS3Endpoint)
	}
}

func TestS3HostTLS(t *testing.T) {
	for _, tc := range []struct {
		endpoint, host string
		secure         bool
		fails          bool
	}{
		{"s3.example.com", "s3.example.com", true, false},
		{"https://s3.example.com", "s3.example.com", true, false},
		{"http://garage.lan:3900", "garage.lan:3900", false, false},
		{"ftp://x", "", false, true},
	} {
		host, secure, err := S3{Endpoint: tc.endpoint}.HostTLS()
		if (err != nil) != tc.fails || host != tc.host || secure != tc.secure {
			t.Errorf("HostTLS(%q) = %q, %v, %v", tc.endpoint, host, secure, err)
		}
	}
}
