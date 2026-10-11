package providertest

import "github.com/geoah/substrate/samples"

// The observations and health sample kinds the health adapter suites read
// back, under the shipped samples authority. Shared by every adapter suite in
// this package, so a suite's own constants never redeclare them.
const (
	obsObservation = samples.Authority + "/observations/observation"
	obsTrace       = samples.Authority + "/observations/trace"
	obsSource      = samples.Authority + "/observations/source"
	healthSleep    = samples.Authority + "/health/sleep"
	healthWorkout  = samples.Authority + "/health/workout"
)
