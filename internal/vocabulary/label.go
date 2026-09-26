package vocabulary

import "strings"

// A KIND MAY DECLARE ITS DISPLAY LABEL (decision record 0106). The kind's name
// is an identifier (`conversation`, `contactgroup`), and some names read wrong
// to a person however a client splits and pluralizes them: Slack calls its
// conversations channels. `label:` carries the words a client shows instead,
// both forms, because a client that had to pluralize the singular would need
// its own word list again, and "Sync progress" has no regular plural.
//
// The label is display text and nothing else: no route, filter, reference or
// grant reads it, and the kind's name stays its one identifier (decision 0033).

// labelKeys is the `label` block's closed key set. Both keys are required when
// the block is declared.
var labelKeys = map[string]bool{"singular": true, "plural": true}

// KindLabel is a kind's declared display label: what a client calls one record
// of the kind and what it calls the collection. The zero value means the kind
// declared none, and a client humanizes the kind's name itself.
type KindLabel struct {
	Singular string
	Plural   string
}

// Empty reports whether the kind declared no label.
func (l KindLabel) Empty() bool { return l.Singular == "" && l.Plural == "" }

// parseKindLabel reads a kind's OPTIONAL `label:` block. A present block
// carries both forms, each a short single-line caption held to the same bound
// as a property's displayName.
func (l *loader) parseKindLabel(where string, d map[string]any) KindLabel {
	raw, present := d["label"]
	if !present {
		return KindLabel{}
	}
	m := asMapOrNil(raw)
	if m == nil {
		l.errf("%s: data.label: must be a mapping with `singular` and `plural`, the words a client shows for one record and for the collection", where)
		return KindLabel{}
	}
	l.checkKeys(where+": data.label", m, labelKeys)
	out := KindLabel{
		Singular: l.labelForm(where, m, "singular"),
		Plural:   l.labelForm(where, m, "plural"),
	}
	if out.Singular == "" || out.Plural == "" {
		return KindLabel{}
	}
	return out
}

func (l *loader) labelForm(where string, m map[string]any, key string) string {
	raw, present := m[key]
	v, ok := raw.(string)
	s := strings.TrimSpace(v)
	switch {
	case present && !ok:
		l.errf("%s: data.label.%s: must be a string, got %T", where, key, raw)
		return ""
	case s == "":
		l.errf("%s: data.label.%s is required: a label declares both forms", where, key)
		return ""
	case s != v:
		l.errf("%s: data.label.%s: no leading or trailing whitespace", where, key)
		return ""
	case strings.ContainsAny(s, "\n\r"):
		l.errf("%s: data.label.%s: a short single-line caption, no newlines", where, key)
		return ""
	case len(s) > maxDisplayName:
		// Bytes, as parseDisplayName counts them: the same bound.
		l.errf("%s: data.label.%s: a short caption (at most %d chars), got %d", where, key, maxDisplayName, len(s))
		return ""
	}
	return s
}
