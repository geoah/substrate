package catalog_test

import (
	"os"
	"testing"

	"github.com/geoah/substrate/internal/testdb"
)

// TestMain is testdb.Main: the data roots on tmpfs, the run, every database
// the run made dropped, and the Postgres container stopped as the binary
// ends rather than when the whole `go test` run does.
func TestMain(m *testing.M) {
	os.Exit(testdb.Main(m))
}
