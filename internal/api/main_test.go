package api

import (
	"net/netip"
	"os"
	"testing"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/testdb"
)

// Every function body a dbEnv's engine starts is confined to loopback
// (runner.ConfineBodyEgress): a body a broken test or a regression points at
// a real API is refused at connect(2) and never reaches it, and a host that
// cannot filter a body's connects, macOS among them, refuses to start the
// body, so such a test fails there instead. testdb.Main: the data roots on
// tmpfs, the run, then every database the run's dbEnvs made dropped.
func TestMain(m *testing.M) {
	runner.Shared.ConfineBodyEgress(netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128"))
	os.Exit(testdb.Main(m))
}
