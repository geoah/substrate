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
// skipping, and a run whose confinement cases did not actually run fails too.
// CI sets it (`ci:go`, `ci:race`); a developer box does not, so the rest of
// the suite still runs there.
package sandboxtest

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"testing"
)

// EnvRequire is set by CI. Any non-empty value other than "0" turns the skips
// into failures.
const EnvRequire = "SUBSTRATE_TEST_REQUIRE_SANDBOX"

// The two sets answer different questions and neither one alone is the gate.
// guarded is which cases got past the confinement check, which is a property
// of the source: on any host that offers the confinement it is the same set,
// so it can be held to an exact size. asserted is which of them then reached
// their assertions instead of skipping on a later precondition (a uid that
// cannot mknod, a probe that will not build), which is a property of the
// machine and can only be given a floor.
//
// Names and not counters, because -count=N and -cpu=1,2 both run the whole set
// more than once, and a counter would make the exact size mean "times the
// number of repeats" rather than "the cases this package has".
var (
	mu       sync.Mutex
	guarded  = map[string]bool{}
	asserted = map[string]bool{}
)

// Required reports whether this run must actually exercise the confinement.
func Required() bool {
	v := os.Getenv(EnvRequire)
	return v != "" && v != "0"
}

// Unavailablef ends the case: a skip normally, a failure when the confinement
// is required.
func Unavailablef(t *testing.T, format string, args ...any) {
	t.Helper()
	if Required() {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

// Case records one confinement case. Call it from the guard, once the guard
// has found the confinement in place. The second record is taken at the END of
// the case and only when it did not skip, because a case that stops at a later
// precondition proved nothing and must not be able to report that it did.
func Case(t *testing.T) {
	t.Helper()
	name := t.Name()
	mu.Lock()
	guarded[name] = true
	mu.Unlock()
	t.Cleanup(func() {
		if t.Skipped() {
			return
		}
		mu.Lock()
		asserted[name] = true
		mu.Unlock()
	})
}

// Run is TestMain for a package with confinement cases: it runs the suite and,
// when the confinement is required, refuses a pass that did not prove what the
// package promises.
func Run(m *testing.M, want int) int {
	code := m.Run()
	if code != 0 || !Required() {
		return code
	}
	// -list prints names and -count=0 asks for nothing; neither is a suite
	// that could have confined anything, so neither is one to judge.
	if listing() || repeats() == "0" {
		return code
	}
	mu.Lock()
	g, a := len(guarded), len(asserted)
	mu.Unlock()
	// The size is held EXACTLY, and a -run/-skip relaxes it only when it
	// actually narrowed the set. Presence of a filter is not enough: a
	// package-wide `-skip '^TestLive'` copied onto the wrong task (test:db
	// carries one, so the flag is easy to copy) leaves every confinement case
	// running, and that run must still be checked.
	switch {
	case g == want:
		if a == 0 {
			return failf("%s is set, and all %d confinement cases skipped after their guard, "+
				"so nothing was actually confined", EnvRequire, g)
		}
	// A floor would let a ninth case be added without touching the number, and
	// one of the nine be deleted afterwards with the gate still green.
	case g > want:
		return failf("%s is set, and %d confinement cases ran their guard, want exactly %d: "+
			"a guarded case was added without changing the number in TestMain", EnvRequire, g, want)
	case filtered() && g > 0:
		fmt.Fprintf(os.Stderr, "%s: -run/-skip narrowed this binary to %d of %d confinement "+
			"cases, so their number is not checked\n", EnvRequire, g, want)
	case filtered():
		return failf("%s is set, but the -run/-skip filter left no confinement case to run", EnvRequire)
	default:
		return failf("%s is set, and %d confinement cases ran their guard, want exactly %d: "+
			"either this kernel offers no Landlock or no seccomp, or a guarded case was "+
			"removed without changing the number in TestMain", EnvRequire, g, want)
	}
	return code
}

// failf writes the reason to stderr and answers with the exit code TestMain
// hands to os.Exit.
func failf(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	return 1
}

// filtered reports whether the test binary was narrowed on the command line.
// The flags are registered by testing's init and parsed by m.Run, so this and
// the two below are only meaningful after it.
func filtered() bool {
	for _, name := range []string{"test.run", "test.skip"} {
		if f := flag.Lookup(name); f != nil && f.Value.String() != "" {
			return true
		}
	}
	return false
}

// listing reports whether -list was asked for, which prints test names and
// runs none of them.
func listing() bool {
	f := flag.Lookup("test.list")
	return f != nil && f.Value.String() != ""
}

// repeats is -count, whose only interesting value here is 0: the flag's other
// values change how many TIMES each case runs, which the name sets above
// already absorb.
func repeats() string {
	f := flag.Lookup("test.count")
	if f == nil {
		return "1"
	}
	return f.Value.String()
}
