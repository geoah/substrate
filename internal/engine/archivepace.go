package engine

// THE ARCHIVE YIELDS TO REAL WORK. A pass encodes and checksums every line
// it copies, and the first pass over a long history copies all of them: one
// core busy for as long as the bytes last. The background digest of the
// segment files, which this replaces, met the same cost: on a four-core arm64
// box a 27 GB history took 2.3 cores for ten minutes, a function that takes
// 5 s alone took 45 s beside it and met its deadline, and a scheduled sync
// parked (#872). So the passes are paced two ways. They copy no more than a
// configured number of bytes per second over every repository the process
// writes, 8 MiB by default (SUBSTRATE_ARCHIVE_BYTES_PER_SECOND); and they
// pause, within one line, while any function body or agent loop runs in this
// process, and go on when the last of them returns, for at most
// archiveYieldMax at a time, so a server whose bodies run back to back still
// archives.

import (
	"context"
	"sync"
	"time"
)

// DefaultArchiveBytesPerSecond is the rate the archive passes copy at when
// no option names one: 8 MiB per second over every repository the process
// writes, under half a core on a four-core arm64 box without SHA-256
// instructions, where a 27 GB history takes about an hour.
const DefaultArchiveBytesPerSecond int64 = 8 << 20

// archiveYieldPoll is how often a paused pass asks whether every invocation
// has returned.
const archiveYieldPoll = 50 * time.Millisecond

// archiveYieldMax is the longest a pass waits for invocations to return
// before it copies one more line. An invocation shorter than it meets no more
// of the pass than before the bound, and a longer one meets a line's worth of
// encoding per bound.
const archiveYieldMax = 30 * time.Second

// archivePacer holds one pass to its two bounds: the bytes the PROCESS may
// copy per second, a token bucket every repository's passes draw from
// (service.archiveBucket), since what the cap sizes is the CPU of the box and
// not of one repository; and a pause while the service has an invocation in
// flight. It is called after each line or blob with its bytes, on the pass's
// own goroutine, so its own counters need no lock.
type archivePacer struct {
	svc        *service
	repository string
	// read is the bytes paced so far; paused is the time spent waiting for
	// invocations to return.
	read   int64
	paused time.Duration
}

// archiveBucket is the one token bucket the process's passes share: the
// bytes it holds, refilled by the time that passed since last at the
// configured rate, up to one read's worth.
type archiveBucket struct {
	mu        sync.Mutex
	rate      int64
	allowance float64
	last      time.Time
}

// take draws n bytes from the bucket and answers how long the caller waits
// for them: nothing while the bucket holds them, else the time the rate
// takes to refill the shortfall. Zero rate is no cap.
func (b *archiveBucket) take(n int) time.Duration {
	if b.rate <= 0 {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if b.last.IsZero() {
		b.last = now
	}
	b.allowance = min(b.allowance+now.Sub(b.last).Seconds()*float64(b.rate), float64(archiveBurstBytes))
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

func (s *service) newArchivePacer(repository string) *archivePacer {
	return &archivePacer{svc: s, repository: repository}
}

// pace is called after each n bytes copied: it waits out every invocation in
// flight, then the bucket.
func (p *archivePacer) pace(ctx context.Context, n int) error {
	p.read += int64(n)
	if hook := p.svc.testArchivePaceHook; hook != nil {
		hook(p.repository, p.read)
	}
	if err := p.yield(ctx); err != nil {
		return err
	}
	wait := p.svc.archiveBucket.take(n)
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

// archiveBurstBytes is the most the bucket holds, so a pass that paused for a
// long invocation does not copy a backlog at full speed when the invocation
// returns.
const archiveBurstBytes = 1 << 20

// yield waits while any function body or agent loop runs in this process,
// for at most the service's archiveYieldMax, and counts the wait.
func (p *archivePacer) yield(ctx context.Context) error {
	if p.svc.invocations.Load() == 0 {
		return nil
	}
	began := time.Now()
	defer func() { p.paused += time.Since(began) }()
	ticker := time.NewTicker(archiveYieldPoll)
	defer ticker.Stop()
	bound := time.NewTimer(p.svc.archiveYieldMax)
	defer bound.Stop()
	for p.svc.invocations.Load() > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-bound.C:
			return nil
		case <-ticker.C:
		}
	}
	return nil
}

// invocationStarted and invocationEnded bracket every function body and
// agent loop the process runs (runner.go runCallableRaw, agentloop.go
// runAgent), nested ones included: the count is what a pass yields to.
func (s *service) invocationStarted() { s.invocations.Add(1) }
func (s *service) invocationEnded()   { s.invocations.Add(-1) }
