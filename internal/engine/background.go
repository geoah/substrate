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

// spawn schedules a repository-scoped detached task (see service.spawn).
func (ds *dataset) spawn(task string, fn func(context.Context)) bool {
	return ds.svc.spawn(task, ds.Repository().ID, fn)
}
