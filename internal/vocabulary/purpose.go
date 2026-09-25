package vocabulary

// A KIND DECLARES ITS PURPOSE (decision record 0104). `purpose:` says why a
// kind exists — a thing a person browses, a detail of another kind, or
// machinery — because the declaration is the one place that knows, and a
// client listing every kind it holds has nothing else to decide by.
//
// The loader's half is the grammar: one of three words or nothing. The value
// is live but advisory: stored with the declaration and read by clients, and
// nothing on the server filters, refuses or orders by it.
//
// The key is reserved by name, not tolerated by prefix (record 0020): it is in
// typeDataKeys, so a binary that did not know it quarantines the package rather
// than store it inert.

// purposes is the closed value set, in the order a refusal names them.
var purposes = []string{PurposePrimary, PurposeSupporting, PurposeInternal}

// parseKindPurpose reads a kind's `purpose:`. The raw value is asserted rather
// than read through mstr, which would turn a number or a list into "" and so
// into the default, admitting a declaration its author did not write.
func (l *loader) parseKindPurpose(where string, d map[string]any, t *Kind) {
	raw, present := d["purpose"]
	if !present {
		return
	}
	v, ok := raw.(string)
	if !ok {
		l.errf("%s: data.purpose: must be one of %q, %q or %q — or drop the key and the kind reads as %q",
			where, PurposePrimary, PurposeSupporting, PurposeInternal, PurposePrimary)
		return
	}
	for _, p := range purposes {
		if v == p {
			t.Purpose = v
			return
		}
	}
	l.errf("%s: data.purpose: %q is not a purpose — %q, %q or %q, or drop the key and the kind reads as %q",
		where, v, PurposePrimary, PurposeSupporting, PurposeInternal, PurposePrimary)
}
