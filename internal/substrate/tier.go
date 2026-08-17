package substrate

// Tier is a property manager's standing against mapping recompute — the
// three explicit ordered tiers of the manager fix
// : owner > bundle >
// machine. Recompute overwrites only machine-held properties; a manager row
// in either higher tier makes it yield. The tier is an EXPLICIT ATTRIBUTE of
// the writing context — a declared actor document's `tier:`,
// the bundle tier function/agent dispatch stamps on its own writes,
// always machine for recompute's own rows — never an inference from the
// actor's spelling, and STORED on the manager row, so the yield is legible
// on every read.
type Tier string

const (
	// TierOwner is the human hand: the three door actors (api, console,
	// substratectl), and any actor no data places elsewhere — a stranger's client
	// holds like the user, never like the machinery.
	TierOwner Tier = "owner"
	// TierBundle is installed code: the write context function and agent
	// dispatch builds, and any declared actor carrying `tier: bundle`.
	// A bundle's direct write pins like an owner edit — visibly,
	// releasable by the same null-patch.
	TierBundle Tier = "bundle"
	// TierMachine is the engine and the sync machinery: recompute's own
	// rows (whatever actor they credit), ActorSystem, and every
	// authority-declared `bundle:<authority>` actor (the actor document's
	// default tier). Machine-held properties are recompute's to overwrite.
	TierMachine Tier = "machine"
)

// Hot property names: the three declared properties that occupy their own
// storage column, spelled as the DSL and the wire spell them.
const (
	PropTitle  = "title"
	PropBody   = "body"
	PropAt     = "at"
	PropEndsAt = "endsAt"
	PropDueAt  = "dueAt"
)
