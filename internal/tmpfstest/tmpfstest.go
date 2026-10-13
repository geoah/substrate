// Package tmpfstest points a test binary's temporary files at a tmpfs. It
// imports nothing but the standard library, so a package whose tests fsync
// can take it without linking testdb's container client.
package tmpfstest

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

// Main runs a test binary with TMPDIR on a tmpfs where there is one (Use),
// and removes the directory after. A TestMain is `os.Exit(tmpfstest.Main(m))`.
func Main(m *testing.M) int {
	cleanup := Use()
	code := m.Run()
	cleanup()
	return code
}

// Use points TMPDIR, and with it every t.TempDir() of the binary, at a
// directory of its own under /dev/shm when that is a tmpfs with at least
// 512 MB free, and leaves it alone otherwise (macOS, a container whose
// /dev/shm is Docker's 64 MB default, TMPDIR already set). A suite calls it
// from TestMain before m.Run and runs the returned cleanup after. The engine
// fsyncs every changelog and sealed write, and sixteen parallel tests
// fsyncing one ext4 journal serialize on it; a tmpfs fsync is free, and the
// tests assert what the files hold, never that a power cut would keep them.
// `TMPDIR=/tmp` is the opt-out.
func Use() (cleanup func()) {
	none := func() {}
	if os.Getenv("TMPDIR") != "" {
		return none
	}
	const shm = "/dev/shm"
	var st syscall.Statfs_t
	if err := syscall.Statfs(shm, &st); err != nil || st.Type != tmpfsMagic {
		return none
	}
	// Bavail and Bsize are the types the platform declares, and they differ
	// across them, so the product is taken in uint64.
	if free := uint64(st.Bavail) * uint64(st.Bsize); free < 512<<20 { //nolint:unconvert // the field types are per platform
		return none
	}
	dir, err := os.MkdirTemp(shm, "substrate-test-")
	if err != nil {
		return none
	}
	if err := os.Setenv("TMPDIR", dir); err != nil {
		_ = os.RemoveAll(dir)
		return none
	}
	fmt.Fprintf(os.Stderr, "tmpfstest: TMPDIR=%s (tmpfs; set TMPDIR=/tmp to opt out)\n", dir)
	return func() { _ = os.RemoveAll(dir) }
}

// tmpfsMagic is TMPFS_MAGIC from linux/magic.h, what Statfs reports as the
// type of a tmpfs mount.
const tmpfsMagic = 0x01021994
