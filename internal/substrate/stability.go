package substrate

// The stability values a discovery feature carries
// (GET /.well-known/substrate/server.json). They say what a client may build
// on, so each one is a promise about CHANGE, not about quality: every surface
// listed is served and works today.
//
//   - alpha: the shape may change or be withdrawn with no v1 wire break. Pin
//     the server version if you depend on it.
//   - beta: served and supported, and the shape is still moving before v1
//     freezes it. A break is announced, never silent.
//   - stable: frozen for v1. Changes are additive only.
//
// A surface with an open ticket that restructures or deletes it is beta at
// best, whatever its age: the stamp reports the freeze, never the mileage.
const (
	StabilityAlpha  = "alpha"
	StabilityBeta   = "beta"
	StabilityStable = "stable"
)
