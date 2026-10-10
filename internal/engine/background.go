package engine

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"
)

// backgroundDrainTimeout bounds how long Close waits for detached tasks once it
// has canceled them. A task that does not watch its context — an agent turn
// inside a provider call that ignores cancellation — must not hold the process
// open past its orchestrator's grace period, so the wait gives up and logs.
const backgroundDrainTimeout = 10 * time.Second

// background supervises the work that outlives the request that scheduled it:
// the judge, a notified thread's resume (the `notifies` marker of ADR 0003,
// which every resolution rides) and the open-time function warm. Each runs an
// agent turn plus writes on this service's pools, so each needs a context that
// shutdown can cancel, a counter shutdown can wait on, and a recover: these
// goroutines carry no request, chi's recoverer never sees them, and an
// unrecovered panic in one kills the process for every request in flight.
type background struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// mu orders Add against closed. Once closed is set nothing is added, which
	// is what makes Wait safe to call at all.
	mu     sync.Mutex
	closed bool
}

// newBackground roots detached tasks at the SERVICE's lifetime, not a caller's:
// Open's context bounds the boot and a request's context ends with its
// response, while a task scheduled from either must run to its own end or to
// Close.
func newBackground() *background {
	ctx, cancel := context.WithCancel(context.Background())
	return &background{ctx: ctx, cancel: cancel}
}

// spawn runs fn in its own goroutine and reports whether it started. It refuses
// once stopBackground has begun, so a shutdown never gains work after it
// decided what it was waiting for; the caller's work is dropped, named in the
// log, and picked up by whatever recovery path owns it (the resolution sweep
// for a resume, the next open for a warm).
//
// repository may be empty for a task that is not repository-scoped.
func (s *service) spawn(task, repository string, fn func(context.Context)) bool {
	s.bg.mu.Lock()
	if s.bg.closed {
		s.bg.mu.Unlock()
		s.log.Warn("substrate: background task refused, the service is shutting down",
			"task", task, "repository", repository)
		return false
	}
	s.bg.wg.Add(1)
	s.bg.mu.Unlock()
	go func() {
		defer s.bg.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("substrate: background task panicked and was contained; its work did not finish",
					"task", task, "repository", repository,
					"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			}
		}()
		fn(s.bg.ctx)
	}()
	return true
}

// stopBackground refuses new tasks, cancels the running ones and waits up to
// timeout for them. It is called before anything closes: a pool closed under a
// live transaction tears the write that transaction was in the middle of.
func (s *service) stopBackground(timeout time.Duration) {
	s.bg.mu.Lock()
	s.bg.closed = true
	s.bg.mu.Unlock()
	s.bg.cancel()

	done := make(chan struct{})
	go func() {
		s.bg.wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		s.log.Error("substrate: background tasks did not return within the shutdown budget; the pools close under them",
			"timeout", timeout)
	}
}

// stopping reports whether stopBackground has begun: every detached task was
// canceled, and waited for once.
func (b *background) stopping() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// spawn schedules a repository-scoped detached task (see service.spawn).
func (ds *dataset) spawn(task string, fn func(context.Context)) bool {
	return ds.svc.spawn(task, ds.Repository().ID, fn)
}

// backgroundPass is the handle of one pass a dataset owns and runs behind
// the open, over its own pool: the search reindex (searchindex.go) and the
// index reprojection (reprojection.go). It names the latest run's cancel and
// done, nil before the first; close and a rebuild cancel the run and wait
// for done, and a rebuild that fails starts the pass again. closed is set
// by the dataset closing, after which nothing starts.
type backgroundPass struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	closed bool
	// rerun is a start that came while a run was in flight. The run calls
	// fn again when it sees it (again), because work written after fn
	// planned its last step is work that run would otherwise return
	// without, a vocabulary apply's request behind the commit most of all.
	rerun bool
}

// start runs fn as a detached task of the dataset, unless the pass is
// closed. Where an earlier run has not returned it starts nothing and has
// that run call fn once more before it returns. fn's context ends when the
// pass is stopped or the service shuts down, which cancels every detached
// task before it closes any dataset.
func (p *backgroundPass) start(ds *dataset, task string, fn func(ctx context.Context)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	if p.done != nil {
		select {
		case <-p.done:
		default:
			p.rerun = true
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	p.cancel, p.done, p.rerun = cancel, done, false
	started := ds.spawn(task, func(bg context.Context) {
		defer cancel()
		stop := context.AfterFunc(bg, cancel)
		defer stop()
		ended := false
		defer func() {
			// fn panicked: done still closes, so a stop does not wait out
			// its budget on a run that is gone.
			if !ended {
				close(done)
			}
		}()
		for {
			fn(ctx)
			if !p.again(ctx, done) {
				ended = true
				return
			}
		}
	})
	if !started {
		cancel()
		close(done)
	}
}

// again reports whether a start came while the run that owns done was in
// flight, and clears it. A stopped run does not go again. When it answers
// no it closes done under the same lock, so a start after it finds the run
// over and begins a new one, rather than setting rerun on a run that has
// already looked.
func (p *backgroundPass) again(ctx context.Context, done chan struct{}) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rerun && ctx.Err() == nil {
		p.rerun = false
		return true
	}
	p.rerun = false
	close(done)
	return false
}

// stop cancels the running pass and waits for it to return, up to the drain
// budget the service's own shutdown gives a detached task. final is the
// dataset closing: nothing starts after it. The service's shutdown already
// waited its one budget for every detached task and said so if any outlived
// it; waiting again there, once per repository it closes, would multiply
// that budget. what names the pass in the line logged otherwise.
func (p *backgroundPass) stop(ds *dataset, final bool, what string) {
	p.mu.Lock()
	if final {
		p.closed = true
	}
	cancel, done := p.cancel, p.done
	p.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if ds.svc.bg.stopping() {
		return
	}
	timer := time.NewTimer(backgroundDrainTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		ds.svc.log.Error("substrate: "+what+" did not stop within the drain budget",
			"repository", logSafeID(ds.scope.Repository), "timeout", backgroundDrainTimeout)
	}
}

// finished is closed when the latest run has returned, finished or stopped;
// a pass that never started answers a closed channel.
func (p *backgroundPass) finished() <-chan struct{} {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	if done != nil {
		return done
	}
	closed := make(chan struct{})
	close(closed)
	return closed
}
