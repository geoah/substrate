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
//
// THE DIGEST YIELDS TO REAL WORK. Hashing is one core busy for as long as
// the bytes last: on a four-core arm64 box a 27 GB history took 2.3 cores
// for ten minutes after the open on 2026-10-06, a function that takes 5 s
// alone took 45 s beside it and met its deadline, and a scheduled sync
// parked. So the digest is paced two ways (digestPacer). It hashes no more
// than a configured number of bytes per second, 8 MiB by default
// (SUBSTRATE_DIGEST_BYTES_PER_SECOND), which on that box is well under half
// a core; and it pauses, within one read of a megabyte, while any function
// body or agent loop runs in this process, and goes on when the last of
// them returns. The pause is what keeps a runner deadline from firing that
// would not have fired without the digest: the hashing stops before an
// invocation has run for a tenth of a second.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
)

// ErrChangelogDamaged is the latched refusal of a repository whose finished
// changelog segment does not hash to its sidecar: the bytes on disk are not
// the history that was written, and only a restore of the directory from a
// backup repairs it.
var ErrChangelogDamaged = fmt.Errorf("%w: a finished changelog segment does not match its sidecar; restore the repository directory from a backup", substrate.ErrCorrupt)

// checkInBackground is the background digest's progress line.
const checkInBackground = "substrate: digesting the changelog segments the open did not read"

// DefaultDigestBytesPerSecond is the rate the background digests hash at
// when no option names one: 8 MiB per second over every repository the
// process has open, sized for a four-core arm64 box without SHA-256
// instructions, where it is under half a core, and a 27 GB history takes
// about an hour.
const DefaultDigestBytesPerSecond int64 = 8 << 20

// digestYieldPoll is how often a paused digest asks whether every invocation
// has returned.
const digestYieldPoll = 50 * time.Millisecond

// digestProgressFactor is how much rarer the digest's progress lines are
// than the open's: the digest runs for an hour behind a large repository,
// and a line every 30 s would be most of the log. Every five minutes, at the
// default interval.
const digestProgressFactor = 10

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
	segments, bytes := log.Unread(), log.UnreadBytes()
	started := ds.spawn("digest the changelog segments", func(bg context.Context) {
		defer close(done)
		defer cancel()
		stop := context.AfterFunc(bg, cancel)
		defer stop()
		began := time.Now()
		pacer := ds.svc.newDigestPacer(id)
		ds.svc.log.Info("substrate: digesting the finished changelog segments behind the open",
			"repository", id, "segments", segments, "bytes", bytes, "bytesPerSecond", pacer.rate())
		err := log.DigestUnread(ctx, changelogfile.DigestOptions{
			Progress: ds.svc.checkProgressEvery(checkInBackground, id, ds.svc.progressEvery*digestProgressFactor),
			Pace:     pacer.pace,
		})
		switch {
		case err == nil:
			ds.svc.log.Info("substrate: every finished changelog segment matches its sidecar",
				"repository", id, "segments", segments, "bytes", bytes,
				"paused", pacer.paused.Round(time.Millisecond), "took", time.Since(began).Round(time.Millisecond))
		case errors.Is(err, changelogfile.ErrSegmentDigest):
			ds.latchDamaged(err)
		case errors.Is(err, context.Canceled):
			// Closed or shutting down: the next open digests them again.
			ds.svc.log.Info("substrate: the changelog segment digest stopped before it finished; the next open digests the rest again",
				"repository", id, "bytes", pacer.read, "totalBytes", bytes, "took", time.Since(began).Round(time.Millisecond))
		default:
			// A read that failed (an I/O error) proves no damage: logged,
			// and the next open digests again.
			ds.svc.log.Warn("substrate: the changelog segment digest stopped",
				"repository", id, "error", err)
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

// digestPacer holds one background digest to its two bounds: the bytes the
// PROCESS may hash per second, a token bucket every open repository's digest
// draws from (service.digestBucket), since what the cap sizes is the CPU of
// the box and not of one repository; and a pause while the service has an
// invocation in flight. It is called after each read with the bytes read
// (changelogfile.DigestOptions.Pace), on the digest's own goroutine, so its
// own counters need no lock.
type digestPacer struct {
	svc        *service
	repository string
	// read is the bytes paced so far; paused is the time spent waiting for
	// invocations to return.
	read   int64
	paused time.Duration
}

// digestBucket is the one token bucket the process's digests share: the
// bytes it holds, refilled by the time that passed since last at the
// configured rate, up to one read's worth.
type digestBucket struct {
	mu        sync.Mutex
	rate      int64
	allowance float64
	last      time.Time
}

// take draws n bytes from the bucket and answers how long the caller waits
// for them: nothing while the bucket holds them, else the time the rate
// takes to refill the shortfall. Zero rate is no cap.
func (b *digestBucket) take(n int) time.Duration {
	if b.rate <= 0 {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if b.last.IsZero() {
		b.last = now
	}
	b.allowance = min(b.allowance+now.Sub(b.last).Seconds()*float64(b.rate), float64(digestBurstBytes))
	b.last = now
	b.allowance -= float64(n)
	if b.allowance >= 0 {
		return 0
	}
	wait := time.Duration(-b.allowance / float64(b.rate) * float64(time.Second))
	// The wait refills exactly the shortfall; the clock restarts after it.
	b.allowance = 0
	b.last = now.Add(wait)
	return wait
}

func (s *service) newDigestPacer(repository string) *digestPacer {
	return &digestPacer{svc: s, repository: repository}
}

// rate is the cap the pacer draws under, for the start line.
func (p *digestPacer) rate() int64 { return p.svc.digestBucket.rate }

// pace is called after each read of n bytes: it waits out every invocation
// in flight, then the bucket.
func (p *digestPacer) pace(ctx context.Context, n int) error {
	p.read += int64(n)
	if hook := p.svc.testDigestPaceHook; hook != nil {
		hook(p.repository, p.read)
	}
	if err := p.yield(ctx); err != nil {
		return err
	}
	wait := p.svc.digestBucket.take(n)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	return nil
}

// digestBurstBytes is the most the bucket holds: one read, so a digest that
// paused for a long invocation does not hash a backlog at full speed when
// the invocation returns.
const digestBurstBytes = 1 << 20

// yield waits while any function body or agent loop runs in this process,
// and counts the wait.
func (p *digestPacer) yield(ctx context.Context) error {
	if p.svc.invocations.Load() == 0 {
		return nil
	}
	began := time.Now()
	defer func() { p.paused += time.Since(began) }()
	ticker := time.NewTicker(digestYieldPoll)
	defer ticker.Stop()
	for p.svc.invocations.Load() > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}

// invocationStarted and invocationEnded bracket every function body and
// agent loop the process runs (runner.go runCallableRaw, agentloop.go
// runAgent), nested ones included: the count is what the digest yields to.
func (s *service) invocationStarted() { s.invocations.Add(1) }
func (s *service) invocationEnded()   { s.invocations.Add(-1) }
