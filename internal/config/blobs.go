package config

import (
	"context"
	"fmt"

	"github.com/kelseyhightower/envconfig"

	"github.com/geoah/substrate/internal/blobbytes"
)

// Blobs says where blob bytes live. It is part of Config and also loadable on
// its own, because substratectl's operator hat opens the same store the
// server writes without the rest of the server's configuration.
type Blobs struct {
	// Store is `fs` (the default: <data root>/repositories/<authority>/blobs,
	// so the repository directory is the whole backup) or `s3` (a bucket,
	// which makes the backup two artifacts; docs/operations.md says what the
	// second one is).
	Store string `envconfig:"SUBSTRATE_BLOB_STORE" default:"fs"`
	// The s3 backend: any S3-compatible endpoint with a general-purpose
	// bucket. The bucket must be PRIVATE: the bytes are stored as they
	// arrived, so anything that can read the bucket can read every
	// repository's blobs.
	S3Endpoint     string `envconfig:"SUBSTRATE_BLOB_S3_ENDPOINT" default:""`
	S3Bucket       string `envconfig:"SUBSTRATE_BLOB_S3_BUCKET" default:""`
	S3Region       string `envconfig:"SUBSTRATE_BLOB_S3_REGION" default:"us-east-1"`
	S3AccessKeyID  string `envconfig:"SUBSTRATE_BLOB_S3_ACCESS_KEY_ID" default:""`
	S3SecretKey    string `envconfig:"SUBSTRATE_BLOB_S3_SECRET_ACCESS_KEY" default:""`
	S3SessionToken string `envconfig:"SUBSTRATE_BLOB_S3_SESSION_TOKEN" default:""`
	// S3Prefix goes in front of every key, for a bucket this substrate shares
	// with something else. Never with another substrate.
	S3Prefix string `envconfig:"SUBSTRATE_BLOB_S3_PREFIX" default:""`
	// S3PathStyle addresses the bucket as a path segment rather than as a
	// subdomain. Self-hosted S3-compatible servers want it, which is why it
	// is the default; AWS still accepts it.
	S3PathStyle bool `envconfig:"SUBSTRATE_BLOB_S3_PATH_STYLE" default:"true"`
}

// LoadBlobs reads the blob store configuration alone, validated, for a
// command that has no use for the rest of the service configuration.
func LoadBlobs() (Blobs, error) {
	var b Blobs
	if err := envconfig.Process("", &b); err != nil {
		return b, err
	}
	return b, b.Validate()
}

// Validate refuses an unknown store, naming the two, rather than falling back
// to the default: a typo in SUBSTRATE_BLOB_STORE would otherwise write bytes
// somewhere the operator did not mean. Under `s3` the bucket's own settings
// are checked too, so a half-configured bucket refuses the boot by name.
func (b Blobs) Validate() error {
	switch b.Store {
	case "", blobbytes.BackendFS:
		return nil
	case blobbytes.BackendS3:
		_, err := b.S3()
		return err
	default:
		return fmt.Errorf("SUBSTRATE_BLOB_STORE is %q: it is %s (the default) or %s",
			b.Store, blobbytes.BackendFS, blobbytes.BackendS3)
	}
}

// S3 builds the bucket backend from the SUBSTRATE_BLOB_S3_* variables,
// whichever store is live: `repository blobs migrate` opens both.
func (b Blobs) S3() (*blobbytes.S3, error) {
	s, err := blobbytes.NewS3(blobbytes.S3Config{
		Endpoint:        b.S3Endpoint,
		Bucket:          b.S3Bucket,
		Region:          b.S3Region,
		AccessKeyID:     b.S3AccessKeyID,
		SecretAccessKey: b.S3SecretKey,
		SessionToken:    b.S3SessionToken,
		Prefix:          b.S3Prefix,
		PathStyle:       b.S3PathStyle,
	})
	if err != nil {
		return nil, fmt.Errorf("the SUBSTRATE_BLOB_S3_* variables: %w", err)
	}
	return s, nil
}

// Open builds the live store and, for a bucket, proves it reachable before
// anything is served: a wrong endpoint, bucket or key refuses here, naming
// the bucket, rather than at the first upload. The fs backend is rooted at
// dataRoot (Data.Root).
func (b Blobs) Open(ctx context.Context, dataRoot string) (blobbytes.Backend, error) {
	if b.Store != blobbytes.BackendS3 {
		return blobbytes.NewFS(dataRoot)
	}
	s, err := b.S3()
	if err != nil {
		return nil, err
	}
	if err := s.Check(ctx); err != nil {
		return nil, fmt.Errorf("SUBSTRATE_BLOB_STORE is s3 and the bucket cannot be listed: %w", err)
	}
	return s, nil
}

// External reports whether the live store is outside the data root, which is
// when a repository directory must hold no blob bytes of its own
// (engine.WithBlobsOutsideDataRoot).
func (b Blobs) External() bool { return b.Store == blobbytes.BackendS3 }

// Live names the live store: SUBSTRATE_BLOB_STORE, with unset read as the
// default.
func (b Blobs) Live() string {
	if b.Store == "" {
		return blobbytes.BackendFS
	}
	return b.Store
}
