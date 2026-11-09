package networks

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// tempStore opens a store in a directory removed after the test. A bridge
// may still hold a connection to its database after Stop returns, which
// bridgev2 leaves to a background query and the driver's finalizer to
// close, so removal collects garbage and retries until the last one let go.
func tempStore(t *testing.T) *store.Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "chatbridge-networks-")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
			runtime.GC()
			err := os.RemoveAll(dir)
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("removing the store: %v", err)
				return
			}
		}
	})
	return st
}
