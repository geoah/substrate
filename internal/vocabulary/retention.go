package vocabulary

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// HOW LONG A KIND'S HISTORY IS KEPT (decision record 0282). `retention:` says
// how long the changelog keeps the entries about a kind's records past each
// record's latest state, which is always kept: `all` (every entry), `none`
// (nothing past the latest state) or an ISO 8601 duration (`P30D`), the one
// duration grammar (duration.go). A package header may state it for the kinds
// that state none, and a kind's own key wins; absent on both reads as `all`,
// so a declaration that says nothing keeps what every kind kept before. A
// repository overrides it per kind with a `core/retention` record.
//
// The loader's half is the grammar. The value is stored with the declaration,
// read by clients and by the engine's one lookup (engine/retention.go), and
// acted on by nothing yet: the compaction that drops entries past it is a
// later step, so a key lands inert and validated (decision 0020). It is
// reserved by name in typeDataKeys and packageDataKeys, so a binary that did
// not know it quarantines the package rather than store it inert.

const (
	// RetentionAll keeps every entry about a record.
	RetentionAll = "all"
	// RetentionNone keeps nothing past a record's latest state.
	RetentionNone = "none"
)

// Retention is one parsed `retention` value.
type Retention struct {
	// Value is the value as written: RetentionAll, RetentionNone or an ISO
	// 8601 duration.
	Value string
	// For is how long entries are kept past a record's latest state when
	// Value is a duration, and zero otherwise.
	For time.Duration
}

// KeepsAll reports whether every entry is kept.
func (r Retention) KeepsAll() bool { return r.Value == RetentionAll }

// KeepsNone reports whether nothing past the latest state is kept.
func (r Retention) KeepsNone() bool { return r.Value == RetentionNone }

// Keeps is how long entries are kept past a record's latest state, the one
// measure two retentions compare by: all is the longest, none is nothing, and
// two spellings of one duration keep the same.
func (r Retention) Keeps() time.Duration {
	switch r.Value {
	case RetentionAll:
		return time.Duration(math.MaxInt64)
	case RetentionNone:
		return 0
	}
	return r.For
}

// ParseRetention reads one `retention` value. A duration must be longer than
// nothing: no time at all is `none`, one spelling for one meaning.
func ParseRetention(s string) (Retention, error) {
	switch s {
	case RetentionAll, RetentionNone:
		return Retention{Value: s}, nil
	}
	// A time designator with no time after it: the shared parse admits it
	// beside a date part, and a retention, which nothing stored yet, does not.
	if !strings.HasPrefix(s, "P") || strings.HasSuffix(s, "T") {
		return Retention{}, fmt.Errorf("%q is not a retention: %q, %q or an ISO 8601 duration such as P30D", s, RetentionAll, RetentionNone)
	}
	d, err := ParseISODuration(s)
	if err != nil {
		return Retention{}, fmt.Errorf("%q is not a retention: %w", s, err)
	}
	if d <= 0 {
		return Retention{}, fmt.Errorf("%q is not a retention: keeping no time past the latest state is %q", s, RetentionNone)
	}
	return Retention{Value: s, For: d}, nil
}

// MustRetention is ParseRetention for a value already admitted: a declared
// one the loader held, or an absent one read as RetentionAll.
func MustRetention(s string) Retention {
	if s == "" {
		return Retention{Value: RetentionAll}
	}
	r, err := ParseRetention(s)
	if err != nil {
		return Retention{Value: RetentionAll}
	}
	return r
}

// parseRetentionKey reads a declaration's `retention:` into into. The raw
// value is asserted rather than read through mstr, which would turn a number
// into "" and so into the absent key, admitting a declaration its author did
// not write.
func (l *loader) parseRetentionKey(where string, d map[string]any, into *string) {
	raw, present := d["retention"]
	if !present {
		return
	}
	v, ok := raw.(string)
	if !ok {
		l.errf("%s: data.retention: must be %q, %q or an ISO 8601 duration such as P30D — or drop the key and it reads as %q",
			where, RetentionAll, RetentionNone, RetentionAll)
		return
	}
	if _, err := ParseRetention(v); err != nil {
		l.errf("%s: data.retention: %v — or drop the key and it reads as %q", where, err, RetentionAll)
		return
	}
	*into = v
}

// RetentionOf is the retention a kind's declaration states: its own key, else
// its package header's, else "" (read as RetentionAll). A repository's
// override is the engine's to apply over it.
func (r *Registry) RetentionOf(ref string) string {
	k, ok := r.ByIdentity(ref)
	if !ok {
		return ""
	}
	if k.Retention != "" {
		return k.Retention
	}
	if p, ok := r.PackageByName(k.Package); ok {
		return p.Retention
	}
	return ""
}
