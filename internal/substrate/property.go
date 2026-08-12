package substrate

import "time"

// PropertyMeta is one property's provenance: who manages it, at which tier,
// since when, and what every other live source says it should be. The CLI
// and console render it as `status.properties`. Tier says whether the hold
// is real: `machine` means recompute may replace the value; `owner` and
// `bundle` mean it yields — a bundle pin is a function's write,
// visible here instead of a silent recompute freeze.
type PropertyMeta struct {
	Manager      string                `json:"manager,omitempty"`
	Tier         Tier                  `json:"tier,omitempty"`
	UpdatedAt    time.Time             `json:"updatedAt,omitzero"`
	Alternatives []PropertyAlternative `json:"alternatives,omitempty"`
}

// PropertyAlternative is one live mapping-source offer that disagrees with
// the stored value — "Google says X" beside a held property. Adopting one is
// just writing it.
type PropertyAlternative struct {
	Actor     string    `json:"actor"`
	Value     any       `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}
