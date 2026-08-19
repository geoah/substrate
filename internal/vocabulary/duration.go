package vocabulary

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The ISO 8601 duration grammar lives with the datatype it belongs to
// (DatatypeDuration): the loader parses a declared `duration` value here, and
// the engine's write coercion reuses the same parse and format, so the wire
// grammar has one home rather than two spellings that can drift.

// reISODuration is ISO 8601's duration, MINUS years and months: weeks, days,
// and a time part, each component optional, fractions only on the time part.
var reISODuration = regexp.MustCompile(
	`^-?P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

// reISOCalendar spots a year or a month in an ISO duration's DATE part (a
// time-part M is minutes and stays legal), so the refusal can say why.
var reISOCalendar = regexp.MustCompile(`^-?P[^T]*\d+[YM]`)

var errDurationGrammar = fmt.Errorf("expected an ISO 8601 duration (PT47M12S, P2DT3H, P1W)")

// ParseISODuration reads a duration. ISO 8601 is the ONE grammar, in and out:
// Go's own syntax ("47m12s") is refused, because two grammars for one word is
// how "duration" stops meaning anything. The parse translates into Go's
// grammar internally and lets time.ParseDuration do the arithmetic (its
// parser sums repeated units, so weeks and days become hour terms). Years and
// months are refused BY NAME: neither has a fixed length, and a duration here
// is exact time: a day is exactly 24h and a week exactly 168h.
func ParseISODuration(s string) (time.Duration, error) {
	m := reISODuration.FindStringSubmatch(s)
	if m == nil {
		if reISOCalendar.MatchString(s) {
			return 0, fmt.Errorf("years and months have no fixed length: spell the duration in weeks, days and a time part (P2DT3H)")
		}
		return 0, errDurationGrammar
	}
	if m[1] == "" && m[2] == "" && m[3] == "" && m[4] == "" && m[5] == "" {
		return 0, errDurationGrammar
	}
	var b strings.Builder
	if strings.HasPrefix(s, "-") {
		b.WriteString("-")
	}
	for _, part := range []struct {
		digits string
		hours  int64
	}{{m[1], 7 * 24}, {m[2], 24}} {
		if part.digits == "" {
			continue
		}
		n, err := strconv.ParseInt(part.digits, 10, 64)
		if err != nil || n > math.MaxInt64/part.hours {
			return 0, fmt.Errorf("the duration overflows what one can hold (about 292 years)")
		}
		fmt.Fprintf(&b, "%dh", n*part.hours)
	}
	for _, part := range []struct{ digits, unit string }{
		{m[3], "h"}, {m[4], "m"}, {m[5], "s"},
	} {
		if part.digits != "" {
			b.WriteString(part.digits + part.unit)
		}
	}
	d, err := time.ParseDuration(b.String())
	if err != nil {
		return 0, fmt.Errorf("the duration overflows what one can hold (about 292 years)")
	}
	return d, nil
}

// FormatISODuration renders the ONE stored spelling of a duration: days (each
// exactly 24h), then a time part, zero components omitted, "PT0S" for zero.
// Deterministic decomposition is what makes it canonical: every value has
// exactly one stored form, however it was authored ("PT36H" stores as
// "P1DT12H", "P2W" as "P14D").
func FormatISODuration(d time.Duration) string {
	var b strings.Builder
	if d < 0 {
		b.WriteString("-")
		d = -d
	}
	b.WriteString("P")
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	if days > 0 {
		fmt.Fprintf(&b, "%dD", days)
	}
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	s := (d % time.Minute) / time.Second
	ns := d % time.Second
	if h > 0 || m > 0 || s > 0 || ns > 0 || days == 0 {
		b.WriteString("T")
		if h > 0 {
			fmt.Fprintf(&b, "%dH", h)
		}
		if m > 0 {
			fmt.Fprintf(&b, "%dM", m)
		}
		if ns > 0 {
			frac := strings.TrimRight(fmt.Sprintf("%09d", ns), "0")
			fmt.Fprintf(&b, "%d.%sS", s, frac)
		} else if s > 0 || (h == 0 && m == 0 && days == 0) {
			fmt.Fprintf(&b, "%dS", s)
		}
	}
	return b.String()
}
