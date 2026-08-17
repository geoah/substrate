// Package sandboxtest is the gate that keeps the confinement suites honest.
//
// The cases that assert what a function body cannot do (read the substrate's
// environment, write outside its work dir, open a socket it was not granted)
// can only run where the kernel offers Landlock and seccomp, so they skip
// where it does not: a laptop with Landlock left out of its lsm= list is an
// environment difference, not a regression. That skip is also how the promise
// disappears without a red build, because an image change, a distribution
// lsm= change or a runner upgrade turns every one of those cases from passing
// to skipping and nothing notices.
//
// SUBSTRATE_TEST_REQUIRE_SANDBOX=1 is the answer: the guards fail instead of
// skipping, and a run that proved fewer cases than the package declares fails
// too. CI sets it (`ci:go`, `ci:race`); a developer box does not, so the rest
// of the suite still runs there.
package sandboxtest

import (
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
)

// EnvRequire is set by CI. Any non-empty value other than "0" turns the skips
// into failures.
const EnvRequire = "SUBSTRATE_TEST_REQUIRE_SANDBOX"

var ran atomic.Int64

// Required reports whether this run must actually exercise the confinement.
func Required() bool {
	v := os.Getenv(EnvRequire)
	return v != "" && v != "0"
}

// Unavailable ends the case: a skip normally, a failure when the confinement
// is required.
func Unavailable(t *testing.T, format string, args ...any) {
	t.Helper()
	if Required() {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

// Ran records one case that reached its assertions with the confinement in
// place. Call it from the guard, after the guard passes.
func Ran() { ran.Add(1) }

// Run is TestMain for a package with confinement cases: it runs the suite and,
// when the confinement is required, refuses a pass that counted fewer than
// want cases. A suite narrowed by -run or -skip is exempt, because a count
// over a subset means nothing.
func Run(m *testing.M, want int) int {
	code := m.Run()
	if code != 0 || !Required() || filtered() {
		return code
	}
	if got := ran.Load(); got < int64(want) {
		fmt.Fprintf(os.Stderr, "%s is set, but only %d confinement cases ran (want %d): "+
			"the kernel offers no Landlock or no seccomp, so nothing here proved anything\n",
			EnvRequire, got, want)
		return 1
	}
	return code
}

// filtered reports whether the test binary was narrowed on the command line.
// The flags are registered by testing's init and parsed by m.Run, so this is
// only meaningful after it.
func filtered() bool {
	for _, name := range []string{"test.run", "test.skip"} {
		if f := flag.Lookup(name); f != nil && f.Value.String() != "" {
			return true
		}
	}
	return false
}
