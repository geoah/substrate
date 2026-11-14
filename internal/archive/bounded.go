package archive

import (
	"context"
	"io"
	"time"
)

// DefaultCallTimeout bounds a list, a delete, and the wait for a get's
// response; DefaultIdleTimeout bounds how long a put's or a get's body may go
// without a byte moving. A store that accepts a connection and never answers
// fails a call within them, rather than holding the boot, a pass or a
// deletion forever.
const (
	DefaultCallTimeout = 2 * time.Minute
	DefaultIdleTimeout = time.Minute
)

// Bounded wraps a store so that no call to it waits on the store forever.
// A list or a delete fails after call; a get fails if its response takes
// longer than call, and its body read fails once idle passes with no byte
// arriving; a put fails once idle passes with the store reading no byte of
// its body or, the body sent, answering nothing. A transfer that keeps
// moving is never cut, however long it runs. A store that can link a local
// file keeps that.
func Bounded(s Store, call, idle time.Duration) Store {
	b := bounded{inner: s, call: call, idle: idle}
	if l, ok := s.(Linker); ok {
		return boundedLinker{bounded: b, linker: l}
	}
	return b
}

type bounded struct {
	inner      Store
	call, idle time.Duration
}

type boundedLinker struct {
	bounded
	linker Linker
}

func (b boundedLinker) Link(ctx context.Context, key, path string) error {
	return b.linker.Link(ctx, key, path)
}

func (b bounded) String() string { return b.inner.String() }

func (b bounded) List(ctx context.Context, prefix string) ([]Object, error) {
	ctx, cancel := context.WithTimeout(ctx, b.call)
	defer cancel()
	return b.inner.List(ctx, prefix)
}

func (b bounded) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, b.call)
	defer cancel()
	return b.inner.Delete(ctx, key)
}

func (b bounded) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	timer := time.AfterFunc(b.idle, cancel)
	defer timer.Stop()
	return b.inner.Put(ctx, key, &idleReadSeeker{ReadSeeker: body, timer: timer, idle: b.idle}, size)
}

func (b bounded) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(b.call, cancel)
	rc, err := b.inner.Get(ctx, key)
	if err != nil {
		timer.Stop()
		cancel()
		return nil, err
	}
	timer.Reset(b.idle)
	return &idleReadCloser{ReadCloser: rc, timer: timer, idle: b.idle, cancel: cancel}, nil
}

// idleReadSeeker restarts its timer at every read, so the timer fires only
// when the store stops taking the body.
type idleReadSeeker struct {
	io.ReadSeeker
	timer *time.Timer
	idle  time.Duration
}

func (r *idleReadSeeker) Read(p []byte) (int, error) {
	r.timer.Reset(r.idle)
	return r.ReadSeeker.Read(p)
}

// idleReadCloser restarts its timer at every read, and ends the call when
// closed.
type idleReadCloser struct {
	io.ReadCloser
	timer  *time.Timer
	idle   time.Duration
	cancel context.CancelFunc
}

func (r *idleReadCloser) Read(p []byte) (int, error) {
	r.timer.Reset(r.idle)
	return r.ReadCloser.Read(p)
}

func (r *idleReadCloser) Close() error {
	r.timer.Stop()
	r.cancel()
	return r.ReadCloser.Close()
}
