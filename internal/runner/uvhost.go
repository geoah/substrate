package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// The isolated Python path: a body that declares PEP 723 dependencies or whose
// bundle ships shared `.py` modules runs in its OWN process, not the shared
// multi-repository host. The process runs the same host.py protocol loop (register
// / describe / invoke, host-call frames, more/resume, scrubbing all unchanged)
// with two additions provisioned around it:
//
//   - uv: when the body carries a `# /// script … # ///` block, the host script
//     is launched with `uv run --script`, so uv reads the block, provisions a
//     cached venv with the declared deps, and runs the body — the body can then
//     `import googleapiclient` by declaring it inline, with NO pip in the base
//     image and NO change to the shared host used by dependency-free functions.
//   - PYTHONPATH: the bundle's shared `.py` modules are written to a
//     bundle-scoped directory placed on the import path, so the body can
//     `import <sharedmodule>`. Isolation holds because the directory — like the
//     process — is per installation, never shared across bundles or repositories.
//
// Deps are resolved by uv at provision time (network at provision, not at
// invoke); a cached env is keyed by the PEP 723 block through uv's own cache
// (UV_CACHE_DIR is pinned stable so a restart reuses it), mirroring the Go
// build-cache discipline. TRUST POSTURE: a body that declares deps pulls
// arbitrary code off PyPI at resolution time — the same trust surface as
// running the inline body itself, which is already arbitrary code execution in
// a child process. The declaration is legible on the manifest and the resolve
// is the author's, not a hidden fetch.

// uvProvisionTimeout is the floor for the register roundtrip on the isolated
// path: a cold uv resolve (download + build a venv) can outlast a body's
// invoke timeout, so provisioning gets its own budget. It only takes effect
// under a context that allows it — Warm (registration) does; a cold Invoke is
// still clamped by the manifest timeout, exactly like the Go build path.
const uvProvisionTimeout = 120 * time.Second

// isoPythonProc returns the live isolated process for one installation,
// starting and registering it if needed. Mirrors goProc: one supervised
// process per Spec.Key, verified by the register roundtrip before anyone
// invokes through it.
func (r *Runner) isoPythonProc(ctx context.Context, spec Spec) (*proc, error) {
	key := spec.Key()
	r.mu.Lock()
	if p, ok := r.isoPys[key]; ok && p.alive() {
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()

	p, err := r.startIsoPython(spec)
	if err != nil {
		return nil, err
	}
	// Register the single body — the same register frame the shared host
	// answers, so a syntax error or a failed module-level import (a missing
	// declared dep) surfaces to the caller and fails schema admission.
	provTimeout := spec.timeout()
	if _, uv := spec.pep723(); uv && provTimeout < uvProvisionTimeout {
		provTimeout = uvProvisionTimeout
	}
	rctx, cancel := context.WithTimeout(ctx, provTimeout)
	defer cancel()
	resp, err := p.roundtrip(rctx, provTimeout, frame{Op: "register", ID: key, Source: spec.Source}, nil)
	if err != nil {
		p.kill()
		return nil, fmt.Errorf("runner: isolated python register: %w%s", err, p.stderrTail())
	}
	if !resp.OK {
		p.kill()
		return nil, fmt.Errorf("runner: isolated python register: %s", resp.Error)
	}
	r.mu.Lock()
	if cur, ok := r.isoPys[key]; ok && cur.alive() {
		r.mu.Unlock()
		p.kill()
		return cur, nil
	}
	p.registered[key] = true
	r.isoPys[key] = p
	r.mu.Unlock()
	return p, nil
}

// startIsoPython materializes the host script and shared modules for one
// installation and launches the process — `uv run --script` when the body
// declares PEP 723 deps, plain `python3` otherwise.
//
// The work dir is keyed by Spec.workID() — a hash of the full installation
// KEY (repository + function + content hash), NOT the bare content hash — so two
// byte-identical modules in different repositories or bundles get SEPARATE
// physical files; one installation can never mutate a file another reloads.
// Files are materialized ATOMICALLY as READ-ONLY (temp + rename, mode 0o444).
// NOTE: this is crash/placement isolation, not a filesystem security boundary
// — every child runs as the same container user, so read-only file modes and
// per-key paths defend against accident and collision, not against a hostile
// body with the same uid; true repository isolation needs separate users, mounts,
// or containers.
func (r *Runner) startIsoPython(spec Spec) (*proc, error) {
	base, err := r.pyDir()
	if err != nil {
		return nil, err
	}
	work := filepath.Join(base, spec.workID())
	modsDir := filepath.Join(work, "mods")
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
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

	var cmd *exec.Cmd
	if uv {
		cmd = exec.Command("uv", "run", "--quiet", "--script", hostFile)
	} else {
		cmd = exec.Command("python3", "-u", hostFile)
	}
	cmd.Dir = work
	// SUBSTRATE_PY_MODULES carries the shared-module dir; host.py appends it to the
	// END of sys.path AFTER detaching its protocol FDs (never the startup
	// PYTHONPATH, which would let a sitecustomize.py auto-run before the host is
	// established and a json.py shadow the serializer — finding #11).
	// PYTHONUNBUFFERED keeps the line protocol from stalling in a pipe buffer
	// (matching `python3 -u`); PYTHONDONTWRITEBYTECODE keeps the interpreter from
	// dropping `__pycache__` beside the read-only module files; a stable
	// UV_CACHE_DIR keys uv's resolved envs so a restart reuses them rather than
	// re-resolving. The env is the minimal allowlisted base (childEnv) plus this
	// plumbing — NEVER os.Environ(), so the body cannot read the host's master
	// secrets out of its own process env.
	cmd.Env = childEnv(
		"SUBSTRATE_PY_MODULES="+modsDir,
		"PYTHONUNBUFFERED=1",
		"PYTHONDONTWRITEBYTECODE=1",
		"UV_CACHE_DIR="+filepath.Join(base, "uv-cache"),
	)
	p, err := startCmd(cmd)
	if err != nil {
		return nil, fmt.Errorf("runner: start isolated python: %w", err)
	}
	return p, nil
}

// writeFileRO materializes one file atomically as READ-ONLY: write a temp file
// in the destination directory, flush it to mode 0o444, then rename it over the
// target. Rename is atomic within a directory and replaces any prior read-only
// copy (the file mode does not block rename — the directory's write bit does,
// and the work dir stays 0o755). A restart re-materializes deterministically.
func writeFileRO(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o444); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// pyDir is the isolated-python scratch root: host scripts, shared-module dirs
// and uv's cache, stable across processes so a substrate restart reuses them.
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
