package archive

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/archive/archivetest"
)

// conformance holds a store to what the archiver relies on (Store's comment).
// Every store runs it.
func conformance(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	put := func(key, body string) {
		t.Helper()
		if err := s.Put(ctx, key, strings.NewReader(body), int64(len(body))); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	get := func(key string) string {
		t.Helper()
		b, err := ReadAll(ctx, s, key)
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		return string(b)
	}

	if _, err := s.Get(ctx, "repositories/rnd-a/repository.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get of a key never put: %v, want ErrNotFound", err)
	}
	put("repositories/rnd-a/repository.json", `{"head":1}`)
	put("repositories/rnd-a/changelog/000000000000001-000000000000003.ndjson", "lines\n")
	put("repositories/rnd-b/repository.json", `{"head":9}`)
	if got := get("repositories/rnd-a/repository.json"); got != `{"head":1}` {
		t.Fatalf("get = %q", got)
	}
	// A put replaces the object whole.
	put("repositories/rnd-a/repository.json", `{"head":2}`)
	if got := get("repositories/rnd-a/repository.json"); got != `{"head":2}` {
		t.Fatalf("get after a second put = %q", got)
	}
	// A seeker with no declared size is measured.
	if err := s.Put(ctx, "repositories/rnd-a/sealed/secret-01.json", strings.NewReader("sealed"), -1); err != nil {
		t.Fatalf("put without a size: %v", err)
	}

	listed, err := s.List(ctx, "repositories/rnd-a/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []Object{
		{Key: "repositories/rnd-a/changelog/000000000000001-000000000000003.ndjson", Size: 6},
		{Key: "repositories/rnd-a/repository.json", Size: 10},
		{Key: "repositories/rnd-a/sealed/secret-01.json", Size: 6},
	}
	if !equalObjects(listed, want) {
		t.Fatalf("list = %+v, want %+v", listed, want)
	}
	// A prefix that is not a whole segment matches keys that start with it.
	if listed, err := s.List(ctx, "repositories/rnd-"); err != nil || len(listed) != 4 {
		t.Fatalf("list of a partial segment = %+v (%v), want the four keys", listed, err)
	}
	if listed, err := s.List(ctx, "repositories/rnd-c/"); err != nil || len(listed) != 0 {
		t.Fatalf("list of an empty prefix = %+v (%v)", listed, err)
	}

	if err := s.Delete(ctx, "repositories/rnd-a/repository.json"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(ctx, "repositories/rnd-a/repository.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, "repositories/rnd-a/repository.json"); err != nil {
		t.Fatalf("a second delete: %v", err)
	}

	for _, key := range []string{"", "/a", "a/", "a//b", "../a", "a/../b", "a/./b", "a b", "a\\b"} {
		if err := s.Put(ctx, key, strings.NewReader("x"), 1); err == nil {
			t.Errorf("put %q was accepted", key)
		}
		if _, err := s.Get(ctx, key); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("get %q: %v, want a refused key", key, err)
		}
	}
	if _, err := s.List(ctx, "../"); err == nil {
		t.Error("a listing that climbs out was accepted")
	}
}

func equalObjects(a, b []Object) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDirStore(t *testing.T) {
	root := t.TempDir()
	d, err := NewDir(root)
	if err != nil {
		t.Fatal(err)
	}
	conformance(t, d)

	// What a crashed put leaves is no object.
	ctx := context.Background()
	stray := filepath.Join(root, "repositories", "rnd-a", "repository.json"+tempMark+"1234")
	if err := os.WriteFile(stray, []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	listed, err := d.List(ctx, "repositories/rnd-a/")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range listed {
		if strings.Contains(o.Key, "~") {
			t.Fatalf("the listing reports a put's temporary file: %+v", listed)
		}
	}
	if _, err := NewDir("relative/path"); err == nil {
		t.Fatal("a relative root was accepted")
	}
}

// A link is a put that shares the bytes: the object reads back, and removing
// the source leaves it.
func TestDirStoreLink(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	d, err := NewDir(filepath.Join(root, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "blob")
	if err := os.WriteFile(src, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := "repositories/rnd-a/blobs/blob-sha256-00"
	if err := d.Link(ctx, key, src); err != nil {
		t.Fatalf("link: %v", err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	if b, err := ReadAll(ctx, d, key); err != nil || string(b) != "bytes" {
		t.Fatalf("the linked object reads %q (%v)", b, err)
	}
	if err := d.Link(ctx, "../x", src); err == nil {
		t.Fatal("a link to a refused key was accepted")
	}
}

func TestS3Store(t *testing.T) {
	fake := archivetest.NewFakeS3("archive-bucket")
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	s, err := NewS3(S3Config{
		Bucket: "archive-bucket", Prefix: "substrate/one", Endpoint: srv.URL, PathStyle: true,
		AccessKeyID: "test-access-key", SecretAccessKey: "test-secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	conformance(t, s)
	// Every key went under the prefix, and the listing pages were followed.
	fake.Lock()
	defer fake.Unlock()
	for key := range fake.Objects {
		if !strings.HasPrefix(key, "substrate/one/") {
			t.Fatalf("the object %q is not under the prefix", key)
		}
	}
	if fake.Pages < 2 {
		t.Fatalf("the listings fetched %d pages; the fake pages every two keys, so a full listing takes several", fake.Pages)
	}
	if fake.Unsigned > 0 {
		t.Fatalf("%d requests carried no signature", fake.Unsigned)
	}
	if got := s.String(); strings.Contains(got, "test-secret-key") || strings.Contains(got, "test-access-key") {
		t.Fatalf("the store's name %q carries a credential", got)
	}
}

func TestS3StoreRefusesAnIncompleteConfiguration(t *testing.T) {
	for name, cfg := range map[string]S3Config{
		"no bucket":      {AccessKeyID: "a", SecretAccessKey: "b"},
		"no credentials": {Bucket: "b"},
		"a bad prefix":   {Bucket: "b", Prefix: "../x", AccessKeyID: "a", SecretAccessKey: "b"},
	} {
		if _, err := NewS3(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// What the directory store creates and deletes is flushed before the call
// returns: every directory a put created, in its parent, and a deleted
// object, in its directory. A repository's archive is deleted before its
// rows on the strength of that delete (decision 0277), and a power loss must
// not bring the archive back.
func TestDirStoreFlushesWhatItCreatesAndDeletes(t *testing.T) {
	root := t.TempDir()
	d, err := NewDir(filepath.Join(root, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	var synced []string
	syncHook = func(dir string) { synced = append(synced, dir) }
	defer func() { syncHook = nil }()
	ctx := context.Background()
	if err := d.Put(ctx, "repositories/x/repository.json", strings.NewReader("{}"), 2); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, filepath.Join(root, "archive"), filepath.Join(root, "archive", "repositories"), filepath.Join(root, "archive", "repositories", "x")} {
		if !slices.Contains(synced, dir) {
			t.Fatalf("a put did not flush %s, which gained an entry: flushed %v", dir, synced)
		}
	}
	synced = nil
	if err := d.Delete(ctx, "repositories/x/repository.json"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(synced, filepath.Join(root, "archive", "repositories", "x")) {
		t.Fatalf("a delete did not flush the directory it removed from: flushed %v", synced)
	}
}

// A bucket that takes the connection and never answers fails every call
// within the bounds, rather than holding its caller.
func TestBoundedFailsTheCallsOfAStoreThatNeverAnswers(t *testing.T) {
	fake := archivetest.NewFakeS3("archive-bucket")
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s3, err := NewS3(S3Config{Bucket: "archive-bucket", Endpoint: srv.URL, PathStyle: true, AccessKeyID: "test-access-key", SecretAccessKey: "test-secret-key"})
	if err != nil {
		t.Fatal(err)
	}
	store := Bounded(s3, 300*time.Millisecond, 300*time.Millisecond)
	ctx := context.Background()
	if err := store.Put(ctx, "repositories/x/a", strings.NewReader("kept"), 4); err != nil {
		t.Fatal(err)
	}
	fake.SetStall(true)
	defer fake.SetStall(false)
	for name, call := range map[string]func() error{
		"list":   func() error { _, err := store.List(ctx, "repositories/"); return err },
		"get":    func() error { _, err := store.Get(ctx, "repositories/x/a"); return err },
		"put":    func() error { return store.Put(ctx, "repositories/x/b", strings.NewReader("more"), 4) },
		"delete": func() error { return store.Delete(ctx, "repositories/x/a") },
	} {
		start := time.Now()
		if err := call(); err == nil {
			t.Errorf("%s against a store that never answers succeeded", name)
		}
		if took := time.Since(start); took > 10*time.Second {
			t.Errorf("%s took %s, past its bound", name, took)
		}
	}
}
