package engine

// Moving a repository's blob bytes between two stores: the data root and a
// bucket, either way round (`substratectl repository blobs`). The manifests
// do not move: they are records, and a record names its bytes by digest,
// which is the same key in every store. So a migration is a copy of objects,
// checked by hash on both sides, and a prune is a delete of the objects the
// other store is proven to hold.
//
// Each walk is a merge of two listings in digest order (blobbytes.Store.List
// promises the order), so finding what the destination lacks costs one
// listing per thousand objects rather than one request per object.
//
// Migrate and prune open the repository as its writer, the way a snapshot
// does: the database lease and the directory's flock are both held for the
// walk, so a running server refuses them and they refuse a server that starts
// while they run. The operator engine's own boot check has already imported a
// restored directory with no row, so every repository the walk meets has one.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/blobbytes"
)

// blobWalkBatch is how many objects a migration walk lists per call: S3's
// page, so one call is one request.
const blobWalkBatch = 1000

// BlobMigrateOptions are a migration's knobs.
type BlobMigrateOptions struct {
	// DryRun lists and compares, and copies nothing.
	DryRun bool
	// Deep hash-reads every object the destination already holds instead of
	// trusting a matching size. A repair needs it: a destination object of
	// the right length and the wrong bytes is found only by reading it.
	Deep bool
	// Concurrency is how many objects are copied at once; below 1 is 1.
	Concurrency int
}

// BlobMigrateReport is what a migration did to one repository.
type BlobMigrateReport struct {
	Repository string `json:"repository"`
	From       string `json:"from"`
	To         string `json:"to"`
	DryRun     bool   `json:"dryRun,omitempty"`
	// Source is every object the source holds, and SourceBytes their size.
	Source      int   `json:"source"`
	SourceBytes int64 `json:"sourceBytes"`
	// Present were already at the destination: the same size, and the same
	// hash under Deep.
	Present int `json:"present"`
	// Copied were written to the destination and read back by hash;
	// Repaired is the part of Copied that replaced a destination object of
	// the wrong size or hash.
	Copied      int   `json:"copied"`
	CopiedBytes int64 `json:"copiedBytes"`
	Repaired    int   `json:"repaired"`
	// SourceDamaged are source objects that do not hash to their digest.
	// They are never copied: the destination would hold the damage under a
	// good name.
	SourceDamaged []string `json:"sourceDamaged,omitempty"`
	// Failed are objects whose copy failed, each with its error.
	Failed []string      `json:"failed,omitempty"`
	Took   time.Duration `json:"took"`
}

// OK reports a migration that left every source object at the destination.
func (r BlobMigrateReport) OK() bool { return len(r.SourceDamaged) == 0 && len(r.Failed) == 0 }

// BlobStatusReport compares one repository's two stores.
type BlobStatusReport struct {
	Repository string `json:"repository"`
	From       string `json:"from"`
	To         string `json:"to"`
	Deep       bool   `json:"deep,omitempty"`
	// Source and Destination count the objects each store holds.
	Source      int `json:"source"`
	Destination int `json:"destination"`
	// Matching are source objects at the destination with the same size (and
	// the same hash under Deep); Missing are not there; Mismatched are there
	// with another size or hash.
	Matching   int `json:"matching"`
	Missing    int `json:"missing"`
	Mismatched int `json:"mismatched"`
	// Manifests counts the live `stored` blob manifests, and
	// ManifestsMissing those whose bytes the destination does not hold: the
	// blobs a server reading the destination would fail to serve.
	Manifests        int `json:"manifests"`
	ManifestsMissing int `json:"manifestsMissing"`
}

// Complete reports a destination a server could switch to: every source
// object and every stored manifest's bytes are there and match.
func (r BlobStatusReport) Complete() bool {
	return r.Missing == 0 && r.Mismatched == 0 && r.ManifestsMissing == 0
}

// BlobPruneOptions are a prune's knobs.
type BlobPruneOptions struct {
	// DryRun checks every object and deletes nothing.
	DryRun bool
}

// BlobPruneReport is what a prune did to one repository's source store.
type BlobPruneReport struct {
	Repository string `json:"repository"`
	From       string `json:"from"`
	Kept       string `json:"kept"`
	DryRun     bool   `json:"dryRun,omitempty"`
	// Deleted were removed from the source after the kept store's copy was
	// read and hashed; DeletedBytes their size.
	Deleted      int   `json:"deleted"`
	DeletedBytes int64 `json:"deletedBytes"`
	// Absent are source objects the kept store does not hold. They stay: a
	// prune never deletes the only copy.
	Absent []string `json:"absent,omitempty"`
	// Mismatched is the kept store's object that did not hash to its digest,
	// at which the prune stopped deleting from this repository.
	Mismatched string        `json:"mismatched,omitempty"`
	Took       time.Duration `json:"took"`
}

// OK reports a prune that left the source empty of everything it held.
func (r BlobPruneReport) OK() bool { return len(r.Absent) == 0 && r.Mismatched == "" }

// ErrSameBlobStore refuses a migration or a prune whose two stores are one.
var ErrSameBlobStore = errors.New("substrate/engine: the source and the destination are the same blob store")

// lister walks one store's listing in digest order, a batch at a time.
type lister struct {
	ctx   context.Context
	store blobbytes.Store
	buf   []blobbytes.Object
	after string
	done  bool
}

// peek returns the next object without consuming it, or false at the end.
func (l *lister) peek() (blobbytes.Object, bool, error) {
	if len(l.buf) == 0 && !l.done {
		page, err := l.store.List(l.ctx, l.after, blobWalkBatch)
		if err != nil {
			return blobbytes.Object{}, false, err
		}
		if len(page) < blobWalkBatch {
			l.done = true
		}
		if len(page) > 0 {
			l.after = page[len(page)-1].Digest
		}
		l.buf = page
	}
	if len(l.buf) == 0 {
		return blobbytes.Object{}, false, nil
	}
	return l.buf[0], true, nil
}

func (l *lister) pop() { l.buf = l.buf[1:] }

// seek consumes every object that sorts before digest and returns the one at
// digest, if the store holds it.
func (l *lister) seek(digest string) (blobbytes.Object, bool, error) {
	for {
		o, ok, err := l.peek()
		if err != nil || !ok {
			return blobbytes.Object{}, false, err
		}
		switch {
		case o.Digest < digest:
			l.pop()
		case o.Digest == digest:
			l.pop()
			return o, true, nil
		default:
			return blobbytes.Object{}, false, nil
		}
	}
}

// bindStores binds both backends to one repository, refusing two that are
// the same store.
func bindStores(repository string, from, to blobbytes.Backend) (blobbytes.Store, blobbytes.Store, error) {
	if storeName(from) == storeName(to) {
		return nil, nil, fmt.Errorf("%w: %s", ErrSameBlobStore, storeName(from))
	}
	src, err := from.Repository(repository)
	if err != nil {
		return nil, nil, err
	}
	dst, err := to.Repository(repository)
	if err != nil {
		return nil, nil, err
	}
	return src, dst, nil
}

// storeName is how a report names a backend: its String, which says where it
// is and never carries a credential.
func storeName(b blobbytes.Backend) string {
	if s, ok := b.(fmt.Stringer); ok {
		return s.String()
	}
	return fmt.Sprintf("%T", b)
}

// openWriter opens a repository as its writer, for a walk that must not run
// beside a server: the lease and the flock are held until the engine closes.
func (s *service) openWriter(ctx context.Context, repository string) error {
	if s.readOnly {
		return ErrDirectoryReadOnly
	}
	repo, err := s.repositoryByID(ctx, repository)
	if err != nil {
		return err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return err
	}
	return ds.directoryErr()
}

// MigrateBlobs copies one repository's blob bytes from one store to another.
// Every source object is read and hashed before it is written, and read and
// hashed again at the destination after; an object the destination already
// holds at the same size (and hash, under Deep) is left alone, so a second
// run picks up where an interrupted one stopped. Nothing is ever deleted:
// PruneBlobs is the separate step that removes the source's copies.
func (s *service) MigrateBlobs(ctx context.Context, repository string, from, to blobbytes.Backend, opts BlobMigrateOptions) (BlobMigrateReport, error) {
	started := time.Now()
	report := BlobMigrateReport{Repository: repository, From: storeName(from), To: storeName(to), DryRun: opts.DryRun}
	src, dst, err := bindStores(repository, from, to)
	if err != nil {
		return report, err
	}
	if err := s.openWriter(ctx, repository); err != nil {
		return report, err
	}
	workers := max(opts.Concurrency, 1)
	type job struct {
		obj    blobbytes.Object
		repair bool
	}
	jobs := make(chan job)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for range workers {
		wg.Go(func() {
			for j := range jobs {
				n, err := copyBlob(ctx, src, dst, j.obj)
				mu.Lock()
				switch {
				case errors.Is(err, errSourceDamaged):
					report.SourceDamaged = append(report.SourceDamaged, j.obj.Digest)
				case err != nil:
					report.Failed = append(report.Failed, fmt.Sprintf("%s: %v", j.obj.Digest, err))
				default:
					report.Copied++
					report.CopiedBytes += n
					if j.repair {
						report.Repaired++
					}
				}
				mu.Unlock()
			}
		})
	}
	p := s.progress("substrate: blobs migrate: copying", "repository", repository)
	walkErr := func() error {
		defer close(jobs)
		srcList := &lister{ctx: ctx, store: src}
		dstList := &lister{ctx: ctx, store: dst}
		for {
			o, ok, err := srcList.peek()
			if err != nil {
				return fmt.Errorf("substrate/engine: list the source: %w", err)
			}
			if !ok {
				return nil
			}
			srcList.pop()
			mu.Lock()
			report.Source++
			report.SourceBytes += o.Size
			copied := report.Copied
			mu.Unlock()
			p.report("objects", report.Source, "copied", copied)
			d, held, err := dstList.seek(o.Digest)
			if err != nil {
				return fmt.Errorf("substrate/engine: list the destination: %w", err)
			}
			present := held && d.Size == o.Size
			if present && opts.Deep {
				_, err := blobbytes.ReadAll(ctx, dst, o.Digest, o.Size)
				switch {
				case errors.Is(err, blobbytes.ErrDigestMismatch), errors.Is(err, blobbytes.ErrNotStored):
					present = false
				case err != nil:
					return fmt.Errorf("substrate/engine: read the destination's %s: %w", o.Digest, err)
				}
			}
			if present {
				mu.Lock()
				report.Present++
				mu.Unlock()
				continue
			}
			if opts.DryRun {
				mu.Lock()
				report.Copied++
				report.CopiedBytes += o.Size
				if held {
					report.Repaired++
				}
				mu.Unlock()
				continue
			}
			select {
			case jobs <- job{obj: o, repair: held}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}()
	wg.Wait()
	slices.Sort(report.SourceDamaged)
	slices.Sort(report.Failed)
	report.Took = time.Since(started)
	return report, walkErr
}

// errSourceDamaged marks a source object that does not hash to its digest.
var errSourceDamaged = errors.New("the source's bytes do not hash to the digest")

// copyBlob copies one object: read whole and hashed at the source (a blob is
// at most the upload cap), written, and read back and hashed at the
// destination. It returns the bytes written.
func copyBlob(ctx context.Context, src, dst blobbytes.Store, o blobbytes.Object) (int64, error) {
	data, err := blobbytes.ReadAll(ctx, src, o.Digest, o.Size)
	if errors.Is(err, blobbytes.ErrDigestMismatch) {
		return 0, fmt.Errorf("%w: %w", errSourceDamaged, err)
	}
	if err != nil {
		return 0, fmt.Errorf("read the source: %w", err)
	}
	if err := dst.Put(ctx, o.Digest, int64(len(data)), bytes.NewReader(data)); err != nil {
		return 0, fmt.Errorf("write the destination: %w", err)
	}
	if _, err := blobbytes.ReadAll(ctx, dst, o.Digest, int64(len(data))); err != nil {
		return 0, fmt.Errorf("read the destination back: %w", err)
	}
	return int64(len(data)), nil
}

// BlobStatus compares one repository's two stores without changing either,
// and checks every live stored manifest's bytes against the destination. It
// takes no lock, so it runs beside a server; what it reports is then a moment
// of a store that may be moving.
func (s *service) BlobStatus(ctx context.Context, repository string, from, to blobbytes.Backend, deep bool) (BlobStatusReport, error) {
	report := BlobStatusReport{Repository: repository, From: storeName(from), To: storeName(to), Deep: deep}
	src, dst, err := bindStores(repository, from, to)
	if err != nil {
		return report, err
	}
	repo, err := s.repositoryByID(ctx, repository)
	if err != nil {
		return report, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return report, err
	}
	manifests, err := storedBlobs(ctx, ds.db)
	if err != nil {
		return report, err
	}
	report.Manifests = len(manifests)

	p := s.progress("substrate: blobs status: comparing", "repository", repository)
	srcList := &lister{ctx: ctx, store: src}
	dstList := &lister{ctx: ctx, store: dst}
	for {
		o, ok, err := srcList.peek()
		if err != nil {
			return report, fmt.Errorf("substrate/engine: list the source: %w", err)
		}
		if !ok {
			break
		}
		srcList.pop()
		report.Source++
		p.report("objects", report.Source)
		d, held, err := dstList.seek(o.Digest)
		if err != nil {
			return report, fmt.Errorf("substrate/engine: list the destination: %w", err)
		}
		switch {
		case !held:
			report.Missing++
		case d.Size != o.Size:
			report.Mismatched++
		case deep:
			_, err := blobbytes.ReadAll(ctx, dst, o.Digest, o.Size)
			switch {
			case err == nil:
				report.Matching++
			case errors.Is(err, blobbytes.ErrDigestMismatch), errors.Is(err, blobbytes.ErrNotStored):
				report.Mismatched++
			default:
				return report, fmt.Errorf("substrate/engine: read the destination's %s: %w", o.Digest, err)
			}
		default:
			report.Matching++
		}
	}

	// The destination on its own, against the manifests: the listing in
	// digest order and the manifests sorted the same way in Go, because the
	// database's collation need not order them bytewise.
	slices.SortFunc(manifests, func(a, b storedBlob) int {
		switch {
		case a.digest < b.digest:
			return -1
		case a.digest > b.digest:
			return 1
		}
		return 0
	})
	full := &lister{ctx: ctx, store: dst}
	for {
		_, ok, err := full.peek()
		if err != nil {
			return report, fmt.Errorf("substrate/engine: list the destination: %w", err)
		}
		if !ok {
			break
		}
		full.pop()
		report.Destination++
	}
	manifestList := &lister{ctx: ctx, store: dst}
	for _, m := range manifests {
		o, held, err := manifestList.seek(m.digest)
		if err != nil {
			return report, fmt.Errorf("substrate/engine: list the destination: %w", err)
		}
		if !held || (m.size >= 0 && o.Size != m.size) {
			report.ManifestsMissing++
		}
	}
	return report, nil
}

// PruneBlobs deletes one repository's blob bytes from the store it is
// leaving, each object only once the kept store's copy has been read and
// hashed against the digest. An object the kept store does not hold is left
// and reported; one whose kept copy does not hash stops the prune for this
// repository, since a store that holds damage under one name may hold it
// under others. Nothing the kept store's manifests or count say is an input
// to a delete: only the read of the object itself is.
func (s *service) PruneBlobs(ctx context.Context, repository string, from, kept blobbytes.Backend, opts BlobPruneOptions) (BlobPruneReport, error) {
	started := time.Now()
	report := BlobPruneReport{Repository: repository, From: storeName(from), Kept: storeName(kept), DryRun: opts.DryRun}
	src, dst, err := bindStores(repository, from, kept)
	if err != nil {
		return report, err
	}
	if err := s.openWriter(ctx, repository); err != nil {
		return report, err
	}
	p := s.progress("substrate: blobs prune: deleting", "repository", repository)
	srcList := &lister{ctx: ctx, store: src}
	for {
		o, ok, err := srcList.peek()
		if err != nil {
			return report, fmt.Errorf("substrate/engine: list the source: %w", err)
		}
		if !ok {
			break
		}
		srcList.pop()
		p.report("deleted", report.Deleted)
		_, err = blobbytes.ReadAll(ctx, dst, o.Digest, o.Size)
		switch {
		case errors.Is(err, blobbytes.ErrNotStored):
			report.Absent = append(report.Absent, o.Digest)
			continue
		case errors.Is(err, blobbytes.ErrDigestMismatch):
			report.Mismatched = o.Digest
			report.Took = time.Since(started)
			return report, nil
		case err != nil:
			return report, fmt.Errorf("substrate/engine: read the kept store's %s: %w", o.Digest, err)
		}
		if !opts.DryRun {
			if err := src.Delete(ctx, o.Digest); err != nil {
				return report, fmt.Errorf("substrate/engine: delete %s from the source: %w", o.Digest, err)
			}
		}
		report.Deleted++
		report.DeletedBytes += o.Size
	}
	report.Took = time.Since(started)
	return report, nil
}
