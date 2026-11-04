package blobbytes_test

// The s3 backend against a real S3-compatible server: a versitygw container
// over its posix backend, started once per test binary, the same shape
// internal/engine's *_db_test.go files use for Postgres. Skipped in -short
// mode.

import (
	"context"
	"errors"
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
	gatewayImage  = "versity/versitygw:v1.9.0"
	gatewayUser   = "substratetest"
	gatewaySecret = "substratetestsecret"
)

var (
	gatewayOnce     sync.Once
	gatewayEndpoint string
	gatewayErr      error
)

// gatewayURL starts the shared gateway container and returns its endpoint.
func gatewayURL(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	gatewayOnce.Do(func() {
		ctx := context.Background()
		c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			Started: true,
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        gatewayImage,
				ExposedPorts: []string{"7070/tcp"},
				// Every directory under the root is a bucket. --nometa, because
				// the container's overlay filesystem need not carry the
				// extended attributes object metadata lives in, and nothing
				// here stores any.
				Cmd: []string{"--access", gatewayUser, "--secret", gatewaySecret, "posix", "--nometa", "/tmp"},
				WaitingFor: wait.ForListeningPort("7070/tcp").
					WithStartupTimeout(120 * time.Second),
			},
		})
		if err != nil {
			gatewayErr = err
			return
		}
		host, err := c.Host(ctx)
		if err != nil {
			gatewayErr = err
			return
		}
		port, err := c.MappedPort(ctx, "7070/tcp")
		if err != nil {
			gatewayErr = err
			return
		}
		gatewayEndpoint = fmt.Sprintf("http://%s:%s", host, port.Port())
	})
	if gatewayErr != nil {
		t.Fatalf("start the s3 gateway container: %v", gatewayErr)
	}
	return gatewayEndpoint
}

// s3Config is a config for a bucket of this test's own on the container.
func s3Config(t *testing.T, bucket, prefix string) blobbytes.S3Config {
	t.Helper()
	return blobbytes.S3Config{
		Endpoint:        gatewayURL(t),
		Bucket:          bucket,
		Region:          "us-east-1",
		AccessKeyID:     gatewayUser,
		SecretAccessKey: gatewaySecret,
		Prefix:          prefix,
		PathStyle:       true,
	}
}

// newS3 opens the backend against a bucket of this test's own.
func newS3(t *testing.T, bucket, prefix string) *blobbytes.S3 {
	t.Helper()
	b, err := blobbytes.NewS3(s3Config(t, bucket, prefix))
	if err != nil {
		t.Fatalf("open the s3 backend: %v", err)
	}
	if err := b.CreateBucketForTest(context.Background()); err != nil {
		t.Fatalf("create the bucket: %v", err)
	}
	return b
}

func bind(t *testing.T, b blobbytes.Backend, repository string) blobbytes.Store {
	t.Helper()
	s, err := b.Repository(repository)
	if err != nil {
		t.Fatalf("bind %s: %v", repository, err)
	}
	return s
}

func TestS3Store(t *testing.T) {
	t.Parallel()
	b := newS3(t, "conformance", "")
	conformance(t, func(t *testing.T, repository string) blobbytes.Store { return bind(t, b, repository) })
}

func TestS3RepositoryIsolation(t *testing.T) {
	t.Parallel()
	b := newS3(t, "isolation", "")
	repositoryIsolation(t, func(t *testing.T, repository string) blobbytes.Store { return bind(t, b, repository) })
	refuseBadRepository(t, b)
}

// A prefix lets one bucket hold this substrate beside something else, and the
// object must not be addressable without it.
func TestS3PrefixIsHonored(t *testing.T) {
	t.Parallel()
	s := bind(t, newS3(t, "prefixed", "substrate/blobs"), "repoprefixed.example.com")
	digest := put(t, s, []byte("under a prefix"))
	if got := read(t, s, digest, len("under a prefix")); string(got) != "under a prefix" {
		t.Fatalf("read back %q", got)
	}
	other := bind(t, newS3(t, "prefixed", ""), "repoprefixed.example.com")
	held, err := other.Exists(context.Background(), digest)
	if err != nil {
		t.Fatalf("exists without the prefix: %v", err)
	}
	if held {
		t.Fatal("the object is addressable without the configured prefix")
	}
}

// MaxKeys bounds the KEYS the endpoint returns, and keys that are not digests
// are dropped, so a page can come back holding no objects while the store
// holds plenty. The orphan sweep reads a short result as the end of the store,
// so the listing follows the continuation token until it has what it asked
// for.
func TestS3ListWalksPastKeysThatAreNotBlobs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newS3(t, "straykeys", "")
	s := bind(t, b, "repostray.example.com")
	digest := put(t, s, []byte("the one real blob"))
	// Sorts before any digest, so a single-key page holds only these.
	for i := range 3 {
		key := fmt.Sprintf("repositories/repostray.example.com/blobs/README-%d", i)
		if err := b.PutRawForTest(ctx, key, []byte("not a blob")); err != nil {
			t.Fatalf("write the stray key: %v", err)
		}
	}
	objects, err := s.List(ctx, "", 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objects) != 1 || objects[0].Digest != digest {
		t.Fatalf("list returned %v, want the one blob %s", objects, digest)
	}
}

// A listing longer than one S3 page (1,000 keys) comes back whole and in
// digest order, and the cursor resumes after the digest it is given.
func TestS3ListPagesPastOneThousandKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := bind(t, newS3(t, "manykeys", ""), "repomany.example.com")
	const n = 1003
	var wg sync.WaitGroup
	errs := make(chan error, n)
	sem := make(chan struct{}, 32)
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			data := fmt.Appendf(nil, "blob %d", i)
			if err := s.Put(ctx, digestOf(data), int64(len(data)), strings.NewReader(string(data))); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("put: %v", err)
	}
	all, err := s.List(ctx, "", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != n {
		t.Fatalf("listed %d objects, want %d", len(all), n)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Digest >= all[i].Digest {
			t.Fatalf("listing out of order at %d: %s then %s", i, all[i-1].Digest, all[i].Digest)
		}
	}
	rest, err := s.List(ctx, all[999].Digest, 0)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(rest) != n-1000 || rest[0].Digest != all[1000].Digest {
		t.Fatalf("listing after the 1000th returned %d objects starting %v, want %d starting %s", len(rest), rest, n-1000, all[1000].Digest)
	}
}

// A bucket that is not there is a configuration error, never "no bytes":
// reading it as ErrNotStored would report every blob missing rather than
// refuse.
func TestS3AbsentBucketIsAnErrorNotAnAbsence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b, err := blobbytes.NewS3(s3Config(t, "nevermade", ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Check(ctx); err == nil {
		t.Fatal("Check passed against a bucket that does not exist")
	}
	s := bind(t, b, "repobucketless.example.com")
	if _, err := s.Open(ctx, digestOf([]byte("x"))); err == nil || errors.Is(err, blobbytes.ErrNotStored) {
		t.Fatalf("open in an absent bucket: %v, want an error that is not ErrNotStored", err)
	}
}

// Bad credentials are an error on every call, never an absence.
func TestS3RefusedCredentialsAreAnErrorNotAnAbsence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	newS3(t, "badcreds", "")
	cfg := s3Config(t, "badcreds", "")
	cfg.SecretAccessKey = "not-the-secret"
	b, err := blobbytes.NewS3(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Check(ctx); err == nil {
		t.Fatal("Check passed with a wrong secret")
	}
	s := bind(t, b, "repobadcreds.example.com")
	if held, err := s.Exists(ctx, digestOf([]byte("x"))); err == nil {
		t.Fatalf("exists with a wrong secret = %v and no error", held)
	}
	if _, err := s.Open(ctx, digestOf([]byte("x"))); err == nil || errors.Is(err, blobbytes.ErrNotStored) {
		t.Fatalf("open with a wrong secret: %v, want an error that is not ErrNotStored", err)
	}
}

// A streaming read is cut by its context: a caller that gives up mid-copy
// ends the transfer and is told so.
func TestS3OpenIsCanceledWithItsContext(t *testing.T) {
	t.Parallel()
	s := bind(t, newS3(t, "cancel", ""), "repocancel.example.com")
	data := []byte(strings.Repeat("x", 4<<20))
	digest := put(t, s, data)
	ctx, cancel := context.WithCancel(context.Background())
	rc, err := blobbytes.OpenVerified(ctx, s, digest, int64(len(data)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = rc.Close() }()
	buf := make([]byte, 1024)
	if _, err := rc.Read(buf); err != nil {
		t.Fatalf("first read: %v", err)
	}
	cancel()
	var readErr error
	for readErr == nil {
		_, readErr = rc.Read(buf)
	}
	if !errors.Is(readErr, context.Canceled) {
		t.Fatalf("read after cancel ended with %v, want context.Canceled", readErr)
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
