package providertest

import (
	"fmt"
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
	code := testdb.Main(m)
	os.Exit(reportGated("providertest", gated.Load(), code))
}

// reportGated says how many cases ran past their gate, on stdout (seen with
// -v or on a failure) and in the CI job's step summary where there is one,
// and fails a run that must not skip (requiredEnv) in which none did.
func reportGated(suite string, n int64, code int) int {
	line := fmt.Sprintf("%s: %d cases ran past the uv and python3 gates", suite, n)
	fmt.Println(line)
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, _ = fmt.Fprintln(f, line)
			_ = f.Close()
		}
	}
	if suiteRequired && n == 0 && code == 0 {
		fmt.Printf("%s: %s is set and no case ran\n", suite, requiredEnv)
		return 1
	}
	return code
}
