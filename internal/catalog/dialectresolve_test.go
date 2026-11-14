package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/vocabulary"
)

// dialectFixture is a publisher whose document lists the given versions of
// example.com/notes, each with the dialect its closure's header states, and
// the document listing it too where listed is true. A dialect of 0 states
// none.
func dialectFixture(t *testing.T, dialects map[int]int, listed bool) *Catalog {
	t.Helper()
	dir := t.TempDir()
	p := authority.AuthorityPackage{Package: "notes"}
	for v := 1; v <= len(dialects); v++ {
		body := publishedClosure("example.com/notes", v)
		if d := dialects[v]; d != 0 {
			body = strings.Replace(body, fmt.Sprintf("  version: %d\n", v), fmt.Sprintf("  version: %d\n  dialect: %d\n", v, d), 1)
		}
		sum := sha256.Sum256([]byte(body))
		name := fmt.Sprintf("v%d.yaml", v)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		entry := authority.AuthorityPackageVersion{Version: v, Closure: name, SHA256: hex.EncodeToString(sum[:])}
		if listed {
			entry.Dialect = dialects[v]
		}
		p.Versions = append(p.Versions, entry)
	}
	raw, _ := json.Marshal(authority.AuthorityDocument{Authority: "example.com", Packages: []authority.AuthorityPackage{p}})
	path := filepath.Join(dir, "authority.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"example.com": path}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// THE DONE-WHEN, at the resolver every door asks: a publisher lists two
// versions, the newer needs a dialect this server lacks, and the older is the
// one resolved. Asked for the newer alone, the refusal names the dialect.
func TestTheResolverTakesTheNewestVersionThisServerCanRead(t *testing.T) {
	newer := int(vocabulary.Dialect) + 1
	c := dialectFixture(t, map[int]int{1: int(vocabulary.Dialect), 2: newer}, true)
	b, err := c.Resolve(context.Background(), "example.com/notes", 0)
	if err != nil || b.Version != 1 {
		t.Fatalf("resolve = %+v %v, want version 1, the newest this server reads", b, err)
	}
	if b.Dialect() != vocabulary.Dialect {
		t.Fatalf("the closure's dialect = %d, want %d", b.Dialect(), vocabulary.Dialect)
	}
	_, err = c.Resolve(context.Background(), "example.com/notes", 2)
	if !errors.Is(err, ErrNotPublished) || !strings.Contains(err.Error(), fmt.Sprintf("version 2: the package needs vocabulary dialect %d", newer)) {
		t.Fatalf("resolve at 2 = %v, want it refused naming the dialect", err)
	}
}

// A version that states no dialect is read, as every version listed before
// the field was, and its closure's header is the check.
func TestAVersionThatStatesNoDialectIsRead(t *testing.T) {
	c := dialectFixture(t, map[int]int{1: 0, 2: 0}, true)
	if b, err := c.Resolve(context.Background(), "example.com/notes", 0); err != nil || b.Version != 2 {
		t.Fatalf("resolve = %+v %v, want version 2", b, err)
	}
}

// A version whose document lists no dialect is fetched, and passed over when
// its header states a newer one: the older version is resolved, and asked
// for the newer alone, the refusal names its dialect.
func TestAVersionWhoseHeaderNeedsANewerDialectIsPassedOver(t *testing.T) {
	newer := int(vocabulary.Dialect) + 1
	c := dialectFixture(t, map[int]int{1: 0, 2: newer}, false)
	b, err := c.Resolve(context.Background(), "example.com/notes", 0)
	if err != nil || b.Version != 1 {
		t.Fatalf("resolve = %+v %v, want version 1", b, err)
	}
	_, err = c.Resolve(context.Background(), "example.com/notes", 2)
	if !errors.Is(err, ErrNotPublished) || !errors.Is(err, ErrNewerDialect) ||
		!strings.Contains(err.Error(), fmt.Sprintf("the package needs vocabulary dialect %d", newer)) {
		t.Fatalf("resolve at 2 = %v, want it refused naming the dialect", err)
	}
}

// The fallback passes over a version for its dialect alone: a fetched
// closure whose header disagrees with the version it is listed at is the
// broken release it is, and the answer, however new its dialect.
func TestAMismatchedVersionIsNotPassedOverForItsDialect(t *testing.T) {
	newer := int(vocabulary.Dialect) + 1
	dir := t.TempDir()
	write := func(name, body string) authority.AuthorityPackageVersion {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		return authority.AuthorityPackageVersion{Closure: name, SHA256: hex.EncodeToString(sum[:])}
	}
	v1 := write("v1.yaml", publishedClosure("example.com/notes", 1))
	v1.Version = 1
	// Listed at 2, its header says 1, and it states the next dialect.
	v2 := write("v2.yaml", strings.Replace(publishedClosure("example.com/notes", 1), "  version: 1\n", fmt.Sprintf("  version: 1\n  dialect: %d\n", newer), 1))
	v2.Version = 2
	raw, _ := json.Marshal(authority.AuthorityDocument{Authority: "example.com", Packages: []authority.AuthorityPackage{{Package: "notes", Versions: []authority.AuthorityPackageVersion{v1, v2}}}})
	path := filepath.Join(dir, "authority.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"example.com": path}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Resolve(context.Background(), "example.com/notes", 0)
	if err == nil || errors.Is(err, ErrNewerDialect) || !strings.Contains(err.Error(), "does not match listed version 2") {
		t.Fatalf("resolve = %v, want the mismatched version 2 refused as such", err)
	}
}
