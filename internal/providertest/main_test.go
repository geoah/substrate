package providertest

import (
	"os"
	"testing"

	"github.com/geoah/substrate/internal/testdb"
)

// Two gates refuse loopback by default, and every suite here leans on both
// through its httptest fake. The sandbox connect gate refuses a
// network-granted body's dial
// (0035-a-network-body-connect-is-filtered-by-destination), and the #241
// server-side gate refuses the engine's own dial of a chosen base URL. A real
// deployment's Postgres and metadata endpoint sit in exactly those ranges, so
// the escape each gate documents is taken here rather than widened there. Set
// before m.Run, so the runner reads the sandbox one when it starts the first
// body.
func TestMain(m *testing.M) {
	if os.Getenv("SUBSTRATE_SANDBOX_EGRESS_ALLOW") == "" {
		_ = os.Setenv("SUBSTRATE_SANDBOX_EGRESS_ALLOW", "127.0.0.0/8,::1/128")
	}
	if os.Getenv("SUBSTRATE_EGRESS_ALLOW") == "" {
		_ = os.Setenv("SUBSTRATE_EGRESS_ALLOW", "127.0.0.0/8,::1/128")
	}
	// testdb.Main: the data roots on tmpfs, the run, then every database the
	// run made dropped (the migrated template among them).
	os.Exit(testdb.Main(m))
}
