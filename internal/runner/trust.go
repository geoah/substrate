package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
)

// What a function body verifies a TLS peer against.
//
// CPython does not carry certificates: it asks the OpenSSL it was linked
// against where the trust store is, and a build can answer with a path that
// does not exist. The python.org framework build on macOS is exactly that —
// its compiled-in `openssl_cafile` points inside the framework at a directory
// the installer leaves EMPTY, expecting the user to run `Install
// Certificates.command`, which installs `certifi` into that interpreter's
// site-packages — so every body that declares `network:` fails
// CERTIFICATE_VERIFY_FAILED with nothing on the wire or in the log to say why.
//
// The runner names the SYSTEM bundle instead of shipping one. `certifi` would
// be a second, stale root list to keep current inside every provisioned
// environment, and an operator who adds a root to the host (a corporate MITM
// proxy is the usual reason) expects bodies to honor it; a bundled list
// silently would not.

// systemCertBundles are the system certificate bundles the runner will name in
// SSL_CERT_FILE, in the order it tries them: macOS first, then the Debian,
// Fedora/RHEL and SUSE spellings. They are FILES, never the hashed directories
// beside them, so the sandbox grant this needs stays one file wide
// (TestEveryCertBundleTheRunnerMayChooseIsGranted holds that).
var systemCertBundles = []string{
	"/etc/ssl/cert.pem",
	"/etc/ssl/certs/ca-certificates.crt",
	"/etc/pki/tls/certs/ca-bundle.crt",
	"/etc/pki/tls/cert.pem",
	"/etc/ssl/ca-bundle.pem",
}

// SystemCertBundles is the list above, for the boot log: an operator on a host
// with no trust store at all needs to know where one was looked for.
func SystemCertBundles() []string { return slices.Clone(systemCertBundles) }

// TrustStore is the decision, for the child environment and for the boot line.
type TrustStore struct {
	// Path is the store in use: the interpreter's own where it has a usable
	// one, else the system bundle the runner chose. Empty means nothing on
	// this host holds a certificate, and every network body will fail its
	// handshake.
	Path string
	// Injected distinguishes the two: true when Path is the runner's choice,
	// passed to every child as SSL_CERT_FILE because the interpreter's own
	// store does not exist or holds no certificate.
	Injected bool
}

// verifyPaths is `ssl.get_default_verify_paths()`. `cafile` and `capath` are
// the RESOLVED answer — python applies the SSL_CERT_FILE/SSL_CERT_DIR
// environment overrides and reports nil for a path that does not exist — while
// the `openssl_*` pair is the raw compiled-in default, carried for the log
// only and never chosen from.
type verifyPaths struct {
	CAFile        string `json:"cafile"`
	CAPath        string `json:"capath"`
	OpenSSLCAFile string `json:"openssl_cafile"`
	OpenSSLCAPath string `json:"openssl_capath"`
}

// trustProbe asks the interpreter, rather than deriving an answer from its
// path or version: which OpenSSL a CPython is linked against, and what that
// build compiled in, is not something the runner can know from outside.
const trustProbe = `import ssl,json; p=ssl.get_default_verify_paths(); print(json.dumps({"cafile":p.cafile,"capath":p.capath,"openssl_cafile":p.openssl_cafile,"openssl_capath":p.openssl_capath}))`

// trustFS is the little the decision needs of a filesystem, and no rule: the
// rule about what counts as a store lives in chooseTrust, so a test fakes the
// filesystem without restating the thing under test.
type trustFS interface {
	// fileSize is the byte size of an existing regular file, and -1 for
	// anything else — a directory, a missing path, an unreadable one.
	fileSize(path string) int64
	// entries are an existing directory's entry names, and nil for anything
	// else. Names only: OpenSSL picks a certificate out of a hashed directory
	// by name, so that is all there is to look at.
	entries(path string) []string
}

// chooseTrust is the whole decision: the interpreter's answer, the bundle list
// and a filesystem in, one TrustStore out. Pure, so the decision can be tested
// without the python.org build that motivates it.
//
// A path that merely EXISTS is not a store. The python.org installer leaves
// its `etc/openssl/certs` directory in place and empty, so accepting an
// existing directory would take the interpreter's word for a store that
// verifies nothing — and would suppress both the fallback and the warning that
// say so, which is the whole failure this file exists to end. An empty bundle
// file is the same mistake in the other shape.
func chooseTrust(p verifyPaths, bundles []string, fs trustFS) TrustStore {
	// The interpreter's own store wins wherever it holds certificates, so an
	// operator who pointed the host's OpenSSL somewhere keeps that answer and
	// the runner adds nothing.
	if isBundle(fs, p.CAFile) {
		return TrustStore{Path: p.CAFile}
	}
	if isCertDir(fs, p.CAPath) {
		return TrustStore{Path: p.CAPath}
	}
	for _, bundle := range bundles {
		if isBundle(fs, bundle) {
			return TrustStore{Path: bundle, Injected: true}
		}
	}
	// Nothing found: name no store at all rather than one that cannot verify.
	// SSL_CERT_FILE pointing at a missing or empty file is worse than an absent
	// variable — OpenSSL fails the load and the error names the runner's
	// invention instead of the interpreter's own empty default.
	return TrustStore{}
}

// isBundle reports a concatenated PEM bundle with something in it. Size alone,
// because parsing to check is the TLS stack's job and a non-empty file that
// holds no certificate fails loudly at handshake, which is a legible failure;
// an empty one fails identically to no store at all, which is not.
func isBundle(fs trustFS, path string) bool {
	return path != "" && fs.fileSize(path) > 0
}

// isCertDir reports a capath OpenSSL can actually verify out of: a directory
// holding at least one HASH-NAMED certificate.
//
// Hash-named is the whole rule, because the lookup does not list the
// directory. OpenSSL takes the subject name it wants, hashes it, builds
// `<dir>/<hash>.<n>` and opens exactly that; a `ca.pem` sitting there unhashed
// is never read. So a directory of plain PEM files that nobody ran `openssl
// rehash` over verifies nothing, and counting it would suppress the fallback
// and the WARN while every handshake still failed — the same bug as an empty
// directory, one step subtler.
func isCertDir(fs trustFS, path string) bool {
	if path == "" {
		return false
	}
	for _, name := range fs.entries(path) {
		if isHashedCert(name) {
			return true
		}
	}
	return false
}

// isHashedCert matches the one name a capath lookup will open: eight lowercase
// hex digits of the subject hash, a dot, and a sequence number. Lowercase
// because that is the only spelling the lookup composes, so an uppercase name
// is unreachable on any case-sensitive filesystem.
//
// A `.r0` is excluded by the sequence being digits: that suffix is how a
// revocation list is named, and a directory holding only CRLs verifies nobody.
func isHashedCert(name string) bool {
	hash, seq, ok := strings.Cut(name, ".")
	if !ok || len(hash) != 8 || seq == "" {
		return false
	}
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	// Cut takes the FIRST dot, so a trailing `.bak` lands here and fails.
	for _, r := range seq {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// hostFS is the real filesystem.
type hostFS struct{}

func (hostFS) fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return -1
	}
	return st.Size()
}

func (hostFS) entries(path string) []string {
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// pythonTrust probes ONCE per process, like pythonInterpreter: the answer is a
// property of the host's interpreter, and a body start must not pay an exec
// for it.
//
// It probes the BASE interpreter even though a PEP 723 body runs on the venv
// interpreter uv built. That is sound because `uv sync --script` builds the
// venv ON this interpreter, so the venv's python is the same binary linked
// against the same OpenSSL and reports the same paths. Were uv ever to
// download an interpreter of its own instead, this assumption would need
// re-checking.
//
// The probe runs under childEnv(), not the substrate's environment, so it sees
// exactly what a body will: a host-set SSL_CERT_FILE that the child-env
// allowlist does not pass through must not make the probe optimistic.
var pythonTrust = sync.OnceValues(func() (TrustStore, error) {
	interpreter, err := pythonInterpreter()
	if err != nil {
		return TrustStore{}, err
	}
	cmd := exec.Command(interpreter, "-c", trustProbe)
	cmd.Env = childEnv()
	out, err := cmd.Output()
	if err != nil {
		return TrustStore{}, fmt.Errorf("runner: probe the interpreter's certificate store: %w", err)
	}
	var paths verifyPaths
	if err := json.Unmarshal(out, &paths); err != nil {
		return TrustStore{}, fmt.Errorf("runner: probe the interpreter's certificate store: %w", err)
	}
	return chooseTrust(paths, systemCertBundles, hostFS{}), nil
})

// Trust reports what function bodies will verify TLS against, for the boot
// log. An operator who reads "function sandbox active" and nothing else has no
// way to learn that every network body on this host fails its handshake before
// it sends a byte.
func (r *Runner) Trust() (TrustStore, error) { return pythonTrust() }

// env is the certificate-store assignment a child carries, and it is EMPTY
// unless the runner chose the store: an interpreter that already has one is
// left to find it the way it always did, so this adds a variable on exactly
// the hosts that are broken without it.
func (t TrustStore) env() []string {
	if !t.Injected {
		return nil
	}
	return []string{"SSL_CERT_FILE=" + t.Path}
}

// trustEnv is that assignment for this host's interpreter.
//
// A probe failure is swallowed here on purpose: it is spoken once at boot, and
// a body that declares no `network:` needs no trust store, so failing a start
// over it would break the functions that work to report on the ones that do
// not.
func trustEnv() []string {
	trust, err := pythonTrust()
	if err != nil {
		return nil
	}
	return trust.env()
}
