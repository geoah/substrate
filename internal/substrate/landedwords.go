package substrate

// THE WORDS TWO LANDED REPOSITORY MIGRATIONS SPELL. A write once stood at a
// tier, owner, bundle or machine, which gates read for what only the owner
// could do and which decided whether its value was pinned. A person's roles
// took the gates' place (decision 0275) and a value's mark the pin's
// (decision 0284), so nothing reads a tier now. Repository migrations 0005
// and 0006 landed naming two of them, and a landed migration is never edited
// (frozen:check): these stay for those two, and nothing else names them.
type Tier string

const (
	TierOwner   Tier = "owner"
	TierMachine Tier = "machine"
)
