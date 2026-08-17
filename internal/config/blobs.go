package config

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"

	"github.com/geoah/substrate/internal/blobbytes"
)

// Blobs says where blob bytes live. It is part of Config and also loadable on
// its own, because substratectl's operator hat needs the same answer to
// migrate bytes from one backend to another.
type Blobs struct {
	// Store is `postgres` (the default, the `blobs` bytea column, where one
	// database dump is a whole backup), `fs` (a directory) or `s3` (a
	// bucket). Both external backends put the bytes outside the database, so
	// a backup becomes two artifacts — docs/operations.md says what the
	// second one is. Switching backends on a store that already holds bytes
	// is refused at boot; `substratectl blobs migrate` is the way across.
	Store string `envconfig:"SUBSTRATE_BLOB_STORE" default:"postgres"`
	// FSRoot is the fs backend's root directory, one subdirectory per
	// repository. It must be absolute, and it must be on storage that
	// outlives the container.
	FSRoot string `envconfig:"SUBSTRATE_BLOB_FS_ROOT" default:""`
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

// Backend builds the configured store. An unknown name is a refusal that
// lists the three, rather than a silent fall back to the default: a typo in
// SUBSTRATE_BLOB_STORE would otherwise write bytes somewhere the operator did
// not mean.
func (b Blobs) Backend() (blobbytes.Backend, error) {
	switch b.Store {
	case "", blobbytes.BackendPostgres:
		return blobbytes.NewPostgres(), nil
	case blobbytes.BackendFS:
		return blobbytes.NewFS(b.FSRoot)
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
	default:
		return nil, fmt.Errorf("unknown SUBSTRATE_BLOB_STORE %q: one of %s, %s, %s",
			b.Store, blobbytes.BackendPostgres, blobbytes.BackendFS, blobbytes.BackendS3)
	}
}
