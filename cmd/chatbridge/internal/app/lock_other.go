//go:build !unix

package app

// lock is a no-op where flock is not available; run one process per store.
func lock(string) (func(), error) { return func() {}, nil }
