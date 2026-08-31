package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The Python path: ONE process per installation, no exceptions. The process
// runs host.py's protocol loop (register / describe / invoke, host-call frames,
// more/resume, scrubbing) against exactly one body, materialized in a work dir
// keyed by Spec.workID(): the full installation key, so two byte-identical
// bodies in different repositories or bundles never share a file.
//
// Two things are provisioned around it:
//
//   - uv: when the body carries a `# /// script … # ///` block, its declared
//     dependencies are resolved AT REGISTRATION by `uv sync --script`, and the
//     interpreter uv built for that script environment is then run DIRECTLY.
//     uv is not in the invocation path at all, which matters because the
//     resolve needs the network and broad filesystem access and the invocation
//     is entitled to neither: splitting them is what lets a connector declare
//     `import googleapiclient` inline and still run confined.
//   - PYTHONPATH: the bundle's shared `.py` modules are written into the work
//     dir and appended to sys.path after the interpreter boots (never the
//     startup PYTHONPATH, which would let a sitecustomize.py auto-run before
//     the host is established and a json.py shadow the serializer).
//
// TRUST POSTURE: a body that declares dependencies pulls arbitrary code off
// PyPI at resolution time: the same trust surface as running the inline body,
// which is already arbitrary code execution. The declaration is legible on the
// manifest and the resolve is the author's, not a hidden fetch. The RESOLVED
// code then runs under the same sandbox as the body itself.

// uvProvisionTimeout is the floor for provisioning: a cold uv resolve
// (download, build a venv) can outlast a body's invoke timeout, so it gets its
// own budget. It only takes effect under a context that allows it: Warm
// (registration) does; a cold Invoke is still clamped by the manifest timeout,
// exactly like the Go build path.
const uvProvisionTimeout = 120 * time.Second

// pythonProc returns the live process for one installation, provisioning,
// starting and registering it if needed. Mirrors goProc: one supervised
// process per Spec.Key, proved to speak the protocol by its register roundtrip
// before anyone invokes through it.
func (r *Runner) pythonProc(ctx context.Context, spec Spec) (*proc, error) {
	key := spec.Key()
	r.mu.Lock()
	if p, ok := r.pys[key]; ok && p.alive() {
		p.touch()
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()

	p, err := r.startPython(ctx, spec)
	if err != nil {
		return nil, err
	}
	// The register frame is also the verification: a syntax error or a failed
	// module-level import surfaces to the caller, which is what lets schema
	// admission refuse a body that cannot load.
	provTimeout := spec.timeout()
	if _, uv := spec.pep723(); uv && provTimeout < uvProvisionTimeout {
		provTimeout = uvProvisionTimeout
	}
	rctx, cancel := context.WithTimeout(ctx, provTimeout)
	defer cancel()
	resp, err := p.roundtrip(rctx, provTimeout, frame{Op: "register", ID: key, Source: spec.Source}, nil)
	if err != nil {
		p.kill()
		return nil, fmt.Errorf("runner: python register: %w%s", err, p.stderrTail())
	}
	if !resp.OK {
		p.kill()
		return nil, fmt.Errorf("runner: python register: %s", resp.Error)
	}
	r.mu.Lock()
	if cur, ok := r.pys[key]; ok && cur.alive() {
		r.mu.Unlock()
		p.kill()
		return cur, nil
	}
	p.registered[key] = true
	r.pys[key] = p
	r.reap()
	r.mu.Unlock()
	return p, nil
}

// startPython materializes the host script and shared modules for one
// installation and launches its interpreter.
//
// The work dir is keyed by Spec.workID(): a hash of the full installation KEY
// (repository + function + content hash), NOT the bare content hash, so two
// byte-identical modules in different repositories or bundles get SEPARATE
// physical files. Files are materialized ATOMICALLY as READ-ONLY (temp +
// rename, mode 0o444), which stops an accident; what stops a hostile body is
// the sandbox, which grants that body its own work dir and nothing else.
func (r *Runner) startPython(ctx context.Context, spec Spec) (*proc, error) {
	base, err := r.pyDir()
	if err != nil {
		return nil, err
	}
	work := filepath.Join(base, spec.workID())
	modsDir := filepath.Join(work, "mods")
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return nil, err
	}
	tmpDir, err := scratch(work)
	if err != nil {
		return nil, err
	}

	block, uv := spec.pep723()
	hostSrc := hostPy
	if uv {
		// The block rides ATOP the host script so uv resolves exactly the deps
		// the author declared; the rest of host.py is ordinary comments uv
		// ignores.
		hostSrc = block + "\n" + hostPy
	}
	hostFile := filepath.Join(work, "host.py")
	if err := writeFileRO(hostFile, []byte(hostSrc)); err != nil {
		return nil, err
	}
	for name, src := range spec.pythonModules() {
		if err := writeFileRO(filepath.Join(modsDir, name), []byte(src)); err != nil {
			return nil, err
		}
	}

	uvCache := filepath.Join(base, "uv-cache")
	interpreter, err := pythonInterpreter()
	if err != nil {
		return nil, err
	}
	var extraExec []string
	if uv {
		// Provision UNSANDBOXED and here, at registration: the resolve needs
		// the network and writes uv's shared cache, and neither is something a
		// body may do. What comes back is the interpreter of the script's
		// environment, which is what actually runs the body.
		interpreter, err = r.provisionUV(ctx, hostFile, work, uvCache, spec.timeout())
		if err != nil {
			return nil, err
		}
		// Read and execute, never write: the environment is shared with every
		// other body that declared the same dependencies, so a writable grant
		// would let one installation rewrite a module another imports.
		extraExec = append(extraExec, uvCache)
	}
	// The interpreter's own tree, which is not under /usr on a dev box or a
	// CI runner, where a version manager owns it.
	extraExec = append(extraExec, runtimeRoot(interpreter)...)

	cmd := exec.Command(interpreter, "-u", hostFile)
	cmd.Dir = work
	// SUBSTRATE_PY_MODULES carries the shared-module dir; host.py appends it to
	// the END of sys.path AFTER detaching its protocol FDs.
	// PYTHONUNBUFFERED keeps the line protocol from stalling in a pipe buffer;
	// PYTHONDONTWRITEBYTECODE keeps the interpreter from dropping __pycache__
	// beside the read-only module files, and out of the read-only venv.
	// TMPDIR is the installation's OWN scratch, not the shared /tmp: the
	// sandbox grants exactly this directory, so a body's tempfile lands
	// somewhere no other installation can read.
	cmd.Env = childEnv(
		"SUBSTRATE_PY_MODULES="+modsDir,
		"PYTHONUNBUFFERED=1",
		"PYTHONDONTWRITEBYTECODE=1",
		"TMPDIR="+tmpDir,
	)
	p, err := r.startCmd(cmd, policyFor(spec, work, extraExec...))
	if err != nil {
		return nil, fmt.Errorf("runner: start python: %w", err)
	}
	return p, nil
}

// provisionUV resolves a PEP 723 script's dependencies and returns the
// interpreter of the environment uv built for it.
//
// `uv sync --script` then `uv python find --script` rather than `uv run
// --script`: `uv run` would be uv, in the invocation path, needing network and
// write access to its cache on every start. Running the resolved interpreter
// directly leaves uv entirely on the registration side of the boundary.
func (r *Runner) provisionUV(ctx context.Context, hostFile, work, uvCache string, timeout time.Duration) (string, error) {
	if timeout < uvProvisionTimeout {
		timeout = uvProvisionTimeout
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// A stable UV_CACHE_DIR keys uv's resolved environments so a restart
	// reuses them rather than re-resolving. TMPDIR is the installation's own
	// scratch for the same reason the body gets one: the resolve unpacks
	// archives through a temp dir, and the shared /tmp is neither granted nor
	// somewhere a resolve should be writing.
	tmpDir, err := scratch(work)
	if err != nil {
		return "", err
	}
	env := childEnv("UV_CACHE_DIR="+uvCache, "TMPDIR="+tmpDir)
	uvBin, err2 := exec.LookPath("uv")
	if err = err2; err != nil {
		return "", fmt.Errorf("runner: uv is not on PATH: %w", err)
	}
	// PATH may hold a SYMLINK into a different tree (mise's `.mise-bins/uv`
	// is one), and Landlock checks the RESOLVED file: a grant derived from
	// the symlink's directory denies the exec of what it points at. The grant
	// and the exec both name the target, the same reason pythonInterpreter
	// asks python for sys.executable instead of trusting PATH. The basename
	// check leaves a version manager's SHIM alone: that symlink resolves to
	// the manager's own binary (mise's shims all point at `mise`), which only
	// re-execs uv when invoked under uv's name.
	if resolved, errLink := filepath.EvalSymlinks(uvBin); errLink == nil && filepath.Base(resolved) == "uv" {
		uvBin = resolved
	}
	// The cache dir is created HERE, by the parent, because the confined
	// resolve cannot make it: a Landlock rule names an existing path, and
	// creating a directory needs the right on its PARENT, which uv is not
	// granted. Granting the parent instead would hand the resolve every other
	// installation's work dir.
	if err := os.MkdirAll(uvCache, 0o755); err != nil {
		return "", err
	}
	// Pin the interpreter rather than letting uv search PATH. PATH may hold a
	// version manager's SHIM, which the resolve is not granted and which uv
	// reports as a hard failure rather than skipping. This is the same reason
	// the body execs a resolved interpreter instead of `python3`.
	interpreter, err2 := pythonInterpreter()
	if err = err2; err != nil {
		return "", err
	}
	policy := provisionPolicy(work, uvCache, uvBin, interpreter)

	// The resolve runs third-party build backends, so it is confined like
	// anything else that runs code we did not write.
	sync := exec.CommandContext(pctx, uvBin, "sync", "--quiet", "--python", interpreter, "--script", hostFile)
	sync.Dir, sync.Env = work, env
	if err := r.sandbox.Wrap(sync, policy); err != nil {
		return "", err
	}
	var syncOut bytes.Buffer
	sync.Stdout, sync.Stderr = &syncOut, &syncOut
	if err := r.runGatedCmd(sync); err != nil {
		return "", fmt.Errorf("runner: uv sync: %w\n%s", err, syncOut.Bytes())
	}
	// What comes back is the interpreter OF THE SCRIPT'S ENVIRONMENT, which is
	// the venv uv just built, not the one pinned above.
	find := exec.CommandContext(pctx, uvBin, "python", "find", "--script", hostFile)
	find.Dir, find.Env = work, env
	if err := r.sandbox.Wrap(find, policy); err != nil {
		return "", err
	}
	var findOut, findErr bytes.Buffer
	find.Stdout, find.Stderr = &findOut, &findErr
	if err := r.runGatedCmd(find); err != nil {
		return "", fmt.Errorf("runner: uv python find: %w\n%s", err, findErr.Bytes())
	}
	venv := strings.TrimSpace(findOut.String())
	if venv == "" {
		return "", fmt.Errorf("runner: uv python find returned no interpreter")
	}
	return venv, nil
}

// writeFileRO materializes one file atomically as READ-ONLY: write a temp file
// in the destination directory, flush it to mode 0o444, then rename it over the
// target. Rename is atomic within a directory and replaces any prior read-only
// copy (the file mode does not block rename: the directory's write bit does,
// and the work dir stays 0o755). A restart re-materializes deterministically.
func writeFileRO(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o444); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// pyDir is the python scratch root: per-installation work dirs and uv's cache,
// stable across processes so a substrate restart reuses them.
func (r *Runner) pyDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "substrate-runner", "py")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
