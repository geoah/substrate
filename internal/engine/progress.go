package engine

// A walk of one repository's whole changelog (verify's two passes, a rebuild,
// the boot import's row copy and its two fold passes, the snapshot's
// read-back of its copy) says nothing until it ends, and on a history of
// millions of entries that is minutes. So each says where it is at info
// level once per progressEvery: the segment it is in, the seq it reached and
// the bytes read so far (issue 745).

import (
	"log/slog"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
)

// progressEvery is how often a long walk reports its position.
const progressEvery = 30 * time.Second

// ProgressKey is the attribute every progress line carries, `progress=true`,
// so a handler that hides the engine's other info lines (substratectl's
// operator hat, whose output is its own report) can still pass these.
const ProgressKey = "progress"

// IsProgress reports whether a log record is one of the engine's progress
// lines.
func IsProgress(r slog.Record) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == ProgressKey && a.Value.Kind() == slog.KindBool && a.Value.Bool() {
			found = true
			return false
		}
		return true
	})
	return found
}

// progress throttles one walk's position reports to one per interval.
type progress struct {
	log   *slog.Logger
	msg   string
	attrs []any
	every time.Duration
	last  time.Time
}

// progress starts a reporter for one walk; attrs name the repository and
// whatever else every line of it carries. The first line comes after one
// interval, not at once: a walk that ends inside it needs none.
func (s *service) progress(msg string, attrs ...any) *progress {
	return &progress{log: s.log, msg: msg, attrs: attrs, every: s.progressEvery, last: time.Now()}
}

// tick reports pos if an interval has passed since the last report.
func (p *progress) tick(pos changelogfile.Position) {
	p.report("segment", pos.Segment, "seq", pos.Seq, "bytes", pos.Bytes)
}

// report logs where the walk is, given as attrs, if an interval has passed
// since the last report.
func (p *progress) report(where ...any) {
	if time.Since(p.last) < p.every {
		return
	}
	p.last = time.Now()
	attrs := make([]any, 0, len(p.attrs)+len(where)+2)
	attrs = append(attrs, p.attrs...)
	attrs = append(attrs, where...)
	attrs = append(attrs, ProgressKey, true)
	p.log.Info(p.msg, attrs...)
}

// The checks of a changelog directory, as their progress lines name them.
// The boot check and the first open after it read each finished segment's
// first and last lines and take the rest on its sidecar's word (repodir.go);
// the server digests those segments after the open (segmentdigest.go); a
// snapshot digests what it copied (snapshot.go).
const (
	checkAtBoot       = "substrate: boot check: checking the changelog segments"
	checkAtOpen       = "substrate: open: checking the changelog segments"
	checkSnapshotCopy = "substrate: snapshot: checking the copied changelog segments"
)

// checkProgress is the Progress of one check of a repository's changelog
// directory (changelogfile.OpenOptions). Digesting every finished segment of
// a long history takes minutes, so it reports once per interval how many of
// the directory's segments and bytes are checked (issue 761). It is also
// where the digest test seam sees each finished segment whose bytes were
// hashed.
func (s *service) checkProgress(msg, repository string) func(changelogfile.OpenProgress) {
	p := s.progress(msg, "repository", repository)
	return func(op changelogfile.OpenProgress) {
		if op.Digested && s.testDigestHook != nil {
			s.testDigestHook(repository, op.Segment)
		}
		p.report("segment", op.Segment,
			"segments", op.Segments, "totalSegments", op.TotalSegments,
			"bytes", op.Bytes, "totalBytes", op.TotalBytes)
	}
}
