package metrics

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
)

// noConnector is a pool that never dials: the collector only reads the
// pool's counters, and sql.OpenDB does not connect until a query asks.
type noConnector struct{}

func (noConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("no connections in this test")
}
func (noConnector) Driver() driver.Driver { return nil }

// published reports whether a go_sql_* series under db_name=name is in the
// registry.
func published(t *testing.T, name string) bool {
	t.Helper()
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "go_sql_open_connections" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "db_name" && l.GetValue() == name {
					return true
				}
			}
		}
	}
	return false
}

// A pool reopened under the same name takes the name over, and the OLD pool's
// unregister, arriving after, must not drop the new pool's collector.
func TestRegisterDBStatsTakeoverSurvivesTheStaleUnregister(t *testing.T) {
	const name = "repository:takeover.test"
	oldDB, newDB := sql.OpenDB(noConnector{}), sql.OpenDB(noConnector{})
	t.Cleanup(func() { _ = oldDB.Close(); _ = newDB.Close() })

	unregisterOld := RegisterDBStats(name, oldDB)
	unregisterNew := RegisterDBStats(name, newDB)
	unregisterOld()
	if !published(t, name) {
		t.Fatal("the stale unregister dropped the live pool's collector")
	}
	unregisterNew()
	if published(t, name) {
		t.Fatal("the live pool's unregister left its collector published")
	}
}

// The shared pool's reading is published under its name and dropped by its
// unregister, so an exhausted pool (acquired at the cap, waits climbing) is
// something a scrape shows.
func TestPoolStatsArePublishedAndDropped(t *testing.T) {
	unregister := RegisterPoolStats("test-pool", func() PoolStats {
		return PoolStats{Max: 4, Total: 4, Acquired: 4, Waits: 7, WaitDuration: 1.5}
	})
	value := func() (float64, bool) {
		families, err := Registry.Gather()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range families {
			if f.GetName() != "substrate_db_pool_waits_total" {
				continue
			}
			for _, m := range f.GetMetric() {
				for _, l := range m.GetLabel() {
					if l.GetName() == "pool" && l.GetValue() == "test-pool" {
						return m.GetCounter().GetValue(), true
					}
				}
			}
		}
		return 0, false
	}
	if v, ok := value(); !ok || v != 7 {
		t.Fatalf("substrate_db_pool_waits_total{pool=test-pool} = %v (published %v), want 7", v, ok)
	}
	unregister()
	if _, ok := value(); ok {
		t.Fatal("the pool's series outlived its unregister")
	}
}
