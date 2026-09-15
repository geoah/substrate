package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeFS is a filesystem as two maps: a regular file's size, and a
// directory's entry names. It carries no rule about what counts as a store —
// that rule is chooseTrust's, and restating it here would test nothing.
type fakeFS struct {
	files map[string]int64
	dirs  map[string][]string
}

func (f fakeFS) fileSize(path string) int64 {
	if size, ok := f.files[path]; ok {
		return size
	}
	return -1
}

func (f fakeFS) entries(path string) []string { return f.dirs[path] }

// hashed is the name OpenSSL actually looks a certificate up by inside a
// capath: the subject hash, a dot, and a sequence number.
const hashed = "5ed36f99.0"

func TestChooseTrust(t *testing.T) {
	bundles := []string{"/etc/ssl/cert.pem", "/etc/ssl/certs/ca-certificates.crt"}
	cases := []struct {
		name     string
		paths    verifyPaths
		fs       fakeFS
		want     string
		injected bool
	}{{
		// The ordinary Linux answer: python reports a store, it holds
		// certificates, the runner adds nothing.
		name:  "the interpreter's own bundle wins",
		paths: verifyPaths{CAFile: "/usr/lib/ssl/cert.pem", OpenSSLCAFile: "/usr/lib/ssl/cert.pem"},
		fs:    fakeFS{files: map[string]int64{"/usr/lib/ssl/cert.pem": 4096, "/etc/ssl/cert.pem": 333483}},
		want:  "/usr/lib/ssl/cert.pem",
	}, {
		name:  "a populated capath is a store too",
		paths: verifyPaths{CAPath: "/usr/lib/ssl/certs"},
		fs: fakeFS{
			files: map[string]int64{"/etc/ssl/cert.pem": 333483},
			dirs:  map[string][]string{"/usr/lib/ssl/certs": {hashed}},
		},
		want: "/usr/lib/ssl/certs",
	}, {
		name:  "a capath of plain pem files is a store",
		paths: verifyPaths{CAPath: "/usr/lib/ssl/certs"},
		fs:    fakeFS{dirs: map[string][]string{"/usr/lib/ssl/certs": {"ISRG_Root_X1.pem"}}},
		want:  "/usr/lib/ssl/certs",
	}, {
		// The bug: the framework build's compiled-in directory is empty, so
		// python reports neither, and the system bundle is named instead.
		name: "an absent interpreter store falls back to the system bundle",
		paths: verifyPaths{
			OpenSSLCAFile: "/Library/Frameworks/Python.framework/Versions/3.13/etc/openssl/cert.pem",
			OpenSSLCAPath: "/Library/Frameworks/Python.framework/Versions/3.13/etc/openssl/certs",
		},
		fs:       fakeFS{files: map[string]int64{"/etc/ssl/cert.pem": 333483}},
		want:     "/etc/ssl/cert.pem",
		injected: true,
	}, {
		// The P2 from review: the installer can leave the hashed directory in
		// PLACE and empty, so python reports a capath that exists. Taking its
		// word would suppress both the fallback and the warning while every
		// handshake still failed.
		name:  "an EMPTY capath directory is not a store",
		paths: verifyPaths{CAPath: "/Library/Frameworks/Python.framework/Versions/3.13/etc/openssl/certs"},
		fs: fakeFS{
			files: map[string]int64{"/etc/ssl/cert.pem": 333483},
			dirs:  map[string][]string{"/Library/Frameworks/Python.framework/Versions/3.13/etc/openssl/certs": {}},
		},
		want:     "/etc/ssl/cert.pem",
		injected: true,
	}, {
		// A capath holding only OpenSSL's bookkeeping verifies nothing: a
		// revocation list is not a root, and `.r0` is how one is named.
		name:  "a capath holding only CRLs and noise is not a store",
		paths: verifyPaths{CAPath: "/usr/lib/ssl/certs"},
		fs: fakeFS{
			files: map[string]int64{"/etc/ssl/cert.pem": 333483},
			dirs:  map[string][]string{"/usr/lib/ssl/certs": {"5ed36f99.r0", "README", "openssl.cnf"}},
		},
		want:     "/etc/ssl/cert.pem",
		injected: true,
	}, {
		// The same mistake in the other shape: a zero-byte bundle loads
		// cleanly and verifies nobody.
		name:  "a ZERO-BYTE cafile is not a store",
		paths: verifyPaths{CAFile: "/usr/lib/ssl/cert.pem"},
		fs: fakeFS{files: map[string]int64{
			"/usr/lib/ssl/cert.pem": 0,
			"/etc/ssl/cert.pem":     333483,
		}},
		want:     "/etc/ssl/cert.pem",
		injected: true,
	}, {
		name:  "a zero-byte system bundle is skipped for the next one",
		paths: verifyPaths{},
		fs: fakeFS{files: map[string]int64{
			"/etc/ssl/cert.pem":                  0,
			"/etc/ssl/certs/ca-certificates.crt": 200000,
		}},
		want:     "/etc/ssl/certs/ca-certificates.crt",
		injected: true,
	}, {
		name:     "the first usable bundle in list order is chosen",
		paths:    verifyPaths{},
		fs:       fakeFS{files: map[string]int64{"/etc/ssl/certs/ca-certificates.crt": 200000}},
		want:     "/etc/ssl/certs/ca-certificates.crt",
		injected: true,
	}, {
		// A reported path that has since gone is not a store. Trusting
		// python's word here would name a file OpenSSL then fails to load,
		// which reads as the runner's invention rather than the host's state.
		name:     "a reported store that does not exist is not one",
		paths:    verifyPaths{CAFile: "/usr/lib/ssl/cert.pem", CAPath: "/usr/lib/ssl/certs"},
		fs:       fakeFS{files: map[string]int64{"/etc/ssl/cert.pem": 333483}},
		want:     "/etc/ssl/cert.pem",
		injected: true,
	}, {
		// Nothing anywhere: name nothing. SSL_CERT_FILE pointing at a missing
		// file is worse than no variable at all.
		name:  "no store anywhere names nothing",
		paths: verifyPaths{},
		want:  "",
	}, {
		// A bundle path that is a DIRECTORY is not an SSL_CERT_FILE: OpenSSL
		// reads a concatenated file there, and a hashed directory is the other
		// variable entirely.
		name:  "a directory is never chosen as the bundle file",
		paths: verifyPaths{},
		fs:    fakeFS{dirs: map[string][]string{"/etc/ssl/cert.pem": {hashed}}},
		want:  "",
	}, {
		// Every store the interpreter reported is unusable AND the host has no
		// bundle: the case the boot line has to WARN about.
		name:  "an empty interpreter store with no system bundle names nothing",
		paths: verifyPaths{CAFile: "/f/cert.pem", CAPath: "/f/certs"},
		fs: fakeFS{
			files: map[string]int64{"/f/cert.pem": 0},
			dirs:  map[string][]string{"/f/certs": {}},
		},
		want: "",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chooseTrust(tc.paths, bundles, tc.fs)
			if got.Path != tc.want || got.Injected != tc.injected {
				t.Fatalf("chooseTrust = %+v, want {Path:%q Injected:%v}", got, tc.want, tc.injected)
			}
		})
	}
}

// What a capath entry has to look like to count. The hashed form is the one
// OpenSSL itself resolves, so getting it wrong would reject a real Debian
// store; `.r0` is a revocation list and must not stand in for a root.
func TestIsCertEntry(t *testing.T) {
	for _, name := range []string{
		"5ed36f99.0", "5ed36f99.12", "ABCDEF01.0",
		"ISRG_Root_X1.pem", "ca-certificates.crt",
	} {
		if !isCertEntry(name) {
			t.Errorf("%q is a certificate entry and was rejected", name)
		}
	}
	for _, name := range []string{
		"5ed36f99.r0", "README", "openssl.cnf", "",
		"private", "5ed36f9.0", "5ed36f99g.0", "notahash.0", ".", "cert.key",
	} {
		if isCertEntry(name) {
			t.Errorf("%q is not a certificate entry and was accepted", name)
		}
	}
}

// hostFS is the only part that touches a real filesystem, so it is held to the
// same two distinctions against a real one.
func TestHostFS(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.pem")
	full := filepath.Join(dir, "full.pem")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("-----BEGIN CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := hostFS{}
	if got := fs.fileSize(empty); got != 0 {
		t.Errorf("fileSize(empty) = %d, want 0", got)
	}
	if got := fs.fileSize(full); got <= 0 {
		t.Errorf("fileSize(full) = %d, want a positive size", got)
	}
	if got := fs.fileSize(dir); got != -1 {
		t.Errorf("fileSize(a directory) = %d, want -1", got)
	}
	if got := fs.fileSize(filepath.Join(dir, "gone")); got != -1 {
		t.Errorf("fileSize(missing) = %d, want -1", got)
	}
	if got := fs.entries(dir); len(got) != 2 {
		t.Errorf("entries = %v, want the two files", got)
	}
	if got := fs.entries(full); got != nil {
		t.Errorf("entries(a file) = %v, want nil", got)
	}
	if got := fs.entries(filepath.Join(dir, "gone")); got != nil {
		t.Errorf("entries(missing) = %v, want nil", got)
	}
}

// The variable is added on exactly the hosts that need it, and on no other:
// overriding an interpreter that already has a store would replace the
// operator's answer with the runner's.
func TestTrustStoreEnv(t *testing.T) {
	if env := (TrustStore{Path: "/usr/lib/ssl/cert.pem"}).env(); len(env) != 0 {
		t.Errorf("an interpreter with its own store got SSL_CERT_FILE forced on it: %v", env)
	}
	if env := (TrustStore{}).env(); len(env) != 0 {
		t.Errorf("no store found, yet something was named: %v", env)
	}
	env := (TrustStore{Path: "/etc/ssl/cert.pem", Injected: true}).env()
	if len(env) != 1 || env[0] != "SSL_CERT_FILE=/etc/ssl/cert.pem" {
		t.Errorf("the chosen bundle did not reach the child env: %v", env)
	}
}

// The probe must describe THIS host's interpreter, so a body sees exactly the
// store the boot line promises. Asserted as a relation rather than a value, so
// it holds on a Mac with the python.org build and on a Linux CI runner alike.
func TestPythonChildEnvCarriesTheChosenTrustStore(t *testing.T) {
	trust, err := pythonTrust()
	if err != nil {
		t.Skipf("no interpreter to probe: %v", err)
	}
	const probe = `
import os
def main(input, host):
    return {"output": os.environ.get("SSL_CERT_FILE", "")}
`
	r := New()
	spec := Spec{
		Repository: "t1", Function: "trust.g.test",
		Runtime: "python", Source: probe, TimeoutMs: 5000,
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	want := ""
	if trust.Injected {
		want = trust.Path
	}
	if got, _ := res.Output.(string); got != want {
		t.Fatalf("a body saw SSL_CERT_FILE=%q, want %q (trust %+v)", got, want, trust)
	}
}

// The probe's answer must be a real store, not a path nobody can open. This is
// the one assertion that would have caught the bug on the host it happens on,
// and it is a skip rather than a failure elsewhere for the same reason the TLS
// isolation test skips offline: a host with no CA package at all is a host
// choice, not a regression.
func TestThisHostsInterpreterHasATrustStore(t *testing.T) {
	trust, err := pythonTrust()
	if err != nil {
		t.Skipf("no interpreter to probe: %v", err)
	}
	if trust.Path == "" {
		t.Skip("this host has no certificate bundle at all: nothing to hold the runner to")
	}
	fs := hostFS{}
	if !isBundle(fs, trust.Path) && !isCertDir(fs, trust.Path) {
		t.Fatalf("the chosen trust store holds no certificate: %+v", trust)
	}
	if trust.Injected && !strings.HasPrefix(trust.Path, "/etc/") {
		t.Fatalf("the runner chose a bundle outside the system list: %+v", trust)
	}
}
