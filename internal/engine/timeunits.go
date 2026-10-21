package engine

// THE DURATION UNITS (decision 0186). The aggregate read's durationIn and
// timeInState, and a `duration` property it reduces, answer times computed
// from instants, never a stored unit, so the units they answer in are this
// fixed list rather than any repository's unit records: every repository
// reads them alike, and none can redefine an hour.

import (
	"math/big"
	"strings"

	"github.com/geoah/substrate/internal/vocabulary"
)

// timeUnits are the duration units, each in seconds.
var timeUnits = map[string]*big.Rat{
	"ms":  big.NewRat(1, 1000),
	"s":   big.NewRat(1, 1),
	"min": big.NewRat(60, 1),
	"h":   big.NewRat(3600, 1),
	"d":   big.NewRat(86400, 1),
	"wk":  big.NewRat(604800, 1),
}

// timeUnitNames is the list as a refusal names it.
const timeUnitNames = "ms, s, min, h, d or wk"

// isTimeUnit reports whether unit is one of the duration units.
func isTimeUnit(unit string) bool {
	_, ok := timeUnits[strings.TrimSpace(unit)]
	return ok
}

// secondsIn converts a time in seconds into a duration unit exactly; false
// for a unit not on the list.
func secondsIn(seconds *big.Rat, unit string) (*big.Rat, bool) {
	per, ok := timeUnits[strings.TrimSpace(unit)]
	if !ok {
		return nil, false
	}
	return new(big.Rat).Quo(seconds, per), true
}

// durationIn writes milliseconds in a duration unit of the fixed list,
// exactly where it can; a unit outside the list answers seconds.
func durationIn(ms int64, unit string) (string, bool) {
	s := big.NewRat(ms, 1000)
	v, ok := secondsIn(s, unit)
	if !ok {
		v = s
	}
	return vocabulary.RatDecimal(v, vocabulary.ConvertedDigits)
}
