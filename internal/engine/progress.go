package engine

// A walk of one repository's whole changelog (verify's two passes, a rebuild,
// the boot import's row copy and its two fold passes, the snapshot's
// read-back of its copy) says nothing until it ends, and on a history of
// millions of entries that is minutes. So each says where it is at info
// level once per progressEvery: the segment it is in, the seq it reached and
// the bytes read so far (issue 745). Every walk's line is the one message
// below, and the `walk` attribute names the walk, because a log message is a
// constant (sloglint's static-msg).

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

// progressMsg is the message of every progress line.
const progressMsg = "substrate: progress"

// progress throttles one walk's position reports to one per interval.
type progress struct {
	log   *slog.Logger
	walk  string
	attrs []any
	every time.Duration
	last  time.Time
}

// progress starts a reporter for one walk; attrs name the repository and
// whatever else every line of it carries. The first line comes after one
// interval, not at once: a walk that ends inside it needs none.
func (s *service) progress(walk string, attrs ...any) *progress {
	return &progress{log: s.log, walk: walk, attrs: attrs, every: s.progressEvery, last: time.Now()}
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
	attrs := make([]any, 0, len(p.attrs)+len(where)+4)
	attrs = append(attrs, "walk", p.walk)
	attrs = append(attrs, p.attrs...)
	attrs = append(attrs, where...)
	attrs = append(attrs, ProgressKey, true)
	p.log.Info(progressMsg, attrs...)
}
