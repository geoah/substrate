package blobbytes_test

// One contract, three backends. Everything a Store promises is asserted here
// and each backend's own file runs the whole set, so `fs` and `s3` cannot
// quietly mean something else by Put, Delete or List than `postgres` does.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/blobbytes"
)

// digestOf is the engine's digest, spelled here so the tests do not import it.
func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "blob-sha256-" + hex.EncodeToString(sum[:])
}

// openStore binds a backend to one repository, for a test that does not care
// which backend it got.
type openStore func(t *testing.T, repository string) blobbytes.Store

func put(t *testing.T, s blobbytes.Store, data []byte) string {
	t.Helper()
	digest := digestOf(data)
	if err := s.Put(context.Background(), digest, int64(len(data)), strings.NewReader(string(data))); err != nil {
		t.Fatalf("put: %v", err)
	}
	return digest
}

func read(t *testing.T, s blobbytes.Store, digest string, size int) []byte {
	t.Helper()
	data, err := blobbytes.ReadAll(context.Background(), s, digest, int64(size))
	if err != nil {
		t.Fatalf("read %s: %v", digest, err)
	}
	return data
}

// conformance is the whole Store contract.
func conformance(t *testing.T, open openStore) {
	ctx := context.Background()

	t.Run("absent bytes are ErrNotStored", func(t *testing.T) {
		s := open(t, "repoabsent")
		digest := digestOf([]byte("never stored"))
		if _, err := s.Open(ctx, digest); !errors.Is(err, blobbytes.ErrNotStored) {
			t.Fatalf("open absent: got %v, want ErrNotStored", err)
		}
		held, err := s.Exists(ctx, digest)
		if err != nil {
			t.Fatalf("exists absent: %v", err)
		}
		if held {
			t.Fatal("exists reported bytes that were never stored")
		}
	})

	t.Run("put then read the same bytes", func(t *testing.T) {
		s := open(t, "reporoundtrip")
		data := []byte("the bytes, whole")
		digest := put(t, s, data)
		held, err := s.Exists(ctx, digest)
		if err != nil || !held {
			t.Fatalf("exists after put: %v %v", held, err)
		}
		if got := read(t, s, digest, len(data)); string(got) != string(data) {
			t.Fatalf("read back %q, stored %q", got, data)
		}
	})

	t.Run("a second put of the same digest is a no-op", func(t *testing.T) {
		s := open(t, "repodedup")
		data := []byte("stored twice, held once")
		digest := put(t, s, data)
		put(t, s, data)
		if got := read(t, s, digest, len(data)); string(got) != string(data) {
			t.Fatalf("read back %q after a re-put, stored %q", got, data)
		}
		objects, err := s.List(ctx, "", 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(objects) != 1 {
			t.Fatalf("a re-put of the same bytes left %d objects, want 1", len(objects))
		}
	})

	t.Run("delete is idempotent", func(t *testing.T) {
		s := open(t, "repodelete")
		digest := put(t, s, []byte("here, then gone"))
		if err := s.Delete(ctx, digest); err != nil {
			t.Fatalf("delete: %v", err)
		}
		held, err := s.Exists(ctx, digest)
		if err != nil {
			t.Fatalf("exists after delete: %v", err)
		}
		if held {
			t.Fatal("the bytes survived their delete")
		}
		// The sweep runs again over what it already collected, so deleting
		// bytes that are gone must succeed rather than report a failure the
		// caller would have to special-case.
		if err := s.Delete(ctx, digest); err != nil {
			t.Fatalf("second delete: %v", err)
		}
	})

	t.Run("list reports size, orders by digest and honors the cursor", func(t *testing.T) {
		s := open(t, "repolist")
		var digests []string
		for _, body := range []string{"one", "two", "three", "four"} {
			digests = append(digests, put(t, s, []byte(body)))
		}
		objects, err := s.List(ctx, "", 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(objects) != len(digests) {
			t.Fatalf("listed %d objects, stored %d", len(objects), len(digests))
		}
		for i := 1; i < len(objects); i++ {
			if objects[i-1].Digest >= objects[i].Digest {
				t.Fatalf("list is not in ascending digest order: %s then %s", objects[i-1].Digest, objects[i].Digest)
			}
		}
		for _, o := range objects {
			if o.Size <= 0 {
				t.Fatalf("object %s listed size %d", o.Digest, o.Size)
			}
			if o.At.IsZero() {
				t.Fatalf("object %s listed no time", o.Digest)
			}
		}
		// The cursor is what lets the orphan sweep walk a store bigger than
		// one batch instead of re-reading its first page forever.
		page, err := s.List(ctx, objects[0].Digest, 2)
		if err != nil {
			t.Fatalf("list after cursor: %v", err)
		}
		if len(page) != 2 {
			t.Fatalf("a limit of 2 returned %d objects", len(page))
		}
		if page[0].Digest != objects[1].Digest {
			t.Fatalf("after %s the next object is %s, want %s", objects[0].Digest, page[0].Digest, objects[1].Digest)
		}
		last, err := s.List(ctx, objects[len(objects)-1].Digest, 0)
		if err != nil {
			t.Fatalf("list past the end: %v", err)
		}
		if len(last) != 0 {
			t.Fatalf("listing past the last digest returned %d objects", len(last))
		}
	})

	t.Run("a key that is not a digest is refused", func(t *testing.T) {
		s := open(t, "repogrammar")
		for _, bad := range []string{
			"",
			"../escape",
			"blob-sha256-short",
			"blob-sha256-" + strings.Repeat("Z", 64),
			"blob-sha256-" + strings.Repeat("a", 64) + "/x",
		} {
			if err := s.Put(ctx, bad, 1, strings.NewReader("x")); err == nil {
				t.Fatalf("put accepted %q as a digest", bad)
			}
			if _, err := s.Open(ctx, bad); err == nil {
				t.Fatalf("open accepted %q as a digest", bad)
			}
			if _, err := s.Exists(ctx, bad); err == nil {
				t.Fatalf("exists accepted %q as a digest", bad)
			}
			if err := s.Delete(ctx, bad); err == nil {
				t.Fatalf("delete accepted %q as a digest", bad)
			}
		}
	})

	t.Run("bytes that are not the promised length are refused", func(t *testing.T) {
		s := open(t, "repolength")
		data := []byte("nine byte")
		if err := s.Put(ctx, digestOf(data), int64(len(data))+5, strings.NewReader(string(data))); err == nil {
			t.Fatal("put accepted a body shorter than the size it was given")
		}
	})
}

// repositoryIsolation is the fs and s3 case: row level security does not reach
// either, so the repository half of the key is what keeps two repositories
// apart. The postgres backend does not run this — its isolation is the row
// level security policy, which internal/engine's isolation suite owns.
func repositoryIsolation(t *testing.T, open openStore) {
	ctx := context.Background()
	data := []byte("one repository's attachment")
	digest := digestOf(data)

	mine := open(t, "repoalpha")
	theirs := open(t, "repobeta")
	put(t, mine, data)

	held, err := theirs.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("exists in the other repository: %v", err)
	}
	if held {
		t.Fatal("a digest stored by one repository is visible to another")
	}
	if _, err := theirs.Open(ctx, digest); !errors.Is(err, blobbytes.ErrNotStored) {
		t.Fatalf("open across repositories: got %v, want ErrNotStored", err)
	}
	objects, err := theirs.List(ctx, "", 0)
	if err != nil {
		t.Fatalf("list in the other repository: %v", err)
	}
	if len(objects) != 0 {
		t.Fatalf("the other repository lists %d objects it did not store", len(objects))
	}
	// A delete in one repository leaves the other's bytes alone, which is the
	// half a shared key namespace would get wrong.
	if err := theirs.Delete(ctx, digest); err != nil {
		t.Fatalf("delete across repositories: %v", err)
	}
	if got := read(t, mine, digest, len(data)); string(got) != string(data) {
		t.Fatalf("another repository's delete removed these bytes")
	}
}

// refuseBadRepository is the other half of the key grammar: a repository id
// that could hold a path separator would let one repository's store address
// another's.
func refuseBadRepository(t *testing.T, b blobbytes.Backend, db blobbytes.DB) {
	// `.` and `..` match the id character class, and either one would address
	// the store's root or its parent instead of one repository inside it.
	for _, bad := range []string{"", ".", "..", "../elsewhere", "a/b", strings.Repeat("r", 129)} {
		if _, err := b.Repository(bad, db); err == nil {
			t.Fatalf("the backend bound to %q as a repository id", bad)
		}
	}
}
