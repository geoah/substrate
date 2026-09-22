package providere2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
)

// --- the gates ----------------------------------------------------------------

// requireUV skips a run that cannot warm a PEP 723 body. Every provider
// closure ships at least one, so the apply itself is what wants uv.
func requireUV(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the closure's body warms through uv at install")
	}
}

// requirePython skips a run with no interpreter: the runner, the mock and
// every scenario are Python.
func requirePython(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH — the runner, the mock and the scenarios are Python")
	}
}

// --- the server ---------------------------------------------------------------

// server is the substrate this suite starts and every case shares: one
// process, one database, one data root, and the paths the runner needs.
type server struct {
	dir          string // scratch: the binaries, the CLI config, one log per case
	suiteDir     string // internal/providere2e, absolute
	repoRoot     string // the checkout, absolute
	baseURL      string
	substratectl string
	python3      string
	path         string // the PATH substrated and the runner are given
	ctlConfig    string // SUBSTRATECTL_CONFIG: the contexts are this run's own
	// logDir outlives the run: <checkout>/.dev/providere2e, gitignored, one
	// file per case plus the server's own. t.TempDir() is deleted the moment
	// the test ends, and the log of a failed sync is the whole diagnosis.
	logDir string

	cmd    *exec.Cmd
	logs   *os.File
	logDst string
}

func startServer(t *testing.T) *server {
	t.Helper()

	suiteDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolving the suite directory: %v", err)
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the checkout: %v", err)
	}
	dir := t.TempDir()

	logDir := filepath.Join(repoRoot, ".dev", "providere2e")
	if err := os.RemoveAll(logDir); err != nil {
		t.Fatalf("clearing %s: %v", logDir, err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", logDir, err)
	}

	srv := &server{
		dir:          dir,
		suiteDir:     suiteDir,
		repoRoot:     repoRoot,
		substratectl: filepath.Join(dir, "substratectl"),
		ctlConfig:    filepath.Join(dir, "substratectl.yaml"),
		logDir:       logDir,
		logDst:       filepath.Join(logDir, "substrated.log"),
	}
	srv.python3, srv.path = toolchain(t)

	build(t, repoRoot, filepath.Join(dir, "substrated"), "./cmd/substrated")
	build(t, repoRoot, srv.substratectl, "./cmd/substratectl")

	dsn := testdb.NewSchema(t)
	port := freePort(t)
	srv.baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	logs, err := os.Create(srv.logDst) //nolint:gosec // a path under the test's own temp dir
	if err != nil {
		t.Fatalf("creating the server log: %v", err)
	}
	srv.logs = logs

	dataRoot := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		t.Fatalf("creating the data root: %v", err)
	}

	cmd := exec.Command(filepath.Join(dir, "substrated")) //nolint:gosec // the binary this test just built
	cmd.Dir = repoRoot
	cmd.Stdout, cmd.Stderr = logs, logs
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(),
		"DATABASE_URL="+dsn,
		fmt.Sprintf("PORT=%d", port),
		"SUBSTRATE_DATA_ROOT="+dataRoot,
		"SUBSTRATE_INVITE_CODE="+inviteCode,
		// The runner registers over HTTP with a password and nothing else;
		// enrolling an authenticator for a repository this run deletes would
		// be friction with nothing behind it.
		"SUBSTRATE_INSECURE_DISABLE_TOTP=true",
		"SUBSTRATE_CREDENTIAL_KEY="+testenv.MintCredentialKey(),
		// The host OAuth facility. The mock serves the consent redirect, so
		// the callback has to be this server's own.
		fmt.Sprintf("SUBSTRATE_OAUTH_CALLBACK_URL=http://127.0.0.1:%d/api/v1/oauth/callback", port),
		"SUBSTRATE_OAUTH_STATE_KEY="+oauthStateKey,
		// Two gates refuse loopback by default and the mock is on loopback:
		// the sandbox connect gate refuses a network-granted body's dial, and
		// the server-side gate refuses the engine's own dial of a chosen base
		// URL. A real deployment's Postgres and metadata endpoint sit in these
		// ranges, so the escape each gate documents is taken here rather than
		// widened there.
		"SUBSTRATE_EGRESS_ALLOW=127.0.0.0/8,::1/128",
		"SUBSTRATE_SANDBOX_EGRESS_ALLOW=127.0.0.0/8,::1/128",
		"WEB_DIR=",
		"LOG_LEVEL=info",
		"PATH="+srv.path,
		// The runner's uv cache, keyed by the interpreter it was built on. See
		// toolchain() for why the interpreter is pinned at all.
		"XDG_CACHE_HOME="+uvCacheHome(t, srv.python3),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting substrated: %v", err)
	}
	srv.cmd = cmd
	t.Cleanup(func() { srv.stop(t) })

	srv.waitUp(t)
	t.Logf("substrate up on %s; logs in %s", srv.baseURL, srv.logDir)
	return srv
}

// waitUp blocks until discovery answers. Boot migrates the schema and opens
// every repository directory, so the wait is generous and the failure prints
// the log rather than a bare timeout.
func (s *server) waitUp(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if s.cmd.ProcessState != nil {
			t.Fatalf("substrated exited before it came up:\n%s", s.logTail(80))
		}
		resp, err := client.Get(s.baseURL + "/.well-known/substrate/server.json")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("substrated did not answer on %s in two minutes:\n%s", s.baseURL, s.logTail(80))
}

// stop ends the server and FAILS the run when it did not end cleanly: a
// shutdown that hangs or panics is a result, not tidying up.
func (s *server) stop(t *testing.T) {
	t.Helper()
	defer func() { _ = s.logs.Close() }()
	if s.cmd.Process == nil {
		return
	}
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Errorf("signaling substrated: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("substrated exited with %v:\n%s", err, s.logTail(60))
		}
	case <-time.After(60 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
		t.Errorf("substrated did not exit within a minute of SIGTERM:\n%s", s.logTail(60))
	}
}

func (s *server) logTail(lines int) string {
	raw, err := os.ReadFile(s.logDst) //nolint:gosec // the log this test wrote
	if err != nil {
		return fmt.Sprintf("(no server log: %v)", err)
	}
	return tail(string(raw), lines)
}

// --- the door -----------------------------------------------------------------

// register creates one repository through substratectl, exactly as an
// operator would, and hands back its bearer. Registering through the CLI (and
// not over HTTP) is what leaves the context the runner's own `substratectl
// apply` addresses; SUBSTRATECTL_CONFIG keeps that file inside the test's temp
// directory, so nothing reads or writes the developer's.
func (s *server) register(t *testing.T, authority string) string {
	t.Helper()
	cmd := exec.Command(s.substratectl, //nolint:gosec // the binary this test just built
		"--context", authority, "--server", s.baseURL,
		"register", "--repository", authority,
		"--invite-code", inviteCode, "--password-stdin")
	cmd.Stdin = strings.NewReader(repoPassword + "\n")
	cmd.Env = append(os.Environ(), "SUBSTRATECTL_CONFIG="+s.ctlConfig, "PATH="+s.path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("registering %s: %v\n%s", authority, err, out)
	}
	token := s.contextToken(t, authority)
	if token == "" {
		t.Fatalf("registering %s stored no token in %s", authority, s.ctlConfig)
	}
	return token
}

// contextToken reads one context's bearer out of the CLI config the
// registration wrote.
func (s *server) contextToken(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(s.ctlConfig) //nolint:gosec // a path under the test's own temp dir
	if err != nil {
		t.Fatalf("reading %s: %v", s.ctlConfig, err)
	}
	var cfg struct {
		Contexts []struct {
			Name  string `yaml:"name"`
			Token string `yaml:"token"`
		} `yaml:"contexts"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parsing %s: %v", s.ctlConfig, err)
	}
	for _, c := range cfg.Contexts {
		if c.Name == name {
			return c.Token
		}
	}
	return ""
}

// --- the toolchain ------------------------------------------------------------

// toolchain resolves the interpreter and the uv the server must use, and
// returns (python3, PATH).
//
// The engine confines the uv resolve and every function body with Landlock,
// and a grant names a RESOLVED file. A version manager's shim is a symlink
// into a tree the grant does not cover, so a substrate whose PATH puts mise
// first fails every provider install: `uv sync: exit status 126`, or
// `exec .../uv-cache/environments-v2/.../bin/python: permission denied` for a
// virtual environment built on the shimmed interpreter. So the PATH handed to
// substrated names the real binaries, and the interpreter's own directory
// comes first: internal/runner/pyhost.go pins `uv sync --python` to whatever
// `python3` resolves to.
func toolchain(t *testing.T) (python3, path string) {
	t.Helper()
	python3 = resolveInterpreter(t)
	uvBin := resolveUV(t)
	path = strings.Join([]string{filepath.Dir(python3), filepath.Dir(uvBin), os.Getenv("PATH")}, string(os.PathListSeparator))
	t.Logf("toolchain: python3 %s, uv %s", python3, uvBin)
	return python3, path
}

// managerRoots are the trees a version manager owns. A binary under one of
// them is a shim or a managed install, and the sandbox will not exec it.
func managerRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".local", "share", "mise"),
		filepath.Join(home, ".asdf"),
		filepath.Join(home, ".pyenv"),
	}
}

func underManager(p string) bool {
	for _, root := range managerRoots() {
		if root != "" && strings.HasPrefix(p, root+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// resolveInterpreter picks the first system python3 the sandbox can exec.
func resolveInterpreter(t *testing.T) string {
	t.Helper()
	for _, dir := range []string{"/usr/bin", "/usr/local/bin", "/bin", "/opt/homebrew/bin"} {
		p := filepath.Join(dir, "python3")
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil || underManager(resolved) {
			continue
		}
		return p
	}
	// Nothing outside a version manager's tree. Take what PATH offers and say
	// so: the install may still fail, and the message is the diagnosis.
	p, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("no python3 anywhere: %v", err)
	}
	t.Logf("no system python3 found; using %s, which the sandbox may refuse to exec", p)
	return p
}

// resolveUV finds the real uv binary, never the shim. `mise which uv` is
// asked first because a shim resolves to the manager itself and nothing about
// the symlink says where the target is.
func resolveUV(t *testing.T) string {
	t.Helper()
	if mise, err := exec.LookPath("mise"); err == nil {
		out, err := exec.Command(mise, "which", "uv").Output() //nolint:gosec // mise as PATH resolved it
		if err == nil {
			if p := strings.TrimSpace(string(out)); p != "" && !isShim(p) {
				return p
			}
		}
	}
	p, err := exec.LookPath("uv")
	if err != nil {
		t.Fatalf("no uv on PATH: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	if isShim(p) {
		t.Fatalf("uv on PATH is a version manager shim (%s) and the sandbox "+
			"cannot exec one; install uv outside the manager or put the real "+
			"binary on PATH", p)
	}
	return p
}

// isShim reports whether a path resolves to something that is not uv itself:
// mise's shims all point at `mise`, which re-execs uv only when invoked under
// uv's name.
func isShim(p string) bool {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return false
	}
	return filepath.Base(resolved) != "uv"
}

// uvCacheHome keys the runner's python scratch (internal/runner pyDir, under
// os.UserCacheDir) by the interpreter the environments are built on, so a
// cache left by a run with a different python is never reused. Reusing one
// built on a version manager's interpreter is the failure this whole pinning
// exists to avoid, and it surfaces as a permission denied at exec, not as a
// cache miss.
func uvCacheHome(t *testing.T, python3 string) string {
	t.Helper()
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	sum := sha256.Sum256([]byte(python3))
	dir := filepath.Join(base, "substrate-providere2e", hex.EncodeToString(sum[:6]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating the runner cache %s: %v", dir, err)
	}
	return dir
}

// --- odds and ends ------------------------------------------------------------

func build(t *testing.T, repoRoot, out, pkg string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, pkg) //nolint:gosec // the suite's own package paths
	cmd.Dir = repoRoot
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, raw)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("releasing the reserved port: %v", err)
	}
	return port
}

// requireMockPortFree fails the case when the provider's pinned mock port is
// already taken. The port is pinned in e2e.json so the rewired function body
// is identical run to run, so the harness cannot quietly pick another: a
// second mock would serve the wrong recordings, and the sync would read a
// live listener's answers as the fixture set's.
func requireMockPortFree(t *testing.T, provider string, port int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			_ = l.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the mock port %d that providers/%s/e2e.json pins is in "+
				"use and did not free up in 30 seconds (%v). Another run of "+
				"this suite is live, or a mock outlived one; the port is "+
				"pinned, so the harness will not pick another.", port, provider, err)
		}
		time.Sleep(time.Second)
	}
}

// runWithBudget runs one command, capturing everything it prints, and kills
// its whole process group when the budget runs out. The group matters: the
// runner starts the mock and the scenario, and killing only the runner would
// leave the mock holding its pinned port.
func runWithBudget(t *testing.T, cmd *exec.Cmd, budget time.Duration) ([]byte, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", cmd.Path, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return out.Bytes(), err
	case <-time.After(budget):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return out.Bytes(), fmt.Errorf("killed after %s, its whole budget", budget)
	}
}

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}
