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

func TestEnvelopesCarryTheAuthority(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		envelope map[string]any
	}{
		{"a record delivery", Envelope(substrate.Change{Seq: 1, Op: substrate.OpPut}, nil, "geoah.example.com")},
		{"a fire", FireEnvelope("hook-1", time.Unix(0, 0), "geoah.example.com")},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo, ok := c.envelope["repository"].(map[string]any)
			if !ok {
				t.Fatalf("repository binding = %v", c.envelope["repository"])
			}
			if repo["authority"] != "geoah.example.com" {
				t.Fatalf("repository = %v, want the authority", repo)
			}
		})
	}
}

// A fire's `at` keeps the fraction of a second it was taken at: a stored
// webhook request's millisecond signature timestamp is measured against it.
func TestFireEnvelopeKeepsFractionalSeconds(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 10, 11, 12, 0, 0, 900_000_000, time.UTC), "2026-10-11T12:00:00.9Z"},
		{time.Date(2026, 10, 11, 12, 0, 0, 123_456_000, time.FixedZone("", 3*3600)), "2026-10-11T09:00:00.123456Z"},
		{time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC), "2026-10-11T12:00:00Z"},
	} {
		fire, _ := FireEnvelope("hook-1", c.at, "geoah.example.com")["fire"].(map[string]any)
		if fire["at"] != c.want {
			t.Errorf("FireEnvelope(%s) fire.at = %v, want %s", c.at, fire["at"], c.want)
		}
	}
}
