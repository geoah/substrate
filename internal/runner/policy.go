package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/geoah/substrate/internal/sandbox"
)

// The confinement policy for one child, derived from the installation it
// serves and the capability envelope the manifest declared. Two rules shape
// every policy here:
//
//   - Nothing SHARED is writable. The Go build cache, uv's cache and the
//     interpreter prefixes are read-and-execute; only the installation's own
//     work dir and its private scratch are writable. A writable shared cache is
//     a cross-installation code-execution vector — plant an artifact under the
//     hash a neighbor will exec — and it survives one-process-per-installation
//     untouched, so the filesystem layer is the only thing that closes it.
//   - Nothing is granted under /proc. That is what makes the runner's env
//     allowlist a boundary rather than a gesture: with no rule naming it,
//     `open("/proc/1/environ")` cannot reach the substrate's own environment,
//     where the credential key and the database URL live.
//
// A path that does not exist is skipped by the sandbox rather than refused, so
// one list can cover the alpine image, a Debian dev box and a macOS-hosted
// container without a per-platform table.

// systemReadExec are the prefixes an interpreter needs to run at all: the
// binary, its shared libraries, its standard library. Read and execute, never
// write — a body cannot rewrite the interpreter the next one will start.
var systemReadExec = []string{
	"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/opt",
}

// systemReadOnly is configuration a runtime reads and must never execute: the
// certificate store, the resolver configuration, the user database musl and
// glibc both consult.
var systemReadOnly = []string{"/etc"}

// deviceReadWrite are the character devices every runtime opens as a matter of
// course — CPython rebinds stdin to /dev/null before a body runs, the Go
// toolchain writes build output to it, and both draw seeds from urandom. They
// are named ONE BY ONE rather than granting /dev, which would hand a body the
// block devices, /dev/mem and the terminal along with them.
var deviceReadWrite = []string{
	"/dev/null", "/dev/zero", "/dev/full", "/dev/random", "/dev/urandom",
}

// The rlimit ceilings. Both are generous — they exist to stop an accident from
// filling a disk or exhausting the substrate's descriptors, not to meter a
// body, which needs a cgroup the deployment cannot give us (/sys/fs/cgroup is
// a read-only mount in a stock container).
const (
	childNoFile   = 1024
	childFileSize = 512 << 20
)

// policyFor builds the confinement for one body process. work is the
// installation's own directory — the one thing it may write — and readExec
// carries whatever else it must be able to run: the venv interpreter uv
// provisioned, or the build cache the compiled binary lives in.
func policyFor(spec Spec, work string, readExec ...string) sandbox.Policy {
	return sandbox.Policy{
		ReadExec:  append(append([]string{}, systemReadExec...), readExec...),
		ReadOnly:  systemReadOnly,
		ReadWrite: append([]string{work}, deviceReadWrite...),
		// The whole enforcement of `capabilities.network`: a manifest that
		// declares no egress gets none, at the syscall. It is binary on
		// purpose — a syscall filter cannot read the sockaddr behind a
		// pointer, so per-host allowlisting needs an egress proxy, and
		// pretending otherwise would be worse than saying so.
		Network:  len(spec.Network) > 0,
		NoFile:   childNoFile,
		FileSize: childFileSize,
	}
}

// pythonInterpreter is the ACTUAL CPython binary, asked of python3 itself
// rather than taken from PATH.
//
// PATH is not good enough once children are confined. A version manager (mise,
// pyenv, asdf) puts a SHIM on PATH — a wrapper that re-execs the real
// interpreter from a versioned directory somewhere else entirely — so a policy
// derived from the PATH entry would grant the wrapper and deny what it runs.
// `sys.executable` is the interpreter's own answer to "where am I", so the
// runner execs the real binary and the grant below names the tree it actually
// needs. On the image, where python3 is /usr/bin/python3, this resolves to
// itself and changes nothing.
var pythonInterpreter = sync.OnceValues(func() (string, error) {
	out, err := exec.Command("python3", "-c", "import sys; print(sys.executable)").Output()
	if err != nil {
		return "", fmt.Errorf("runner: locate python3: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("runner: python3 reported no executable path")
	}
	return path, nil
})

// runtimeRoot is the tree an interpreter needs granted, given its binary: the
// directory holding it, and — when that directory is a `bin` — its parent, so
// the `lib` beside it comes too. That is the layout of every prefix-installed
// runtime, from /usr on down.
//
// The parent is never taken past a top-level directory or a home directory: a
// grant that wide would defeat the point.
func runtimeRoot(bin string) []string {
	dir := filepath.Dir(bin)
	out := []string{dir}
	if filepath.Base(dir) != "bin" {
		return out
	}
	parent := filepath.Dir(dir)
	if parent == "/" || parent == "." {
		return out
	}
	if home, err := os.UserHomeDir(); err == nil && parent == home {
		return out
	}
	return append(out, parent)
}

// scratch is the installation's private temp dir, created under its work dir
// and handed to the child as TMPDIR. Without it a body would need the shared
// /tmp, which is both an escape from the work-dir grant and a place two
// installations meet.
func scratch(work string) (string, error) {
	dir := filepath.Join(work, "tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
