package changelogfile

import (
	"os"
	"testing"

	"github.com/geoah/substrate/internal/tmpfstest"
)

// Every writer test fsyncs its segment files, which on a disk-backed TMPDIR
// costs most of the package's run time; on a tmpfs an fsync is free.
func TestMain(m *testing.M) {
	os.Exit(tmpfstest.Main(m))
}
