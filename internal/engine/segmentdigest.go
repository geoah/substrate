package engine

// The boot check and the open take every finished segment on its sidecar's
// word, reading its first and last lines, because digesting a long history
// before serving held a 16 GB repository's boot for 6.5 minutes (issue 825).
// A finished segment never changes, so the digest does not have to gate
// serving: every line a read returns is still held to its own sum. The
// server digests the segments after the open, one at a time, and a segment
// that does not match its sidecar latches the repository's writes refused
// (ErrChangelogDamaged), since a history known damaged must not grow on top
// of the damage. An operator process and a read-only one digest nothing in
// the background: `repository verify` digests every segment, and a snapshot
// holds what it copies to its own checks.

import (
	"context"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// ErrChangelogDamaged is the latched refusal of a repository whose finished
// changelog segment does not hash to its sidecar: the bytes on disk are not
// the history that was written, and only a restore of the directory from a
// backup repairs it.
var ErrChangelogDamaged = fmt.Errorf("%w: a finished changelog segment does not match its sidecar; restore the repository directory from a backup", substrate.ErrCorrupt)

// checkInBackground is the background digest's progress line.
const checkInBackground = "substrate: digesting the changelog segments the open did not read"

// startSegmentDigest starts the background digest of the finished segments
// the open took on their sidecars' word, when this process digests any and
// the open left some.
func (ds *dataset) startSegmentDigest() {
	log := ds.unread
	ds.unread = nil
	if !ds.svc.digestUnread || log == nil || log.Unread() == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ds.digestMu.Lock()
	ds.digestCancel, ds.digestDone = cancel, done
	ds.digestMu.Unlock()
	id := ds.info.ID
	started := ds.spawn("digest the changelog segments", func(bg context.Context) {
		defer close(done)
		defer cancel()
		stop := context.AfterFunc(bg, cancel)
		defer stop()
		began := time.Now()
		err := log.DigestUnread(ctx, ds.svc.checkProgress(checkInBackground, id))
		switch {
		case ctx.Err() != nil:
			// Closed or shutting down: the next open digests them again.
		case err != nil:
			ds.latchDamaged(err)
		default:
			ds.svc.log.Info("substrate: every finished changelog segment matches its sidecar",
				"repository", id, "segments", log.Unread(), "took", time.Since(began).Round(time.Millisecond))
		}
	})
	if !started {
		cancel()
		close(done)
	}
}

// stopSegmentDigest cancels the background digest and waits for it to
// return, up to the drain budget a detached task gets at shutdown.
func (ds *dataset) stopSegmentDigest() {
	ds.digestMu.Lock()
	cancel, done := ds.digestCancel, ds.digestDone
	ds.digestMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(backgroundDrainTimeout):
		ds.svc.log.Warn("substrate: the changelog segment digest did not stop in time",
			"repository", ds.info.ID)
	}
}

// latchDamaged refuses the repository's writes from now on, naming the
// segment, unless a refusal is latched already.
func (ds *dataset) latchDamaged(cause error) {
	ds.writerMu.Lock()
	defer ds.writerMu.Unlock()
	if ds.fileErr != nil {
		return
	}
	ds.fileErr = fmt.Errorf("%w: repository %s: %w", ErrChangelogDamaged, ds.info.ID, cause)
	ds.svc.log.Error("substrate: a finished changelog segment does not match its sidecar; refusing writes",
		"repository", ds.info.ID, "error", cause)
}
