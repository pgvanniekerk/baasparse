package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// s3Store is the S3-compatible object-storage backend (topology B). Works with
// AWS S3, MinIO, Ceph RGW and GCS S3-interop. The bucket is the store root; keys
// are object keys. Atomic PutObject means output is never partially visible
// (BR-STO-004, strengthening BR-DST-003). There is no atomic rename, so Move is
// a server-side copy + delete — the authoritative lifecycle state is PostgreSQL
// (TS 16 §16.3).
type s3Store struct {
	client *minio.Client
	bucket string
}

func newS3(ctx context.Context, cfg Config) (*s3Store, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("storage: s3 requires endpoint and bucket")
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: s3 client: %w", err)
	}
	s := &s3Store{client: client, bucket: cfg.Bucket}
	if cfg.CreateBucket {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			// Idempotent across concurrent replicas: "already owned/exists" is fine.
			exists, existsErr := client.BucketExists(ctx, cfg.Bucket)
			if existsErr != nil {
				return nil, fmt.Errorf("storage: s3 bucket check: %w", existsErr)
			}
			if !exists {
				return nil, fmt.Errorf("storage: s3 make bucket: %w", err)
			}
		}
	}
	return s, nil
}

// s3Ping validates an S3 endpoint + credentials without a specific bucket, for a
// connection-only datasource. ListBuckets exercises auth and reachability; some
// scoped credentials may lack s3:ListAllMyBuckets, in which case the operator can
// instead test against a specific bucket (Probe via TestConnection).
func s3Ping(ctx context.Context, cfg Config) error {
	if cfg.Endpoint == "" {
		return fmt.Errorf("storage: s3 requires an endpoint")
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return fmt.Errorf("storage: s3 client: %w", err)
	}
	if _, err := client.ListBuckets(ctx); err != nil {
		return fmt.Errorf("storage: s3 connect: %w", err)
	}
	return nil
}

func (s *s3Store) Backend() string { return BackendS3 }

func (s *s3Store) List(ctx context.Context, prefix string) ([]Entry, error) {
	p := prefix
	if p != "" && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	var out []Entry
	// Recursive:false applies the "/" delimiter, giving single-level listing that
	// matches the POSIX backend (os.ReadDir): direct objects plus common-prefix
	// pseudo-entries, which we skip along with dot-prefixed/hidden names.
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: p, Recursive: false}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		if strings.HasSuffix(obj.Key, "/") { // common-prefix ("directory") marker
			continue
		}
		if strings.HasPrefix(path.Base(obj.Key), ".") { // hidden / in-progress temp names
			continue
		}
		out = append(out, Entry{Key: obj.Key, Size: obj.Size, ModTime: obj.LastModified, ETag: obj.ETag})
	}
	return out, nil
}

func (s *s3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// Surface a missing object as ErrNotFound on first read via a stat.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return obj, nil
}

func (s *s3Store) Put(ctx context.Context, key string, body io.Reader, m Meta) error {
	ct := m.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	// size -1 => streaming multipart upload; completion is atomic.
	_, err := s.client.PutObject(ctx, s.bucket, key, body, -1, minio.PutObjectOptions{ContentType: ct})
	return err
}

func (s *s3Store) Stat(ctx context.Context, key string) (Entry, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, err
	}
	return Entry{Key: key, Size: info.Size, ModTime: info.LastModified, ETag: info.ETag}, nil
}

func (s *s3Store) Move(ctx context.Context, srcKey, dstKey string) error {
	dst := minio.CopyDestOptions{Bucket: s.bucket, Object: dstKey}
	src := minio.CopySrcOptions{Bucket: s.bucket, Object: srcKey}
	if _, err := s.client.CopyObject(ctx, dst, src); err != nil {
		if isNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	return s.client.RemoveObject(ctx, s.bucket, srcKey, minio.RemoveObjectOptions{})
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *s3Store) Close() error { return nil }

func isNotFound(err error) bool {
	var resp minio.ErrorResponse
	if errors.As(err, &resp) {
		return resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket" || resp.StatusCode == 404
	}
	return false
}
