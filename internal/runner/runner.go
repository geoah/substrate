// Package runner is the function runner: a child process of the substrate
// (same host, same container, NEVER in-process) for every installed function
// body. ONE PROCESS PER INSTALLATION, always: python bodies get their own
// interpreter with the source exec'd into it, go bodies compile at
// registration to a binary in a content-addressed build cache and run
// supervised. Live runner state is keyed by repository + function identity +
// content hash, so nothing is ever shared across repositories or functions; the
// build cache alone is shared, because its artifacts are immutable and it is
// mounted read-only into every body. Both runtimes speak the same JSON-lines
// protocol (protocol.go documents the frames), pinned so moving to Connect
// Describe/Invoke on a local socket, or moving a bundle into its own
// container, is a placement change, not a contract change.
//
// WHY ONE PROCESS PER INSTALLATION. Python bodies used to share one
// interpreter per call level for the whole substrate, one module namespace
// each. A module namespace is not a boundary: a body could read a neighbor's
// globals straight off `sys.modules['__main__']`, monkeypatch `json` or
// `urllib` to intercept what another function was handed, including the live
// provider tokens the connector bundles receive on their config, and reach the
// protocol's own file descriptors to forge frames at the parent. That was
// cross-REPOSITORY, not merely cross-function. The shared host is gone; the
// cost is one interpreter per live installation, which the idle reaper below
// bounds.
//
// Every child is CONFINED (internal/sandbox): Landlock for the filesystem,
// seccomp for the syscall surface and the network capability, rlimits for the
// cheap ceilings. That is what makes the env allowlist below a boundary rather
// than a gesture: without it a body reads the substrate's own environment out
// of /proc and helps itself to the credential key.
//
// Supervision is lazy: a crashed or timed-out process is killed — the whole
// process GROUP, so spawned descendants die with it — and the next
// invocation restarts it. The per-invocation timeout comes from the manifest
// and bounds the WHOLE delivery as a context deadline threaded through every
// host call; the dispatcher's ordinary retry-then-park absorbs the failure.
package runner

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/geoah/substrate/internal/sandbox"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Spec is one function body as the runner needs it, flattened off the
// manifest so this package needs no registry.
type Spec struct {
	// Repository and Function pin the INSTALLATION identity: live runner state
	// (python module namespaces, Go processes) never crosses repositories or
	// functions, even for byte-identical source.
	//
	// Repository is the repository's ID, never its name. The id is the
	// identity; a name is a lookup key, and two DIFFERENT repositories can
	// carry the same one — every test schema in a test binary calls its
	// repository "geoah", and keying on that made them share interpreters and
	// retire each other's mid-delivery through Reconcile.
	Repository string
	Function   string // "<authority>/<name>"

	Runtime   string // vocabulary.RuntimePython | vocabulary.RuntimeGo
	Source    string
	TimeoutMs int
	// The read capability: the allowlist and budget every host call is held
	// to. Empty ReadTypes means the body declared no reads: list and search
	// trip, and get answers as though the record were absent — the allowlist
	// is never an existence oracle.
	ReadTypes []string
	ReadCalls int
	ReadRows  int
	// CallTargets is the `permissions.call` allowlist: the function
	// identities the body's host Call may invoke. Empty means every
	// sub-call trips.
	CallTargets []string
	// Network is the `permissions.network` declaration. Its CONTENT (the
	// host patterns) is still only documentation, but its EMPTINESS is
	// enforced: a body that declares no egress is denied AF_INET and AF_INET6
	// sockets by the sandbox's seccomp filter. Holding it to the declared
	// hosts needs an egress proxy, because a syscall filter cannot read the
	// address behind a pointer.
	Network []string
	// Modules are the SHARED bundle library modules this function's bundle
	// ships, filename → source. `.py` files land on a bundle-scoped PYTHONPATH
	// dir the isolated Python process imports; `.go` files land in the built
	// binary's `substratefn.local/lib` package. Nil for a standalone function.
	// Modules re-key the installation (contentHash), so changing one rebuilds
	// or re-registers, exactly like changing the body.
	Modules map[string]string
}

// Key is the LIVE state identity: repository, function and the content hash. Two
// installations of identical source get separate registrations and separate
// processes — only the compiled ARTIFACT is shared, through the build cache.
func (s Spec) Key() string {
	return s.Repository + "|" + s.Function + "|" + s.contentHash()
}

// contentHash fingerprints the body, its shared modules AND the part of the
// capability envelope the sandbox enforces at process start: a change to any
// of them re-keys the installation, so a new process is started and (Python)
// re-registered or (Go) rebuilt against the new inputs.
//
// The network state has to be in here. The sandbox policy is applied ONCE, when
// the process starts, so a manifest that drops `permissions.network` without
// touching a line of source would otherwise leave the running process,
// started when egress was granted, serving deliveries with its sockets
// intact. A
// capability that is only withdrawn on the next unrelated edit is not withdrawn.
func (s Spec) contentHash() string {
	h := sha256.New()
	h.Write([]byte(s.Runtime + "\x00" + s.Source))
	for _, name := range sortedKeys(s.Modules) {
		h.Write([]byte{0})
		h.Write([]byte(name + "\x00" + s.Modules[name]))
	}
	h.Write([]byte{0})
	if len(s.Network) > 0 {
		h.Write([]byte("network"))
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// workID is the installation's work-dir name: a hash of the full installation
// Key (repository + function + content hash), so two byte-identical
// bodies/modules in different repositories or bundles materialize to SEPARATE
// on-disk files. Keying the dir on contentHash alone would share writable files
// across installations: one could mutate a module another reloads. The
// separation is also what the sandbox grants against: a body may write ITS work
// dir and no other.
func (s Spec) workID() string {
	h := sha256.Sum256([]byte(s.Key()))
	return hex.EncodeToString(h[:16])
}

// pep723 returns the body's PEP 723 script block if it declares one — the
// signal that uv has to provision an environment before the body can run.
func (s Spec) pep723() (string, bool) {
	if s.Runtime != vocabulary.RuntimePython {
		return "", false
	}
	return pep723Block(s.Source)
}

// pythonModules is the shared modules that belong on the Python import path.
func (s Spec) pythonModules() map[string]string {
	return modulesWithExt(s.Modules, ".py")
}

// goModules is the shared modules vendored into the Go build's `lib` package.
func (s Spec) goModules() map[string]string {
	return modulesWithExt(s.Modules, ".go")
}

func modulesWithExt(mods map[string]string, ext string) map[string]string {
	var out map[string]string
	for name, src := range mods {
		if strings.HasSuffix(name, ext) {
			if out == nil {
				out = map[string]string{}
			}
			out[name] = src
		}
	}
	return out
}

// sortedKeys returns a map's keys in a stable order — for hashing.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s Spec) timeout() time.Duration {
	ms := s.TimeoutMs
	if ms <= 0 {
		ms = vocabulary.DefaultRunTimeoutMs
	}
	return time.Duration(ms) * time.Millisecond
}

// Runner manages the child processes. Safe for concurrent use; invocations
// against one process serialize.
type Runner struct {
	mu sync.Mutex
	// pys and gos hold the live processes, one per INSTALLATION (Spec.Key) in
	// both cases. There is no per-call-level pool any more and none is needed:
	// a body N host-Calls deep is by definition a DIFFERENT function: the
	// engine refuses direct and mutual recursion before the call reaches here
	// (engine/runner.go, the identity stack), so a nested invocation never
	// queues on the process its caller is blocking.
	pys map[string]*proc
	gos map[string]*proc
	// sandbox confines every child. Built once, because it probes the kernel.
	sandbox  *sandbox.Confiner
	cacheDir string
	// reaping is set once the idle sweeper is running. It starts with the
	// first process rather than with the runner, so a substrate that never
	// runs a function never has the goroutine.
	reaping bool
}

// Shared is the process-wide runner every dataset dispatches through.
var Shared = New()

// New returns an empty runner; processes start on first use. The sandbox mode
// comes from SUBSTRATE_SANDBOX here rather than from a caller, so a test
// binary and the server confine bodies identically: an isolation property
// that only holds in production is not one worth having.
func New() *Runner {
	mode, err := sandbox.ParseMode(os.Getenv("SUBSTRATE_SANDBOX"))
	if err != nil {
		// An unreadable setting must not silently mean "off".
		mode = sandbox.ModeEnforce
	}
	return &Runner{pys: map[string]*proc{}, gos: map[string]*proc{}, sandbox: sandbox.New(mode)}
}

// Sandbox is the confiner every child goes through, for the boot log, which
// is where an operator learns whether the deployment's kernel actually offers
// what the mode claims.
func (r *Runner) Sandbox() *sandbox.Confiner { return r.sandbox }

// Warm prepares a body ahead of its first delivery — the registration-time
// half of "bodies prepare at registration". Build and registration errors
// surface to the caller, which is what lets schema admission fail on a body
// that cannot compile or load.
func (r *Runner) Warm(ctx context.Context, spec Spec) error {
	switch spec.Runtime {
	case vocabulary.RuntimeGo:
		_, err := r.goProc(ctx, spec)
		return err
	case vocabulary.RuntimePython:
		// Provision at registration, where a slow cold uv resolve belongs,
		// not on the first delivery's timeout, and register the body, so a
		// syntax error or a missing declared dependency fails schema admission
		// instead of parking the first delivery.
		_, err := r.pythonProc(ctx, spec)
		return err
	default:
		return fmt.Errorf("runner: unknown runtime %q", spec.Runtime)
	}
}

// Reconcile retires live runner state the repository no longer references,
// the registry-publish hook: the process serving a removed or superseded
// installation stops. With one process per installation there is nothing to
// deregister any more; retiring a body is closing its process, which takes its
// module namespace, its open descriptors and its scratch with it. The
// content-addressed BUILD cache is deliberately untouched: its artifacts are
// immutable and read-only to every body; eviction is a later, bounded policy.
func (r *Runner) Reconcile(_ context.Context, repository string, live []Spec) {
	keep := map[string]bool{}
	for _, s := range live {
		keep[s.Key()] = true
	}
	prefix := repository + "|"

	r.mu.Lock()
	var stop []*proc
	for _, live := range []map[string]*proc{r.gos, r.pys} {
		for key, p := range live {
			if strings.HasPrefix(key, prefix) && !keep[key] {
				delete(live, key)
				stop = append(stop, p)
			}
		}
	}
	r.mu.Unlock()

	// Each kill takes the process's OWN lock first, so a retirement never lands
	// in the middle of a delivery: roundtrip holds that lock for the whole
	// exchange, and waiting for it turns "killed mid-request" into "the last
	// request finished, then the process closed". The wait is bounded by the
	// invocation timeout the delivery is already under.
	//
	// Unlike the idle sweep this WAITS rather than skipping a busy process. A
	// swept process is merely idle and will be swept again; a reconciled one
	// belongs to an installation the registry no longer has, and leaving it
	// alive because it happened to be busy would leave a retired body serving.
	for _, p := range stop {
		p.mu.Lock()
		p.kill()
		p.mu.Unlock()
	}
}

// Invoke runs one delivery to completion: input in, output/effects/logs out,
// host calls served from backend under the spec's read capability. The
// manifest timeout is a context deadline over the WHOLE invocation —
// including every backend call, so a stuck read cannot outlive it. A
// deterministic capability trip (allowlist, budget) surfaces as its sentinel
// error even when the body swallowed the failed read — after a trip, nothing
// applies.
func (r *Runner) Invoke(ctx context.Context, spec Spec, in Input, backend Backend) (*Result, error) {
	if spec.ReadCalls > 0 || spec.ReadRows > 0 {
		in.Budgets = &Budgets{Calls: spec.ReadCalls, Rows: spec.ReadRows}
	}
	ictx, cancel := context.WithTimeout(ctx, spec.timeout())
	defer cancel()
	var p *proc
	var err error
	switch spec.Runtime {
	case vocabulary.RuntimePython:
		// One process per installation, serving this body alone: a nested Call
		// is a different function, so it lands on a different process and can
		// never queue behind the caller that is blocking on it.
		p, err = r.pythonProc(ictx, spec)
	case vocabulary.RuntimeGo:
		p, err = r.goProc(ictx, spec)
	default:
		return nil, fmt.Errorf("runner: unknown runtime %q", spec.Runtime)
	}
	if err != nil {
		return nil, err
	}
	state := &readState{spec: spec, backend: backend}
	resp, err := p.roundtrip(ictx, spec.timeout(),
		frame{Op: "invoke", ID: spec.Key(), Input: &in}, state)
	if state.tripped != nil {
		return nil, state.tripped
	}
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("runner: %s", resp.Error)
	}
	return &Result{Output: resp.Output, Effects: resp.Effects, Logs: resp.Logs, More: resp.More}, nil
}

//go:embed host.py
var hostPy string

// --- the go binaries ---------------------------------------------------------

// goProc returns the live process for one installation, building the cached
// artifact and starting a verified process if needed. A binary that cannot
// exec or answer a describe — a corrupt or protocol-stale artifact — is
// invalidated and rebuilt exactly once.
func (r *Runner) goProc(ctx context.Context, spec Spec) (*proc, error) {
	key := spec.Key()
	r.mu.Lock()
	if p, ok := r.gos[key]; ok && p.alive() {
		p.touch()
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()
	var lastErr error
	for range 2 {
		bin, err := r.ensureBinary(ctx, spec)
		if err != nil {
			return nil, err
		}
		p, err := r.startVerified(ctx, spec, bin)
		if err == nil {
			r.mu.Lock()
			if cur, ok := r.gos[key]; ok && cur.alive() {
				r.mu.Unlock()
				p.kill()
				return cur, nil
			}
			r.gos[key] = p
			r.reap()
			r.mu.Unlock()
			return p, nil
		}
		lastErr = err
		// Invalidate once: the artifact failed to exec or to speak the
		// protocol, so the cache entry cannot be trusted.
		_ = os.Remove(bin)
	}
	return nil, lastErr
}

// startVerified starts one compiled body and proves it speaks the protocol
// with a describe roundtrip before anyone invokes through it.
func (r *Runner) startVerified(ctx context.Context, spec Spec, bin string) (*proc, error) {
	work, err := r.goWorkDir(spec)
	if err != nil {
		return nil, err
	}
	tmpDir, err := scratch(work)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin)
	cmd.Dir = work
	// The compiled body's own scratch, not the shared /tmp: the sandbox
	// grants this directory and no other writable path.
	cmd.Env = childEnv("TMPDIR=" + tmpDir)
	// The build cache is read-and-EXECUTE: the binary lives there and every
	// other installation's does too, so a writable grant would let one body
	// replace an artifact another is about to run.
	p, err := r.startCmd(cmd, policyFor(spec, work, filepath.Dir(bin)))
	if err != nil {
		return nil, fmt.Errorf("runner: start %s: %w", bin, err)
	}
	dctx, cancel := context.WithTimeout(ctx, spec.timeout())
	defer cancel()
	resp, err := p.roundtrip(dctx, spec.timeout(), frame{Op: "describe"}, nil)
	if err != nil {
		p.kill()
		return nil, fmt.Errorf("runner: describe %s: %w", bin, err)
	}
	if !resp.OK {
		p.kill()
		return nil, fmt.Errorf("runner: describe %s: %s", bin, resp.Error)
	}
	// The version negotiation: a binary speaking another protocol is a stale
	// artifact — refused here so goProc invalidates and rebuilds it.
	if resp.Protocol != ProtocolVersion {
		p.kill()
		return nil, fmt.Errorf("runner: describe %s: protocol %d, want %d", bin, resp.Protocol, ProtocolVersion)
	}
	return p, nil
}

// --- frames -------------------------------------------------------------------

// The child→parent frame kinds (protocol.go).
const (
	frameKindResponse = "response"
	frameKindCall     = "call"
	frameKindReply    = "reply"
)

// frame is one parent → child request line.
type frame struct {
	Op     string `json:"op"`
	ReqID  uint64 `json:"reqId"`
	ID     string `json:"id,omitempty"`
	Source string `json:"source,omitempty"`
	Input  *Input `json:"input,omitempty"`
}

// response is a child's final line for one request.
type response struct {
	OK      bool     `json:"ok"`
	Error   string   `json:"error"`
	Output  any      `json:"output"`
	Effects []any    `json:"effects"`
	Logs    []string `json:"logs"`
	// More, present, is the paged-checkpoint continuation (protocol.go): this
	// page is done, re-invoke with More.Cursor. Absent means drained.
	More *Continuation `json:"more"`
	// Protocol rides the describe response only: the child's pinned wire
	// version, asserted against ProtocolVersion before anything invokes.
	Protocol int `json:"protocol"`
}

// hostCall is a child's interleaved read request.
type hostCall struct {
	Host   string          `json:"host"`
	Params json.RawMessage `json:"params"`
}

// --- the process --------------------------------------------------------------

const (
	// maxScanBytes is the parent's frame ceiling. Children cap their own
	// frames well below it (host.py / substratefn MAX_FRAME_BYTES = 8 MiB); a
	// line over the ceiling is a scanner error that KILLS the child instead
	// of leaving it blocked on its pipe.
	maxScanBytes = 16 << 20
	// stderrCap bounds the retained child stderr — the capped changelog the
	// redirected body output lands in, surfaced on failures.
	stderrCap = 32 << 10
)

// proc is one supervised child: stdio pipes plus the line channel its reader
// goroutine feeds, so a roundtrip can time out without blocking on a read.
type proc struct {
	mu    sync.Mutex // serializes roundtrips — the protocol is one frame in flight
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan []byte
	// dead closes when the reader goroutine finishes; waited when the child
	// is reaped — kill() blocks on neither, but Wait always completes.
	dead   chan struct{}
	waited chan struct{}
	// gone closes the moment the parent gives up on the child, unblocking a
	// reader stuck delivering unsolicited lines.
	gone     chan struct{}
	goneOnce sync.Once
	// killed is set the moment the parent gives up on the child — the pipe
	// EOF that closes dead lags the kill, and alive() must not say yes in
	// between.
	killed atomic.Bool
	// reqID numbers this process's requests; children echo it on every frame.
	reqID atomic.Uint64
	// registered marks the source keys THIS python process holds; guarded by
	// Runner.mu. A dead generation's map dies with it.
	registered map[string]bool
	// lastUsed is the reaper's clock: unix nanos of the last roundtrip.
	lastUsed atomic.Int64
	// stderr retains the tail of the child's stderr — where redirected body
	// prints land — for diagnostics.
	stderr *capBuf
}

// capBuf is a concurrency-safe ring buffer keeping the LAST cap bytes.
type capBuf struct {
	mu  sync.Mutex
	cap int
	buf []byte
}

func (c *capBuf) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, p...)
	if len(c.buf) > c.cap {
		c.buf = c.buf[len(c.buf)-c.cap:]
	}
	return len(p), nil
}

func (c *capBuf) tail() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(string(c.buf))
}

// touch records that this process was just used, for the idle reaper.
func (p *proc) touch() { p.lastUsed.Store(time.Now().UnixNano()) }

// idleFor reports how long the process has gone unused.
func (p *proc) idleFor(now time.Time) time.Duration {
	return now.Sub(time.Unix(0, p.lastUsed.Load()))
}

// The reaper's dials. One process per installation is the right isolation
// boundary but the wrong steady state for a substrate with many bundles
// installed: an interpreter is tens of megabytes of RSS whether or not its
// trigger ever fires again. So an idle body is closed and restarted on
// demand: the cost of a restart is one register roundtrip, and for a uv body
// the environment is already provisioned.
const (
	idleTTL      = 10 * time.Minute
	reapInterval = time.Minute
)

// reap starts the idle sweeper once. Callers hold r.mu.
func (r *Runner) reap() {
	if r.reaping {
		return
	}
	r.reaping = true
	go func() {
		for {
			time.Sleep(reapInterval)
			r.sweep(time.Now())
		}
	}()
}

// sweep closes every process idle past the TTL. A process mid-invocation is
// skipped rather than waited on: roundtrip holds proc.mu for the whole
// exchange, so a failed TryLock means busy, and busy means recently used
// anyway.
//
// The kill happens while proc.mu is still HELD, not after releasing it, so a
// caller that is ALREADY inside roundtrip finishes first: that lock is held for
// the whole exchange.
//
// It does not make lookup-and-use atomic, and the comment should not pretend
// otherwise. A caller can take a pointer out of the map, lose the race to this
// sweep, and then write to a process that is already dead; roundtrip does not
// restart anything, it returns "child exited mid-request" and the dispatcher
// retries the delivery, which starts a fresh process through the ordinary path.
// The window is between a map lookup and the next line of the same function,
// against a process that has been idle for the TTL, so a delivery losing it is
// a rarity that costs one retry.
func (r *Runner) sweep(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, live := range []map[string]*proc{r.gos, r.pys} {
		for key, p := range live {
			if !p.alive() {
				delete(live, key)
				continue
			}
			if p.idleFor(now) < idleTTL {
				continue
			}
			if !p.mu.TryLock() {
				continue
			}
			delete(live, key)
			p.kill()
			p.mu.Unlock()
		}
	}
}

// startCmd wires stdio, supervision and the SANDBOX onto a command. Every
// runner child goes through here, which is the point: one place decides what a
// body may touch, so a new exec path cannot forget to be confined.
func (r *Runner) startCmd(cmd *exec.Cmd, policy sandbox.Policy) (*proc, error) {
	if err := r.sandbox.Wrap(cmd, policy); err != nil {
		return nil, err
	}
	return startCmd(cmd)
}

// childEnv is the MINIMAL environment every runner child is started with, built
// DEFAULT-DENY: an explicit allowlist of names copied from the host env, NEVER
// os.Environ(). A bundle body is arbitrary third-party code running as the same
// container user, so if it inherited the substrate's environment it could read
// the master secrets — SUBSTRATE_CREDENTIAL_KEY (unseals every repository's OAuth
// tokens and client secrets), DATABASE_URL (direct Postgres as the app role,
// past all repository isolation), SUBSTRATE_OPERATOR_OTP (mint operator tokens),
// SUBSTRATE_OAUTH_STATE_KEY. Filtering os.Environ()
// to drop those would be a standing regression: the next secret added to the
// host env would leak until someone remembered to deny it. A named allowlist
// default-denies whatever it does not list, so a new host secret is safe by
// construction; callers add only their own KEY=VALUE plumbing via `extra`.
//
// The base is the platform basics a child legitimately needs: PATH (to find an
// interpreter it may shell out to), HOME (uv, python and the go toolchain all
// read it), TMPDIR (scratch), and the locale so text handling is stable.
func childEnv(extra ...string) []string {
	base := passEnv(
		"PATH", "HOME", "TMPDIR",
		"LANG", "LC_ALL", "LC_CTYPE",
	)
	return append(base, extra...)
}

// passEnv copies the named variables from the host env, in the given order,
// skipping any that are unset — so the child env carries no empty ghosts and
// nothing but the names asked for.
func passEnv(names ...string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if v, ok := os.LookupEnv(name); ok {
			out = append(out, name+"="+v)
		}
	}
	return out
}

// startCmd wires stdio pipes, the reader goroutine and process-authority
// supervision onto an already-built command — so an isolated python host can
// carry its own Dir/Env (PYTHONPATH, UV_CACHE_DIR) and still be supervised
// exactly like every other child.
func startCmd(cmd *exec.Cmd) (*proc, error) {
	stderr := &capBuf{cap: stderrCap}
	cmd.Stderr = stderr
	// Default-deny env: unless the caller already built a minimal, explicit env
	// (the isolated python host and the go build do, with their own plumbing),
	// start the child with the minimal allowlisted base — NEVER the substrate's
	// os.Environ(), which carries the master secrets a bundle body could
	// otherwise read straight out of its own process environment.
	if cmd.Env == nil {
		cmd.Env = childEnv()
	}
	// Its own process group, so a kill reaps the body's descendants too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &proc{
		cmd: cmd, stdin: stdin, lines: make(chan []byte, 4),
		dead: make(chan struct{}), waited: make(chan struct{}),
		gone: make(chan struct{}), registered: map[string]bool{}, stderr: stderr,
	}
	p.touch()
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), maxScanBytes)
		for sc.Scan() {
			line := make([]byte, len(sc.Bytes()))
			copy(line, sc.Bytes())
			// Cancellation-aware: an unsolicited flood never wedges the
			// reader — once the parent gives up, lines drop on the floor and
			// the loop drains to EOF.
			select {
			case p.lines <- line:
			case <-p.gone:
			}
		}
		if sc.Err() != nil {
			// An oversized or torn frame: the stream is unusable and the
			// child may be blocked mid-write on its pipe — kill it.
			p.kill()
		}
		close(p.dead)
		// The reader owns the reap: it runs after EOF, so Wait never races
		// the pipe reads and always completes.
		_ = cmd.Wait()
		close(p.waited)
	}()
	return p, nil
}

func (p *proc) alive() bool {
	if p.killed.Load() {
		return false
	}
	select {
	case <-p.dead:
		return false
	default:
		return true
	}
}

// kill tears the child down — the WHOLE process group, so descendants a body
// spawned die with it; the next invocation restarts it.
func (p *proc) kill() {
	p.killed.Store(true)
	p.goneOnce.Do(func() { close(p.gone) })
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		// Setpgid above makes the child its own process-group leader.
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		_ = p.cmd.Process.Kill()
	}
}

// stderrTail formats the retained child stderr for an error message.
func (p *proc) stderrTail() string {
	tail := p.stderr.tail()
	if tail == "" {
		return ""
	}
	return " (stderr: " + tail + ")"
}

// roundtrip sends one frame and reads to its final response, serving
// interleaved host calls from state. Frames are matched by kind and request
// id: a stale response, a stray line or an undecodable frame is a protocol
// desync that kills the process — its state is unknowable — as does the
// context deadline (the invocation timeout) or a torn pipe. The error rides
// the dispatcher's ordinary retry-then-park.
func (p *proc) roundtrip(ctx context.Context, timeout time.Duration, f frame, state *readState) (*response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.touch()
	f.ReqID = p.reqID.Add(1)

	raw, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	if _, err := p.stdin.Write(append(raw, '\n')); err != nil {
		p.kill()
		return nil, fmt.Errorf("runner: write to child: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			p.kill()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("runner: invocation exceeded %s", timeout)
			}
			return nil, ctx.Err()
		case <-p.dead:
			return nil, fmt.Errorf("runner: child exited mid-request%s", p.stderrTail())
		case line := <-p.lines:
			var head struct {
				Kind  string `json:"kind"`
				ReqID uint64 `json:"reqId"`
			}
			if err := json.Unmarshal(line, &head); err != nil {
				p.kill()
				return nil, fmt.Errorf("runner: undecodable frame from child: %w", err)
			}
			if head.ReqID != f.ReqID || (head.Kind != frameKindResponse && head.Kind != frameKindCall) {
				// A response for a request nobody is waiting on, or a frame
				// with no kind: the stream is desynchronized and anything
				// read from here on could pair with the wrong delivery.
				p.kill()
				return nil, fmt.Errorf("runner: protocol desync: kind %q reqId %d during request %d",
					head.Kind, head.ReqID, f.ReqID)
			}
			if head.Kind == frameKindCall {
				var call hostCall
				if err := json.Unmarshal(line, &call); err != nil {
					p.kill()
					return nil, err
				}
				reply := state.serve(ctx, call)
				reply["kind"] = frameKindReply
				reply["reqId"] = f.ReqID
				rawReply, err := json.Marshal(reply)
				if err != nil {
					p.kill()
					return nil, err
				}
				if _, err := p.stdin.Write(append(rawReply, '\n')); err != nil {
					p.kill()
					return nil, fmt.Errorf("runner: reply to child: %w", err)
				}
				continue
			}
			var resp response
			if err := json.Unmarshal(line, &resp); err != nil {
				p.kill()
				return nil, fmt.Errorf("runner: undecodable response from child: %w", err)
			}
			// Re-decode ONLY the effects with UseNumber so an ifVersion
			// concurrency token survives as json.Number rather than float64: a
			// version past 2^53 would otherwise round to a neighboring integer
			// in transit and be accepted as a different precondition. The engine's
			// asInt64 reads json.Number through Int64; property coercion reads it
			// through asFloat. Output/More stay ordinary float64 — only a
			// precondition token needs the exact-integer guarantee.
			if len(resp.Effects) > 0 {
				var raw struct {
					Effects json.RawMessage `json:"effects"`
				}
				if err := json.Unmarshal(line, &raw); err == nil && len(raw.Effects) > 0 {
					dec := json.NewDecoder(bytes.NewReader(raw.Effects))
					dec.UseNumber()
					var effs []any
					if err := dec.Decode(&effs); err != nil {
						p.kill()
						return nil, fmt.Errorf("runner: undecodable effects from child: %w", err)
					}
					resp.Effects = effs
				}
			}
			return &resp, nil
		}
	}
}

// --- the capability-scoped reads ------------------------------------------------

// readState meters one invocation's host calls and remembers the first trip:
// the trip decides the delivery whatever the body does with the error. Calls
// are charged BEFORE they execute — a failed call is never free — and list
// and search are clamped to the remaining row budget before they run, so an
// over-budget request never performs the work first.
type readState struct {
	spec    Spec
	backend Backend
	calls   int
	rows    int
	tripped error
}

// serve answers one host call. Every failure becomes an error frame the body
// surfaces as its own error; a nil state (a register roundtrip) refuses.
func (s *readState) serve(ctx context.Context, call hostCall) map[string]any {
	if s == nil || s.backend == nil {
		return map[string]any{"ok": false, "error": "runner: no host calls outside an invocation"}
	}
	out, err := s.handle(ctx, call)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true, "result": out}
}

func (s *readState) handle(ctx context.Context, call hostCall) (any, error) {
	switch call.Host {
	case "get":
		var in struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(call.Params, &in); err != nil {
			return nil, fmt.Errorf("get: %w", err)
		}
		if in.Kind == "" {
			return nil, fmt.Errorf("get: kind is required — records are addressed by (kind, id)")
		}
		if err := s.chargeCall(); err != nil {
			return nil, s.trip(err)
		}
		e, err := s.backend.Get(ctx, in.Kind, in.ID)
		if err != nil {
			return nil, err
		}
		// A resolved record outside the allowlist answers exactly like an
		// absent one — same shape, same budget charge — so a disallowed get
		// is never an existence or type oracle.
		if e != nil && !s.allowed(e.Kind) {
			e = nil
		}
		if e != nil {
			if err := s.chargeRows(1); err != nil {
				return nil, s.trip(err)
			}
		}
		return map[string]any{"record": e}, nil
	case "list":
		var q substrate.Query
		if err := json.Unmarshal(call.Params, &q); err != nil {
			return nil, fmt.Errorf("list: %w", err)
		}
		if err := s.requireTypes(q.Filter.Kinds, "list"); err != nil {
			return nil, err
		}
		if err := s.chargeCall(); err != nil {
			return nil, s.trip(err)
		}
		rem, err := s.remainingRows()
		if err != nil {
			return nil, s.trip(err)
		}
		if q.First <= 0 || q.First > rem {
			q.First = rem
		}
		page, err := s.backend.List(ctx, q)
		if err != nil {
			return nil, err
		}
		if err := s.chargeRows(len(page.Records)); err != nil {
			return nil, s.trip(err)
		}
		return map[string]any{"page": page}, nil
	case "search":
		var in substrate.SearchInput
		if err := json.Unmarshal(call.Params, &in); err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}
		if err := s.requireTypes(in.Kinds, "search"); err != nil {
			return nil, err
		}
		if err := s.chargeCall(); err != nil {
			return nil, s.trip(err)
		}
		rem, err := s.remainingRows()
		if err != nil {
			return nil, s.trip(err)
		}
		if in.K <= 0 || in.K > rem {
			in.K = rem
		}
		hits, err := s.backend.Search(ctx, in)
		if err != nil {
			return nil, err
		}
		if err := s.chargeRows(len(hits)); err != nil {
			return nil, s.trip(err)
		}
		return map[string]any{"hits": hits}, nil
	case "call":
		var in struct {
			Function string `json:"function"`
			Input    any    `json:"input"`
		}
		if err := json.Unmarshal(call.Params, &in); err != nil {
			return nil, fmt.Errorf("call: %w", err)
		}
		if in.Function == "" {
			return nil, fmt.Errorf("call: function is required")
		}
		// The allowlist is checked before the budget charges: an ungranted
		// call is a deterministic trip that decides the delivery.
		if !s.callAllowed(in.Function) {
			return nil, s.trip(fmt.Errorf("%w: %s calls %s", ErrCallForbidden, s.spec.Function, in.Function))
		}
		if err := s.chargeCall(); err != nil {
			return nil, s.trip(err)
		}
		out, err := s.backend.Call(ctx, in.Function, in.Input)
		if err != nil {
			return nil, err
		}
		return map[string]any{"output": out}, nil
	default:
		return nil, fmt.Errorf("unknown host call %q", call.Host)
	}
}

// callAllowed reports whether a function identity is in the call allowlist.
func (s *readState) callAllowed(ident string) bool {
	for _, t := range s.spec.CallTargets {
		if t == ident {
			return true
		}
	}
	return false
}

// allowed reports whether a full type identity is in the reads allowlist.
func (s *readState) allowed(ident string) bool {
	for _, t := range s.spec.ReadTypes {
		if t == ident {
			return true
		}
	}
	return false
}

// requireTypes holds a list/search to the allowlist: the request must name
// kinds and every one must be declared — an unscoped read would see
// everything, so naming no kinds is itself a violation.
//
// Each name is RESOLVED first, because a kind has two spellings and a body is
// entitled to use either: `get` has always been held to the resolved kind of
// the record it found, and a list naming `task` against a capability declaring
// `tasks.substrate.reamde.dev/task` is the same read, so it cannot be a different answer.
func (s *readState) requireTypes(types []string, what string) error {
	if len(types) == 0 {
		return s.trip(fmt.Errorf("%w: %s names no kinds", ErrReadForbidden, what))
	}
	for _, t := range types {
		if !s.allowed(s.resolved(t)) {
			return s.trip(fmt.Errorf("%w: %s reads %s", ErrReadForbidden, what, t))
		}
	}
	return nil
}

// resolved asks the backend for a kind reference's identity, leaving anything
// it cannot answer for alone — an unknown kind then fails the allowlist, which
// is the same answer a body gets for one it may not read.
func (s *readState) resolved(name string) string {
	if s.backend == nil {
		return name
	}
	if ident := s.backend.ResolveKind(name); ident != "" {
		return ident
	}
	return name
}

// budgets resolves the effective call and row budgets.
func (s *readState) budgets() (calls, rows int) {
	calls, rows = s.spec.ReadCalls, s.spec.ReadRows
	if calls <= 0 {
		calls = vocabulary.DefaultReadCalls
	}
	if rows <= 0 {
		rows = vocabulary.DefaultReadRows
	}
	return calls, rows
}

// chargeCall meters one call BEFORE it executes: a call past the budget
// trips without doing any work, and a failed call is never free.
func (s *readState) chargeCall() error {
	calls, _ := s.budgets()
	s.calls++
	if s.calls > calls {
		return fmt.Errorf("%w: call %d of %d", ErrReadBudget, s.calls, calls)
	}
	return nil
}

// remainingRows is the row headroom a list/search may still return; zero
// headroom is itself a budget trip — the call could not return anything.
func (s *readState) remainingRows() (int, error) {
	_, maxRows := s.budgets()
	rem := maxRows - s.rows
	if rem <= 0 {
		return 0, fmt.Errorf("%w: %d rows of %d already read", ErrReadBudget, s.rows, maxRows)
	}
	return rem, nil
}

// chargeRows meters returned rows.
func (s *readState) chargeRows(n int) error {
	_, maxRows := s.budgets()
	s.rows += n
	if s.rows > maxRows {
		return fmt.Errorf("%w: %d rows of %d", ErrReadBudget, s.rows, maxRows)
	}
	return nil
}

// trip records the first deterministic violation.
func (s *readState) trip(err error) error {
	if s.tripped == nil {
		s.tripped = err
	}
	return err
}
