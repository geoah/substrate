package vocabulary

// A KIND DECLARES WHO NAMES ITS RECORDS (decision record 0228). `ids:` is an
// object so the kind can say more about its ids later (a function that
// generates them) without changing the key's shape; today it carries `type`
// alone:
//
//	ids:
//	  type: named
//
// `minted` is a record id the substrate mints, a UUIDv7 in 26 Crockford
// characters, and a person, an app or an agent may create a record of the
// kind under no other id (a writer may compute one in the same shape).
// `named` is an id the writer chooses, because the id is a name the substrate
// and other records resolve by: a metric's `health/body_mass`, a unit's
// spelling, a provider row's `openai`. Absent, an `internal` kind reads as
// `named` and every other kind as `minted`; the default is the reader's and
// never written into the row.
//
// The key is reserved by name, not tolerated by prefix (record 0020): it is
// in typeDataKeys, so a binary that does not know it quarantines the package
// rather than store it inert.

// The two id types a kind may declare.
const (
	IDsMinted = "minted"
	IDsNamed  = "named"
)

// idsKeys is the `ids` block's closed key set.
var idsKeys = map[string]bool{"type": true}

// IDType is who names a new record of the kind: the declared `ids.type`, or
// the default for its purpose.
func (k *Kind) IDType() string {
	if k.IDs != "" {
		return k.IDs
	}
	if k.Purpose == PurposeInternal {
		return IDsNamed
	}
	return IDsMinted
}

// parseKindIDs reads a kind's `ids:` block.
func (l *loader) parseKindIDs(where string, d map[string]any, t *Kind) {
	raw, present := d["ids"]
	if !present {
		return
	}
	block, ok := raw.(map[string]any)
	if !ok {
		l.errf("%s: data.ids: must be a mapping with `type: %s` or `type: %s` — or drop the key and the kind reads as %s (%s when it is internal)",
			where, IDsMinted, IDsNamed, IDsMinted, IDsNamed)
		return
	}
	for k := range block {
		if !idsKeys[k] {
			l.errf("%s: data.ids.%s: not a key of ids — it takes `type` alone", where, k)
			return
		}
		l.noteKey(idsKeys, k)
	}
	v, ok := block["type"].(string)
	switch {
	case !ok:
		l.errf("%s: data.ids.type: must be %q or %q", where, IDsMinted, IDsNamed)
	case v == IDsMinted || v == IDsNamed:
		t.IDs = v
	default:
		l.errf("%s: data.ids.type: %q is not an id type — %q or %q", where, v, IDsMinted, IDsNamed)
	}
}
