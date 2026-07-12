package storage

import (
	"context"
	"fmt"

	"github.com/pgvanniekerk/baasparse/internal/store"
)

// BackendSFTP is a source-origin marker, not a storage backend: the fetcher pulls
// from the remote SFTP endpoint and lands files into the source's input area,
// which is itself posix (rooted at Source.Root) or s3. So for storage resolution
// an "sftp" source behaves as posix.
const BackendSFTP = "sftp"

// NewForSource builds the storage.Store the data plane uses for a pipeline's
// source: an s3 store when Source.Backend=="s3", otherwise a posix store rooted
// at Source.Root (covers "posix", "sftp" and the empty/legacy default).
func NewForSource(ctx context.Context, src store.Source) (Store, error) {
	switch src.Backend {
	case BackendS3:
		return New(ctx, Config{
			Backend: BackendS3, Endpoint: src.S3.Endpoint, Region: src.S3.Region, Bucket: src.S3.Bucket,
			AccessKey: src.S3.AccessKey, SecretKey: src.S3.SecretKey, UseSSL: src.S3.UseSSL,
			CreateBucket: src.S3.CreateBucket,
		})
	case "", BackendPOSIX, BackendSFTP:
		return New(ctx, Config{Backend: BackendPOSIX, Root: src.Root})
	default:
		return nil, fmt.Errorf("storage: unsupported source backend %q", src.Backend)
	}
}

// NewForDest builds the storage.Store for an archive destination.
func NewForDest(ctx context.Context, dest store.Dest) (Store, error) {
	switch dest.Backend {
	case BackendS3:
		return New(ctx, Config{
			Backend: BackendS3, Endpoint: dest.S3.Endpoint, Region: dest.S3.Region, Bucket: dest.S3.Bucket,
			AccessKey: dest.S3.AccessKey, SecretKey: dest.S3.SecretKey, UseSSL: dest.S3.UseSSL,
			CreateBucket: dest.S3.CreateBucket,
		})
	case "", BackendPOSIX, BackendSFTP:
		return New(ctx, Config{Backend: BackendPOSIX, Root: dest.Root})
	default:
		return nil, fmt.Errorf("storage: unsupported dest backend %q", dest.Backend)
	}
}

// SourceKey is a stable cache key for the store a source resolves to. An empty
// string means the source carries no storage config and callers should fall back
// to the process-default store.
func SourceKey(src store.Source) string {
	switch src.Backend {
	case BackendS3:
		return s3Key(src.S3.Endpoint, src.S3.Bucket, src.S3.Region, src.S3.AccessKey, src.S3.UseSSL)
	case BackendPOSIX, BackendSFTP:
		return "posix|" + src.Root
	default: // "" / legacy
		if src.Root != "" {
			return "posix|" + src.Root
		}
		return ""
	}
}

// DestKey is a stable cache key for the store a dest resolves to.
func DestKey(dest store.Dest) string {
	switch dest.Backend {
	case BackendS3:
		return s3Key(dest.S3.Endpoint, dest.S3.Bucket, dest.S3.Region, dest.S3.AccessKey, dest.S3.UseSSL)
	default:
		return "posix|" + dest.Root
	}
}

// s3Key builds the cache identity for an s3 store. It includes the access key and
// TLS flag (not just endpoint/bucket/region) so two datasources on the same bucket
// that authenticate differently resolve to SEPARATE Store instances rather than
// colliding on one (which would silently reuse the first datasource's credentials).
func s3Key(endpoint, bucket, region, accessKey string, useSSL bool) string {
	ssl := "p"
	if useSSL {
		ssl = "s"
	}
	return "s3|" + endpoint + "|" + bucket + "|" + region + "|" + accessKey + "|" + ssl
}
