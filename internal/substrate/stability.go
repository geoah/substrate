package substrate

// The stability labels (docs/stability.md, decision 0256). A label is a
// promise about CHANGE, not about quality: everything that carries one is
// served and works today. It sits in two places and nowhere else: on a
// package, for its kinds (the header's `stability` key), and on a discovery
// feature, for its routes (GET /.well-known/substrate/server.json).
//
//   - stable: only additions within one major version; removed only in the
//     next.
//   - beta: supported. A break is announced one minor release ahead, in the
//     discovery file and in the changelog.
//   - experimental: may change or go in any release.
//
// A label never covers stored data: the latest state of every record, an
// experimental package's included, survives every upgrade.
const (
	StabilityStable       = "stable"
	StabilityBeta         = "beta"
	StabilityExperimental = "experimental"
)

// Stabilities is the closed label set, strongest promise first.
var Stabilities = []string{StabilityStable, StabilityBeta, StabilityExperimental}
