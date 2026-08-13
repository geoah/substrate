package runner

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/geoah/substrate/internal/sandbox"
)

// The Go ingestion path: an inline `runtime: go` body compiles at
// registration into a self-contained module — the body as `package body`,
// the embedded substratefn SDK beside it, a generated main wiring
// substratefn.Serve(body.Main) — built with the standard toolchain into a binary
// in a CONTENT-ADDRESSED cache. The cache key hashes everything the artifact
// depends on: the body, the embedded SDK, the generated wrapper and go.mod,
// the protocol version, the toolchain version and target (GOOS/GOARCH), and
// the build environment — so a substrate upgrade that changes any of them
// misses the cache and rebuilds instead of reusing an incompatible binary.
// The module has no dependencies beyond the stdlib, so builds are hermetic
// (GOFLAGS/GOPROXY pinned off) and a cache hit costs one stat.

//go:embed substratefn/substratefn.go
var substratefnSource string

const generatedMain = `package main

import (
	"substratefn.local/body"
	"substratefn.local/substratefn"
)

func main() { substratefn.Serve(body.Main) }
`

const generatedGoMod = `module substratefn.local

go 1.26
`

// goBuildEnv is the pinned build environment; it participates in the cache
// key, so changing a flag invalidates every cached artifact.
var goBuildEnv = []string{"GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off", "CGO_ENABLED=0"}

// toolchain is what one `go env` probe tells us: the identity that keys the
// build cache, and the directories the build reads and writes — which the
// sandbox needs by name, because a confined build cannot discover them.
type toolchain struct {
	// ID is version/os/arch, the part that participates in the cache key.
	ID string
	// Root, Cache and ModCache are GOROOT, GOCACHE and GOMODCACHE.
	Root, Cache, ModCache string
}

// goToolchain probes the `go` the builds actually use, once per process. A
// probe failure surfaces at build time anyway, so the error is carried, not
// swallowed.
var goToolchain = sync.OnceValues(func() (toolchain, error) {
	out, err := exec.Command("go", "env",
		"GOVERSION", "GOOS", "GOARCH", "GOROOT", "GOCACHE", "GOMODCACHE").Output()
	if err != nil {
		return toolchain{}, fmt.Errorf("runner: probe go toolchain: %w", err)
	}
	f := strings.Fields(string(out))
	if len(f) < 6 {
		return toolchain{}, fmt.Errorf("runner: probe go toolchain: got %d values, want 6", len(f))
	}
	return toolchain{
		ID:       strings.Join(f[:3], "/"),
		Root:     f[3],
		Cache:    f[4],
		ModCache: f[5],
	}, nil
})

// buildKey is the artifact's cache identity: body + SDK + wrapper + protocol
// + toolchain + target + build flags. Content-addressed and repository-free on
// purpose — the artifact is immutable, so SHARING it across installations is
// safe; only live processes are repository-keyed.
func buildKey(spec Spec) (string, error) {
	tool, err := goToolchain()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, part := range []string{
		"protocol " + strconv.Itoa(ProtocolVersion),
		tool.ID,
		strings.Join(goBuildEnv, " "),
		generatedGoMod,
		generatedMain,
		substratefnSource,
		spec.Source,
	} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	// Shared `.go` modules are vendored into the build, so they belong in the
	// key: changing one rebuilds the binary.
	mods := spec.goModules()
	for _, name := range sortedKeys(mods) {
		h.Write([]byte("lib/" + name + "\x00" + mods[name]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16]), nil
}

// ensureBinary returns the cached binary for one body, building it on the
// first sight of the cache key. A cache entry that is not a regular
// executable file is invalidated and rebuilt, never trusted.
func (r *Runner) ensureBinary(ctx context.Context, spec Spec) (string, error) {
	dir, err := r.binDir()
	if err != nil {
		return "", err
	}
	key, err := buildKey(spec)
	if err != nil {
		return "", err
	}
	tool, err := goToolchain()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, key)
	if fi, err := os.Stat(bin); err == nil {
		if fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return bin, nil
		}
		// A directory, a truncated stub, a stripped mode: not an artifact.
		_ = os.RemoveAll(bin)
	}
	work, err := os.MkdirTemp("", "substrate-fnbuild-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(work) }()

	body := spec.Source
	// The source is the body package: a package clause is optional, and must
	// be `package body` when present.
	if !strings.HasPrefix(strings.TrimSpace(body), "package ") {
		body = "package body\n\n" + body
	}
	files := map[string]string{
		"go.mod":                     generatedGoMod,
		"main.go":                    generatedMain,
		"body/body.go":               body,
		"substratefn/substratefn.go": substratefnSource,
	}
	// Shared bundle modules land in the `substratefn.local/lib` package, so a
	// body may `import "substratefn.local/lib"` to dedup helpers across the
	// bundle's functions. Each file must declare `package lib`; an
	// unimported lib package is simply not compiled by `go build .`.
	for name, src := range spec.goModules() {
		files[filepath.Join("lib", name)] = src
	}
	for name, content := range files {
		path := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", err
		}
	}
	// A unique temp output per builder: two concurrent builds of one hash
	// (the warm goroutine racing the first delivery) must not share a path.
	tmp, err := os.CreateTemp(dir, key+"-*.tmp")
	if err != nil {
		return "", err
	}
	out := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(out) }()
	// GOROOT's own `go`, not PATH's: PATH may hold a version manager's shim,
	// and the confined build would then be granted the wrapper rather than the
	// toolchain it re-execs. The probe already told us where the toolchain is.
	goBin := "go"
	if tool.Root != "" {
		goBin = filepath.Join(tool.Root, "bin", "go")
	}
	cmd := exec.CommandContext(ctx, goBin, "build", "-o", out, ".")
	cmd.Dir = work
	// Hermetic: no network, no workspace interference, and no stale GOROOT
	// leaking in from the parent environment — the found toolchain knows its
	// own root.
	// The toolchain writes its own scratch — a link step, a vet run — and it
	// takes the path from TMPDIR. Pointing it inside the build's work dir
	// keeps the shared /tmp out of the policy.
	buildTmp, err := scratch(work)
	if err != nil {
		return "", err
	}
	cmd.Env = append(goBuildChildEnv(), "TMPDIR="+buildTmp)
	// The build compiles UNTRUSTED source, so it is confined too — a looser
	// policy than a body's, because a build legitimately writes the module and
	// build caches, but the same shape: no network (the module is stdlib-only
	// and GOPROXY is off, so a build that reaches for the network is a build
	// doing something it was never meant to) and no filesystem beyond the
	// toolchain, the caches and its own scratch.
	if err := r.sandbox.Wrap(cmd, buildPolicy(tool, work, dir)); err != nil {
		return "", err
	}
	if raw, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("runner: go build: %w\n%s", err, raw)
	}
	// Rename-into-place keeps concurrent builders of one hash safe: both
	// build, one rename wins, both run the identical artifact.
	if err := os.Rename(out, bin); err != nil {
		return "", err
	}
	return bin, nil
}

// goWorkDir is one installation's runtime directory for a Go body: where its
// scratch lives and the only writable path its sandbox grants. Keyed by
// workID() — the full installation key — like the python one, so two
// repositories running byte-identical source never share a directory.
func (r *Runner) goWorkDir(spec Spec) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "substrate-runner", "go", spec.workID())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// binDir is the build cache: stable across processes so a substrate restart
// costs no rebuilds.
func (r *Runner) binDir() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cacheDir != "" {
		return r.cacheDir, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "substrate-runner", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	r.cacheDir = dir
	return dir, nil
}

// goBuildChildEnv is the go-build child's environment: the minimal allowlisted
// base (childEnv — PATH, HOME, TMPDIR, locale) plus ONLY the Go toolchain
// variables a hermetic build legitimately reads (the build/module caches and
// the module/toolchain locations), then the pinned build flags. Default-deny
// like every runner child: no SUBSTRATE_*/DATABASE_URL
// reach the build — a `go build` never needs a host secret. GOROOT/GOFLAGS/
// GOWORK are deliberately NOT passed through: the found `go` knows its own
// root, and GOFLAGS/GOWORK are pinned by goBuildEnv, appended LAST so they win.
func goBuildChildEnv() []string {
	toolchain := passEnv(
		"GOCACHE", "GOMODCACHE", "GOPATH", "GOTOOLCHAIN",
	)
	return append(childEnv(toolchain...), goBuildEnv...)
}

// buildPolicy confines one `go build`. The build cache and module cache are
// writable — a build cannot work otherwise, and they are content-addressed —
// and so is the artifact directory, because the build writes its output there
// and renames it into place. Every one of those is shared across
// installations, which is why the BODY that comes out gets a policy where they
// are read-only: the compiler may write them, the compiled body may not.
func buildPolicy(tool toolchain, work, binDir string) sandbox.Policy {
	rw := append([]string{work, binDir}, deviceReadWrite...)
	for _, dir := range []string{tool.Cache, tool.ModCache} {
		if dir != "" {
			rw = append(rw, dir)
		}
	}
	readExec := append([]string{}, systemReadExec...)
	if tool.Root != "" {
		readExec = append(readExec, tool.Root)
	}
	return sandbox.Policy{
		ReadExec:  readExec,
		ReadOnly:  systemReadOnly,
		ReadWrite: rw,
		NoFile:    childNoFile,
		FileSize:  childFileSize,
	}
}
