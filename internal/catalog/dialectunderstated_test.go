package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/authority"
)

// understatedFixture is a publisher of example.com/notes at two versions:
// version 1 uses no key a dialect introduced, and version 2's header carries
// `stability`, a key dialect 3 introduced, with header2 added to its header
// and listed at the dialect given.
func understatedFixture(t *testing.T, header2 string, listed int) *Catalog {
	t.Helper()
	dir := t.TempDir()
	p := authority.AuthorityPackage{Package: "notes"}
	for v, body := range []string{
		publishedClosure("example.com/notes", 1),
		strings.Replace(publishedClosure("example.com/notes", 2), "  version: 2\n", "  version: 2\n"+header2+"  stability: stable\n", 1),
	} {
		name := filepath.Join(dir, "v"+string(rune('1'+v))+".yaml")
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		entry := authority.AuthorityPackageVersion{Version: v + 1, Closure: filepath.Base(name), SHA256: hex.EncodeToString(sum[:])}
		if v == 1 {
			entry.Dialect = listed
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

// A release that understates the dialect it needs, in its header or in its
// publisher's listing, would send a server older than its keys to fetch it
// and stop on a key it does not know, instead of passing it over for the
// older release beside it, which that server reads (decision 0268). So it is
// a broken release: refused as such, naming the key or both numbers, and
// neither taken as readable nor passed over as newer, by this server too.
// Listed at what it needs, the same closure is the answer.
func TestAnUnderstatedReleaseIsRefusedAsBroken(t *testing.T) {
	for _, c := range []struct {
		name, header string
		want         string
	}{
		{
			"the header states less than its keys need", "  dialect: 1\n",
			"the package states dialect 1, and its declarations use `dialect`, which dialect 3 introduced",
		},
		{
			"the listing states less than the closure needs", "",
			"version 2 is listed at vocabulary dialect 1, and its closure needs dialect 3",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			cat := understatedFixture(t, c.header, 1)
			_, err := cat.Resolve(context.Background(), "example.com/notes", 0)
			if !errors.Is(err, ErrNotPublished) || errors.Is(err, ErrNewerDialect) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("resolve = %v, want the understated version 2 refused as a broken release: %q", err, c.want)
			}
		})
	}
	cat := understatedFixture(t, "", 3)
	if b, err := cat.Resolve(context.Background(), "example.com/notes", 0); err != nil || b.Version != 2 || b.NeedsDialect() != 3 {
		t.Fatalf("resolve = %+v %v, want version 2, which needs dialect 3", b, err)
	}
}
