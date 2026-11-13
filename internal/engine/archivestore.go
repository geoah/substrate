package engine

import (
	"context"
	"errors"
	"io"

	"github.com/geoah/substrate/internal/archive"
)

// errArchiveStore marks a failure of the archive's store itself: any answer
// to a call but "no object under this key", from a store that refuses the
// connection, times out or answers a service error. It is everyone's, never
// one repository's damage, so the start never sets a repository aside for it
// as damaged (setaside.go through), and it is no failure of the server's own
// either: the plan's archive keeps serving while its copy falls behind.
var errArchiveStore = errors.New("substrate/engine: the archive's store did not answer")

// storeDidNotAnswer reports whether err is the archive's store failing.
func storeDidNotAnswer(err error) bool { return errors.Is(err, errArchiveStore) }

// storeError is a store's failure, marked errArchiveStore and otherwise as
// the store said it.
type storeError struct{ err error }

func (e storeError) Error() string        { return e.err.Error() }
func (e storeError) Unwrap() error        { return e.err }
func (e storeError) Is(target error) bool { return target == errArchiveStore }

func markStoreError(err error) error {
	if err == nil || errors.Is(err, archive.ErrNotFound) || errors.Is(err, io.EOF) {
		return err
	}
	return storeError{err}
}

// markStoreFailures wraps a store so that every failure of the store itself
// carries errArchiveStore, wherever in the engine it surfaces. A store that
// can link a local file keeps that.
func markStoreFailures(s archive.Store) archive.Store {
	m := markedStore{inner: s}
	if l, ok := s.(archive.Linker); ok {
		return markedLinker{markedStore: m, linker: l}
	}
	return m
}

type markedStore struct{ inner archive.Store }

type markedLinker struct {
	markedStore
	linker archive.Linker
}

func (m markedLinker) Link(ctx context.Context, key, path string) error {
	return markStoreError(m.linker.Link(ctx, key, path))
}

func (m markedStore) String() string { return m.inner.String() }

func (m markedStore) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	return markStoreError(m.inner.Put(ctx, key, body, size))
}

func (m markedStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := m.inner.Get(ctx, key)
	if err != nil {
		return nil, markStoreError(err)
	}
	return markedBody{rc}, nil
}

func (m markedStore) List(ctx context.Context, prefix string) ([]archive.Object, error) {
	objects, err := m.inner.List(ctx, prefix)
	return objects, markStoreError(err)
}

func (m markedStore) Delete(ctx context.Context, key string) error {
	return markStoreError(m.inner.Delete(ctx, key))
}

// markedBody is an object's body whose read fails as the store does: a
// connection that drops mid-object is the store's failure too.
type markedBody struct{ io.ReadCloser }

func (b markedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	return n, markStoreError(err)
}
