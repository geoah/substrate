package blobbytes_test

// The s3 backend against a real S3-compatible server. The signing is written
// by hand (s3sign.go), so nothing but an endpoint that checks signatures
// proves it: a MinIO container, started once per test binary, the same shape
// internal/engine's *_db_test.go files use for Postgres.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/geoah/substrate/internal/blobbytes"
)

// The container's credentials. They are a throwaway container's root
// credentials, reachable only from this test's own docker network.
const (
	minioImage  = "minio/minio:RELEASE.2025-09-07T16-13-09Z"
	minioUser   = "substratetest"
	minioSecret = "substratetestsecret"
)

var (
	minioOnce     sync.Once
	minioEndpoint string
	minioErr      error
)

// minioURL starts the shared MinIO container and returns its endpoint.
func minioURL(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	minioOnce.Do(func() {
		ctx := context.Background()
		c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			Started: true,
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        minioImage,
				ExposedPorts: []string{"9000/tcp"},
				Cmd:          []string{"server", "/data"},
				Env: map[string]string{
					"MINIO_ROOT_USER":     minioUser,
					"MINIO_ROOT_PASSWORD": minioSecret,
				},
				WaitingFor: wait.ForHTTP("/minio/health/live").
					WithPort("9000/tcp").WithStartupTimeout(120 * time.Second),
			},
		})
		if err != nil {
			minioErr = err
			return
		}
		host, err := c.Host(ctx)
		if err != nil {
			minioErr = err
			return
		}
		port, err := c.MappedPort(ctx, "9000/tcp")
		if err != nil {
			minioErr = err
			return
		}
		minioEndpoint = fmt.Sprintf("http://%s:%s", host, port.Port())
	})
	if minioErr != nil {
		t.Fatalf("start the minio container: %v", minioErr)
	}
	return minioEndpoint
}

// newS3 opens the backend against a bucket of this test's own.
func newS3(t *testing.T, bucket, prefix string) *blobbytes.S3 {
	t.Helper()
	b, err := blobbytes.NewS3(blobbytes.S3Config{
		Endpoint:        minioURL(t),
		Bucket:          bucket,
		Region:          "us-east-1",
		AccessKeyID:     minioUser,
		SecretAccessKey: minioSecret,
		Prefix:          prefix,
		PathStyle:       true,
	})
	if err != nil {
		t.Fatalf("open the s3 backend: %v", err)
	}
	if err := b.CreateBucketForTest(context.Background()); err != nil {
		t.Fatalf("create the bucket: %v", err)
	}
	return b
}

func TestS3Store(t *testing.T) {
	t.Parallel()
	b := newS3(t, "conformance", "")
	conformance(t, func(t *testing.T, repository string) blobbytes.Store {
		t.Helper()
		s, err := b.Repository(repository, nil)
		if err != nil {
			t.Fatalf("bind %s: %v", repository, err)
		}
		return s
	})
}

func TestS3RepositoryIsolation(t *testing.T) {
	t.Parallel()
	b := newS3(t, "isolation", "")
	repositoryIsolation(t, func(t *testing.T, repository string) blobbytes.Store {
		t.Helper()
		s, err := b.Repository(repository, nil)
		if err != nil {
			t.Fatalf("bind %s: %v", repository, err)
		}
		return s
	})
	refuseBadRepository(t, b, nil)
}

// A prefix lets one bucket hold this substrate beside something else, and the
// listing must not report the something else as blobs.
func TestS3PrefixIsHonored(t *testing.T) {
	t.Parallel()
	b := newS3(t, "prefixed", "substrate/blobs")
	s, err := b.Repository("repoprefixed", nil)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	digest := put(t, s, []byte("under a prefix"))
	if got := read(t, s, digest, len("under a prefix")); string(got) != "under a prefix" {
		t.Fatalf("read back %q", got)
	}
	// The same bucket, no prefix: a store that ignored the prefix would find
	// this object at the bucket root.
	bare := newS3(t, "prefixed", "")
	other, err := bare.Repository("repoprefixed", nil)
	if err != nil {
		t.Fatalf("bind without the prefix: %v", err)
	}
	held, err := other.Exists(context.Background(), digest)
	if err != nil {
		t.Fatalf("exists without the prefix: %v", err)
	}
	if held {
		t.Fatal("the object is addressable without the configured prefix")
	}
}

// `max-keys` bounds the KEYS the endpoint returns, and keys that are not
// digests are dropped here, so a page can come back holding no objects while
// the store holds plenty. A caller reading that as the end of the store would
// stop a sweep or a move early, so the listing follows the continuation token
// until it has what it asked for.
func TestS3ListWalksPastKeysThatAreNotBlobs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newS3(t, "straykeys", "")
	s, err := b.Repository("repostray", nil)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	digest := put(t, s, []byte("the one real blob"))
	// Sorts before any digest, so a single-key page holds only this.
	if err := b.PutRawForTest(ctx, "repostray/README", []byte("not a blob")); err != nil {
		t.Fatalf("write the stray key: %v", err)
	}
	objects, err := s.List(ctx, "", 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objects) != 1 || objects[0].Digest != digest {
		t.Fatalf("list returned %v, want the one blob %s", objects, digest)
	}
}

func TestS3RefusesAHalfConfiguredBackend(t *testing.T) {
	t.Parallel()
	full := blobbytes.S3Config{
		Endpoint: "http://127.0.0.1:9000", Bucket: "b", Region: "us-east-1",
		AccessKeyID: "k", SecretAccessKey: "s",
	}
	cases := map[string]func(c *blobbytes.S3Config){
		"no endpoint":    func(c *blobbytes.S3Config) { c.Endpoint = "" },
		"no bucket":      func(c *blobbytes.S3Config) { c.Bucket = "" },
		"no region":      func(c *blobbytes.S3Config) { c.Region = "" },
		"no key id":      func(c *blobbytes.S3Config) { c.AccessKeyID = "" },
		"no secret":      func(c *blobbytes.S3Config) { c.SecretAccessKey = "" },
		"hostless URL":   func(c *blobbytes.S3Config) { c.Endpoint = "not-a-url" },
		"escaped prefix": func(c *blobbytes.S3Config) { c.Prefix = "a?b" },
	}
	for name, break_ := range cases {
		cfg := full
		break_(&cfg)
		if _, err := blobbytes.NewS3(cfg); err == nil {
			t.Fatalf("the s3 backend opened with %s", name)
		}
	}
}

// The engine hands Put a reader it promises hashes to the digest, and the
// request is signed with that hash: an endpoint that checks it refuses bytes
// that disagree, so a caller cannot store one blob's bytes under another
// blob's name.
func TestS3RefusesBytesThatAreNotTheirDigest(t *testing.T) {
	t.Parallel()
	b := newS3(t, "mismatch", "")
	s, err := b.Repository("repomismatch", nil)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	lie := []byte("these are not those bytes")
	digest := digestOf([]byte("the bytes the digest names"))
	err = s.Put(context.Background(), digest, int64(len(lie)), strings.NewReader(string(lie)))
	if err == nil {
		t.Fatal("the endpoint accepted bytes that do not hash to the digest they were stored under")
	}
}
