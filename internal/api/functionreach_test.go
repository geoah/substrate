package api

import (
	"testing"

	"github.com/geoah/substrate/internal/scope"
)

func TestMissingKindReachRequiresExactScope(t *testing.T) {
	const missing = "absent.example/data/secret"
	for _, pattern := range []string{"absent.example/data/*", missing} {
		grant, err := scope.NewGrant([]string{"read:" + pattern})
		if err != nil {
			t.Fatal(err)
		}
		got := uncoveredReach(appReach{grant}, functionReach{reads: []string{missing}}, nil)
		if (len(got) == 0) != (pattern == missing) {
			t.Fatalf("%s covers missing kind: %v", pattern, got)
		}
	}
}
