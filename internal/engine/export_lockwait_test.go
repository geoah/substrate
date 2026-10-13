package engine

import (
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// LockWaiters is how many sessions wait, ungranted, on the named advisory
// locks of d's repository (lockorder_db_test.go parkedOn), so a test outside
// the package waits for a racer to reach its lock instead of sleeping. A name
// is the engine's own, before the repository scopes it: ChangelogLockName,
// BlobLockName.
func LockWaiters(t *testing.T, d substrate.Dataset, names ...string) int {
	t.Helper()
	return parkedOn(t, d.(*dataset), names...)
}

// ChangelogLockName names the first lock of the global order (rows.go
// changelogLockKey), the one inTx takes before any other.
const ChangelogLockName = changelogLockKey

// BlobLockName names the per-digest lock a blob reference holds shared and
// the collection takes exclusive (blobs.go blobLockKey).
func BlobLockName(digest string) string { return blobLockKey(digest) }
