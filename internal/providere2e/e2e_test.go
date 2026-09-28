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

// The providers. Each name is a directory under providers/ and under
// fixtures/, and a directory under kinds/providers.substrate.reamde.dev/. The
// cases run side by side (see TestProviderE2E), so the order is only the
// order they start in.
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

// TestProviderE2E runs every provider's case against one server, all at once.
//
// The cases share nothing that would make them wait on each other: each one
// registers its own repository, its mock listens on the port its e2e.json
// pins (distinct per provider), its substratectl contexts live in a config
// file of its own, and the runner and the scenario read and write that
// repository alone. What they share is the server, whose dispatcher runs one
// pass per repository at a time and caps the passes at eight, above the seven
// here. Run sequentially the suite was the sum of its cases, ten minutes,
// most of it the slowest three waiting on their own syncs; side by side it is
// the slowest case alone.
//
// The registrations are made here, before the cases start, so a door refusal
// is one failure on the parent rather than seven. The auth gate admits them
// back to back: its per-peer and per-repository buckets key on the repository
// name, which differs per case, and the global bucket holds thirty-two.
func TestProviderE2E(t *testing.T) {
	requirePython(t)
	requireUV(t)

	srv := startServer(t)

	cases := make([]providerCase, 0, len(providers))
	for _, provider := range providers {
		cases = append(cases, newProviderCase(t, srv, provider))
	}
	for _, c := range cases {
		t.Run(c.provider, func(t *testing.T) {
			t.Parallel()
			runProvider(t, srv, c)
		})
	}
}

// providerCase is one provider's repository: the authority it registered as,
// its bearer and the substratectl config that holds its context.
type providerCase struct {
	provider  string
	authority string
	token     string
	ctlConfig string
}

// newProviderCase registers the case's repository. The config file is the
// case's own: substratectl rewrites the whole file on every context change,
// so seven cases sharing one would race each other's writes.
func newProviderCase(t *testing.T, srv *server, provider string) providerCase {
	t.Helper()
	c := providerCase{
		provider:  provider,
		authority: fmt.Sprintf("e2e-%s.localhost", provider),
		ctlConfig: filepath.Join(srv.dir, provider+"-substratectl.yaml"),
	}
	c.token = srv.register(t, c.authority, c.ctlConfig)
	return c
}

// runProvider is one case: the runner over the case's repository.
func runProvider(t *testing.T, srv *server, c providerCase) {
	t.Helper()
	provider := c.provider
	cfg := readProviderConfig(t, provider)

	logPath := filepath.Join(srv.logDir, provider+".log")
	budget := caseBudget(cfg.SettleSeconds)

	args := []string{
		filepath.Join(srv.suiteDir, "runner", "e2e.py"), provider,
		"--mode", "e2e",
		"--server", srv.baseURL,
		"--authority", c.authority,
		"--ctl", srv.substratectl,
		"--recordings", filepath.Join(srv.suiteDir, "fixtures", provider),
	}
	cmd := exec.Command(srv.python3, args...) //nolint:gosec // every argument is the suite's own
	cmd.Dir = srv.suiteDir
	cmd.Env = append(os.Environ(),
		// The bearer goes through the environment and never through argv:
		// `ps` shows a command line to every process on the box.
		"SUBSTRATE_TOKEN="+c.token,
		"SUBSTRATE_E2E_ROOT="+srv.suiteDir,
		"SUBSTRATE_E2E_REPO="+srv.repoRoot,
		// The contexts substratectl reads and writes are this case's own.
		"SUBSTRATECTL_CONFIG="+c.ctlConfig,
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
