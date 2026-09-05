package config

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"

	"github.com/geoah/substrate/internal/blobbytes"
)

// Blobs says where blob bytes live. It is part of Config and also loadable on
// its own, because substratectl's operator hat needs the same answer to
// migrate bytes out of the `blobs` column.
type Blobs struct {
	// Store is `fs` (the default: <data root>/repositories/<id>/blobs, so the
	// repository directory is the whole backup) or `s3` (a bucket, which
	// makes the backup two artifacts; docs/operations.md says what the second
	// one is). `postgres`, the `blobs` bytea column, is no longer a runtime
	// choice: the column is readable only through `substratectl blobs
	// migrate --from postgres`, which moves the bytes out.
	Store string `envconfig:"SUBSTRATE_BLOB_STORE" default:"fs"`
	// The s3 backend: any S3-compatible endpoint. The bucket must be
	// PRIVATE — the bytes are stored as they arrived, so anything that can
	// read the bucket can read every repository's blobs.
	S3Endpoint     string `envconfig:"SUBSTRATE_BLOB_S3_ENDPOINT" default:""`
	S3Bucket       string `envconfig:"SUBSTRATE_BLOB_S3_BUCKET" default:""`
	S3Region       string `envconfig:"SUBSTRATE_BLOB_S3_REGION" default:"us-east-1"`
	S3AccessKeyID  string `envconfig:"SUBSTRATE_BLOB_S3_ACCESS_KEY_ID" default:""`
	S3SecretKey    string `envconfig:"SUBSTRATE_BLOB_S3_SECRET_ACCESS_KEY" default:""`
	S3SessionToken string `envconfig:"SUBSTRATE_BLOB_S3_SESSION_TOKEN" default:""`
	// S3Prefix goes in front of every key, for a bucket this substrate shares
	// with something else.
	S3Prefix string `envconfig:"SUBSTRATE_BLOB_S3_PREFIX" default:""`
	// S3PathStyle addresses the bucket as a path segment rather than as a
	// subdomain. Every self-hosted S3-compatible server wants it, which is
	// why it is the default; AWS still accepts it.
	S3PathStyle bool `envconfig:"SUBSTRATE_BLOB_S3_PATH_STYLE" default:"true"`
}

// LoadBlobs reads the blob store configuration alone, for a command that has
// no use for the rest of the service configuration and no DATABASE_URL to
// satisfy.
func LoadBlobs() (Blobs, error) {
	var b Blobs
	err := envconfig.Process("", &b)
	return b, err
}

// Backend builds the configured store. The fs backend is rooted at dataRoot,
// the data root every repository directory lives under (Data.Root). An
// unknown name is a refusal that lists the two, rather than a silent fall
// back to the default: a typo in SUBSTRATE_BLOB_STORE would otherwise write
// bytes somewhere the operator did not mean. `postgres` is refused by name,
// with the one command that still reads the column.
func (b Blobs) Backend(dataRoot string) (blobbytes.Backend, error) {
	switch b.Store {
	case "", blobbytes.BackendFS:
		return blobbytes.NewFS(dataRoot)
	case blobbytes.BackendS3:
		return blobbytes.NewS3(blobbytes.S3Config{
			Endpoint:        b.S3Endpoint,
			Bucket:          b.S3Bucket,
			Region:          b.S3Region,
			AccessKeyID:     b.S3AccessKeyID,
			SecretAccessKey: b.S3SecretKey,
			SessionToken:    b.S3SessionToken,
			Prefix:          b.S3Prefix,
			PathStyle:       b.S3PathStyle,
		})
	case blobbytes.BackendPostgres:
		return nil, fmt.Errorf("SUBSTRATE_BLOB_STORE %q is not a runtime store: the `blobs` column is readable only through `substratectl blobs migrate --from postgres`, which moves the bytes into %s or %s",
			b.Store, blobbytes.BackendFS, blobbytes.BackendS3)
	default:
		return nil, fmt.Errorf("unknown SUBSTRATE_BLOB_STORE %q: one of %s, %s",
			b.Store, blobbytes.BackendFS, blobbytes.BackendS3)
	}
}
