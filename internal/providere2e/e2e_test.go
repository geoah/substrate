// Package providere2e runs the shipped provider bundles end to end against a
// substrate the suite starts itself and a mock upstream replaying recorded
// traffic.
//
// One case per provider. Each one registers a fresh repository, hands the
// Python runner under runner/ the server, the repository's bearer and a
// substratectl, and the runner does the rest: it starts the mock over
// fixtures/<provider>, rewires a COPY of
// kinds/providers.substrate.reamde.dev/<provider>/*.yaml at that mock,
// applies the copy, writes the config and account records, completes the
// OAuth dance (or writes a token, per providers/<provider>/e2e.json), waits
// for the on-connect trigger's runs to settle, and then runs
// providers/<provider>/scenario.py, which is where the assertions live. A
// non-zero exit from the runner fails the case.
//
// The bundles in git are never edited: the rewire lands in a temp copy, the
// same seam internal/providertest uses.
//
// internal/providertest is the other half of this. It drives ONE callable
// through the engine with a hand-written fake, in seconds; this drives the
// whole closure through a real server over recorded traffic, in minutes.
package providere2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/testdb"
)

// The suite's own constants. The invite code and the password are the door's;
// the state key signs OAuth flow state and only has to be stable for the life
// of one server.
const (
	inviteCode    = "providere2e-invite"
	repoPassword  = "providere2e-password"
	oauthStateKey = "providere2e-oauth-state-key"
)

// The providers, in the order the suite runs them: cheapest recordings first,
// so a harness fault shows up in the first minute rather than the twentieth.
// Each name is a directory under providers/ and under fixtures/, and a
// directory under kinds/providers.substrate.reamde.dev/.
var providers = []string{"whoop", "linear", "notion", "beeper", "github", "google", "slack"}

// caseBudget is what one provider's run gets before the harness kills it. The
// runner waits `settleSeconds` from e2e.json for the triggers to go quiet and
// may do that several times over (a bounded drain continues), so the budget is
// that number plus a flat margin rather than a multiple of it. Tripping it is
// a failure, not a skip: a sync that has not converged in this long is stuck.
func caseBudget(settleSeconds int) time.Duration {
	return time.Duration(settleSeconds)*2*time.Second + 15*time.Minute
}

func TestMain(m *testing.M) {
	// testdb.Main: the data roots on tmpfs, the run, then every database the
	// run made dropped.
	os.Exit(testdb.Main(m))
}

func TestProviderE2E(t *testing.T) {
	requirePython(t)
	requireUV(t)

	srv := startServer(t)

	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			runProvider(t, srv, provider)
		})
	}
}

// runProvider is one case: a fresh repository, then the runner over it.
func runProvider(t *testing.T, srv *server, provider string) {
	t.Helper()
	cfg := readProviderConfig(t, provider)
	authority := fmt.Sprintf("e2e-%s.localhost", provider)
	token := srv.register(t, authority)

	logPath := filepath.Join(srv.logDir, provider+".log")
	budget := caseBudget(cfg.SettleSeconds)

	args := []string{
		filepath.Join(srv.suiteDir, "runner", "e2e.py"), provider,
		"--mode", "e2e",
		"--server", srv.baseURL,
		"--authority", authority,
		"--ctl", srv.substratectl,
		"--recordings", filepath.Join(srv.suiteDir, "fixtures", provider),
	}
	cmd := exec.Command(srv.python3, args...) //nolint:gosec // every argument is the suite's own
	cmd.Dir = srv.suiteDir
	cmd.Env = append(os.Environ(),
		// The bearer goes through the environment and never through argv:
		// `ps` shows a command line to every process on the box.
		"SUBSTRATE_TOKEN="+token,
		"SUBSTRATE_E2E_ROOT="+srv.suiteDir,
		"SUBSTRATE_E2E_REPO="+srv.repoRoot,
		// The contexts substratectl reads and writes are this run's own.
		"SUBSTRATECTL_CONFIG="+srv.ctlConfig,
		"PATH="+srv.path,
		// The mock, the scenario and the runner all print progress; buffered,
		// a case that trips its budget would show nothing.
		"PYTHONUNBUFFERED=1",
	)

	started := time.Now()
	out, err := runWithBudget(t, cmd, budget)
	elapsed := time.Since(started)

	if werr := os.WriteFile(logPath, out, 0o600); werr != nil {
		t.Logf("writing %s: %v", logPath, werr)
	}
	t.Logf("%s: %s (%d recordings, settle %ds); output in %s",
		provider, elapsed.Round(time.Second), countRecordings(t, srv, provider),
		cfg.SettleSeconds, logPath)
	t.Log("\n" + string(out))
	if err != nil {
		t.Fatalf("%s: runner/e2e.py %s failed after %s: %v",
			provider, provider, elapsed.Round(time.Second), err)
	}
}

// providerConfig is the part of providers/<p>/e2e.json this side reads. The
// runner reads the rest; the two must agree only about the port and the wait.
type providerConfig struct {
	MockPort      int `json:"mockPort"`
	SettleSeconds int `json:"settleSeconds"`
}

func readProviderConfig(t *testing.T, provider string) providerConfig {
	t.Helper()
	path := filepath.Join("providers", provider, "e2e.json")
	raw, err := os.ReadFile(path) //nolint:gosec // a path built from the suite's own table
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var cfg providerConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if cfg.SettleSeconds == 0 {
		cfg.SettleSeconds = 120 // the runner's own default
	}
	if cfg.MockPort == 0 {
		t.Fatalf("%s names no mockPort: the port is pinned per provider so the "+
			"rewired function body is identical run to run", path)
	}
	// The port is pinned, so a second run of this suite on the same box would
	// serve one provider's recordings to the other's sync. The runner reaps an
	// orphan mock it left behind itself; a LIVE one is somebody else's run.
	requireMockPortFree(t, provider, cfg.MockPort)
	return cfg
}

func countRecordings(t *testing.T, srv *server, provider string) int {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(srv.suiteDir, "fixtures", provider, "*.json"))
	if err != nil {
		return 0
	}
	return len(names)
}
