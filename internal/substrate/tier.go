package substrate

// Tier is a writing context's standing, owner > bundle > machine, which
// decides what the owner's gates let its writes touch: a change request, a
// policy, the version check. A property's `writer:` names types of actor,
// never a tier (decision 0290). It is an EXPLICIT ATTRIBUTE of the writing context — the bundle
// tier function and agent dispatch stamps on its own writes, always machine
// for recompute's — never an inference from the actor's spelling. Whether
// sync may replace a value is not the tier's: it is the value's mark, pinned
// or not, stored when the value is written (decision 0284).
type Tier string

const (
	// TierOwner is the human hand: the three door actors (api, console,
	// substratectl), and any actor no data places elsewhere — a stranger's client
	// holds like the user, never like the machinery.
	TierOwner Tier = "owner"
	// TierBundle is installed code: the write context function and agent
	// dispatch builds, and any declared actor carrying `tier: bundle`.
	// Its direct writes pin, as the person's do (decision 0284).
	TierBundle Tier = "bundle"
	// TierMachine is the engine and the sync machinery: recompute's writes,
	// ActorSystem, and a package's own hand.
	TierMachine Tier = "machine"
)

// Hot property names: the properties that occupy their own storage column,
// spelled as the DSL and the wire spell them.
const (
	PropTitle  = "title"
	PropAt     = "at"
	PropEndsAt = "endsAt"
	PropDueAt  = "dueAt"
)
