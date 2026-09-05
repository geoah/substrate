package runner

// The envelope's `repository` binding carries both names a repository has:
// `owner`, the username that logs in, and `authority`, the name it publishes
// under. A `when:` guard and a function body read this map, so dropping
// either name is a break nothing else would catch.

import (
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func TestEnvelopesCarryOwnerAndAuthority(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		envelope map[string]any
	}{
		{"a record delivery", Envelope(substrate.Change{Seq: 1, Op: substrate.OpPut}, nil, "geoah", "geoah.example.com")},
		{"a fire", FireEnvelope("hook-1", time.Unix(0, 0), "geoah", "geoah.example.com")},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo, ok := c.envelope["repository"].(map[string]any)
			if !ok {
				t.Fatalf("repository binding = %v", c.envelope["repository"])
			}
			if repo["owner"] != "geoah" || repo["authority"] != "geoah.example.com" {
				t.Fatalf("repository = %v, want both the username and the authority", repo)
			}
		})
	}
}
