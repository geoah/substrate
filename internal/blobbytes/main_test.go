package blobbytes_test

import (
	"os"
	"testing"

	"github.com/geoah/substrate/internal/tmpfstest"
)

// The filesystem store fsyncs every object it writes, which on a disk-backed
// TMPDIR costs most of the package's run time; on a tmpfs an fsync is free.
func TestMain(m *testing.M) {
	os.Exit(tmpfstest.Main(m))
}
