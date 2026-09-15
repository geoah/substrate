package runner

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// fakeFS answers the two predicates chooseTrust takes from a fixed listing, so
// the decision is tested on every platform's layout from any platform — the
// python.org framework build that motivates this code is on nobody's CI.
func fakeFS(files, dirs []string) (func(string) bool, func(string) bool) {
	return func(p string) bool { return slices.Contains(files, p) },
		func(p string) bool { return slices.Contains(dirs, p) }
}

func TestChooseTrust(t *testing.T) {
	bundles := []string{"/etc/ssl/cert.pem", "/etc/ssl/certs/ca-certificates.crt"}
	cases := []struct {
		name     string
		paths    verifyPaths
		files    []string
		dirs     []string
		want     string
		injected bool
	}{{
		// The ordinary Linux answer: python reports a store, it exists, the
		// runner adds nothing.
		name:  "the interpreter's own bundle wins",
		paths: verifyPaths{CAFile: "/usr/lib/ssl/cert.pem", OpenSSLCAFile: "/usr/lib/ssl/cert.pem"},
		files: []string{"/usr/lib/ssl/cert.pem", "/etc/ssl/cert.pem"},
		want:  "/usr/lib/ssl/cert.pem",
	}, {
		name:  "a capath the interpreter reports is a store too",
		paths: verifyPaths{CAPath: "/usr/lib/ssl/certs"},
		dirs:  []string{"/usr/lib/ssl/certs"},
		files: []string{"/etc/ssl/cert.pem"},
		want:  "/usr/lib/ssl/certs",
	}, {
		// The bug: the framework build's compiled-in directory is empty, so
		// python reports neither, and the system bundle is named instead.
		name: "an empty interpreter store falls back to the system bundle",
		paths: verifyPaths{
			OpenSSLCAFile: "/Library/Frameworks/Python.framework/Versions/3.13/etc/openssl/cert.pem",
			OpenSSLCAPath: "/Library/Frameworks/Python.framework/Versions/3.13/etc/openssl/certs",
		},
		files:    []string{"/etc/ssl/cert.pem"},
		want:     "/etc/ssl/cert.pem",
		injected: true,
	}, {
		name:     "the first existing bundle in list order is chosen",
		paths:    verifyPaths{},
		files:    []string{"/etc/ssl/certs/ca-certificates.crt"},
		want:     "/etc/ssl/certs/ca-certificates.crt",
		injected: true,
	}, {
		// A reported path that has since gone is not a store. Trusting
		// python's word here would name a file OpenSSL then fails to load,
		// which reads as the runner's invention rather than the host's state.
		name:     "a reported store that does not exist is not one",
		paths:    verifyPaths{CAFile: "/usr/lib/ssl/cert.pem", CAPath: "/usr/lib/ssl/certs"},
		files:    []string{"/etc/ssl/cert.pem"},
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
		dirs:  []string{"/etc/ssl/cert.pem"},
		want:  "",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isFile, isDir := fakeFS(tc.files, tc.dirs)
			got := chooseTrust(tc.paths, bundles, isFile, isDir)
			if got.Path != tc.want || got.Injected != tc.injected {
				t.Fatalf("chooseTrust = %+v, want {Path:%q Injected:%v}", got, tc.want, tc.injected)
			}
		})
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
	if !isFile(trust.Path) && !isDir(trust.Path) {
		t.Fatalf("the chosen trust store does not exist: %+v", trust)
	}
	if trust.Injected && !strings.HasPrefix(trust.Path, "/etc/") {
		t.Fatalf("the runner chose a bundle outside the system list: %+v", trust)
	}
}
