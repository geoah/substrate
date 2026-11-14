package engine

// The edit built-in: an agent reads one long text property by line and
// changes it by exact replacement, instead of reading and rewriting the whole
// value. A function's source, a bundle module, an agent's prompt and a long
// markdown body are what it is for. A replacement is applied to the version
// the agent read and lands through the same patch `write` takes, so the
// effective emit, the declaration rule, the policy door and admission all
// hold it, and its changelog entries ride the tool row.

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// editViewLines is how many lines one view answers when it names no end.
const editViewLines = 400

// editableDatatypes are the property types edit reads and writes: text a
// person or a model reads by line. A secret or a digest is never one.
var editableDatatypes = []vocabulary.Datatype{vocabulary.DatatypeString, vocabulary.DatatypeText, vocabulary.DatatypeMarkdown}

func (l *agentLoop) dispatchEdit(ctx context.Context, c *toolRun, args map[string]any) (string, bool) {
	for _, k := range sortedKeys(args) {
		switch k {
		case "op", "kind", "id", "property", "key", "start", "end", "edits", "ifVersion":
		default:
			return toolError(fmt.Sprintf("unknown argument %q; edit takes op, kind, id, property, key, start, end, edits, ifVersion", k)), false
		}
	}
	op, _ := args["op"].(string)
	kind, _ := args["kind"].(string)
	id, _ := args["id"].(string)
	prop, _ := args["property"].(string)
	key, _ := args["key"].(string)
	if kind == "" || id == "" || prop == "" {
		return toolError("kind, id and property are required: edit addresses one text property of one record"), false
	}
	if ok, err := l.readsAllow(ctx, kind); err != nil {
		return toolError(err.Error()), false
	} else if !ok {
		return toolError(fmt.Sprintf("%s is not in agent %s's reads allowlist", kind, l.ag.Identity())), false
	}
	ty, err := l.ds.resolveType(kind)
	if err != nil {
		return toolError(err.Error()), false
	}
	p := ty.Props[prop]
	if p == nil {
		return toolError(fmt.Sprintf("%s declares no property %q", kind, prop)), false
	}
	if !slices.Contains(editableDatatypes, p.Datatype) || p.Repeated || p.Keyed != (key != "") {
		return toolError(fmt.Sprintf("property %q is %s%s: edit takes a string, text or markdown value, and key names one entry of a keyed one", prop, p.Datatype, containerWord(p))), false
	}
	rec, err := l.ds.Get(ctx, kind, id)
	if err != nil {
		return toolError(err.Error()), false
	}
	text, err := editTarget(rec.Properties[prop], key)
	if err != nil {
		return toolError(fmt.Sprintf("%s: %v", prop, err)), false
	}
	switch op {
	case "view":
		return l.editView(rec, text, args)
	case "replace":
		return l.editReplace(ctx, c, rec, prop, key, text, args)
	}
	return toolError("op must be view or replace"), false
}

// editView answers numbered lines of the text, and the version a replace
// must name.
func (l *agentLoop) editView(rec *substrate.Record, text string, args map[string]any) (string, bool) {
	lines := strings.Split(text, "\n")
	start, end := 1, min(len(lines), editViewLines)
	if f, ok := anyFloat(args["start"]); ok {
		start = int(f)
	}
	if f, ok := anyFloat(args["end"]); ok {
		end = int(f)
	} else if start > 1 {
		end = min(len(lines), start+editViewLines-1)
	}
	if start < 1 || end < start || start > len(lines) {
		return toolError(fmt.Sprintf("start and end must lie within 1..%d, start first", len(lines))), false
	}
	// A view is at most editViewLines whatever end asks for; the answer's
	// end says where it stopped, so the next view starts after it.
	end = min(end, len(lines), start+editViewLines-1)
	var b strings.Builder
	for i := start; i <= end; i++ {
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\t')
		b.WriteString(lines[i-1])
		b.WriteByte('\n')
	}
	return toolJSON(map[string]any{
		"version": rec.Version, "lines": len(lines), "start": start, "end": end, "text": b.String(),
	}), true
}

// editReplace applies every edit to the text as read, each matching exactly
// once, and patches the property with the result under ifVersion. Nothing
// lands unless every edit applies.
func (l *agentLoop) editReplace(ctx context.Context, c *toolRun, rec *substrate.Record, prop, key, text string, args map[string]any) (string, bool) {
	ifVersion, err := versionArg(args["ifVersion"])
	if err != nil {
		return toolError(err.Error()), false
	}
	if ifVersion == nil {
		return toolError("ifVersion is required: the version a view answered, so the edits apply to the text you read"), false
	}
	if *ifVersion != rec.Version {
		return toolError(fmt.Sprintf("the record is at version %d, not %d: view it again and edit what is there now", rec.Version, *ifVersion)), false
	}
	edits, err := editsArg(args["edits"])
	if err != nil {
		return toolError(err.Error()), false
	}
	for i, e := range edits {
		switch n := strings.Count(text, e.Old); n {
		case 1:
			text = strings.Replace(text, e.Old, e.New, 1)
		case 0:
			return toolError(fmt.Sprintf("edits[%d]: old matches nothing in the text as it stands after the edits before it; nothing applied", i)), false
		default:
			return toolError(fmt.Sprintf("edits[%d]: old matches %d places; add surrounding lines until it matches one. Nothing applied", i, n)), false
		}
	}
	var value any = text
	if key != "" {
		entries, _ := rec.Properties[prop].(map[string]any)
		next := maps.Clone(entries)
		next[key] = text
		value = next
	}
	target := &agentWriteDataset{Dataset: l.ds, loop: l, call: c}
	e, err := target.Patch(ctx, l.actor, rec.Kind, rec.ID, substrate.PatchInput{
		Properties: map[string]any{prop: value}, IfVersion: ifVersion,
	})
	if err != nil {
		return toolError(err.Error()), false
	}
	return toolJSON(map[string]any{"kind": e.Kind, "id": e.ID, "version": e.Version, "edits": len(edits)}), true
}

type textEdit struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// editsArg reads `edits`: a list of {old, new}, or the same list encoded as a
// JSON string. Each edit names exactly old and new, both strings: a lenient
// decode reads a misspelled or missing new as "" and deletes the text, so
// deleting is said with an explicit "new": "". An empty old would match
// everywhere, so it is refused.
func editsArg(v any) ([]textEdit, error) {
	raw, err := json.Marshal(v)
	if s, ok := v.(string); ok {
		raw, err = []byte(s), nil
	}
	if err != nil {
		return nil, err
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 {
		return nil, fmt.Errorf("edits is required: a list of {old, new}, old copied exactly from a view")
	}
	edits := make([]textEdit, len(list))
	for i, e := range list {
		for _, k := range slices.Sorted(maps.Keys(e)) {
			if k != "old" && k != "new" {
				return nil, fmt.Errorf("edits[%d]: unknown key %q; an edit is {old, new}", i, k)
			}
		}
		old, okOld := jsonText(e["old"])
		repl, okNew := jsonText(e["new"])
		if !okOld || !okNew {
			return nil, fmt.Errorf(`edits[%d]: an edit names old and new, both strings; "new": "" deletes`, i)
		}
		if old == "" {
			return nil, fmt.Errorf("edits[%d]: old is empty; it must be text copied exactly from a view", i)
		}
		edits[i] = textEdit{Old: old, New: repl}
	}
	return edits, nil
}

// jsonText decodes raw as a JSON string; absent, null and every other value
// are not one.
func jsonText(raw json.RawMessage) (string, bool) {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// editTarget is the text a property holds, or one entry of a keyed one; an
// absent value is the empty text, so a view of it answers one empty line.
func editTarget(v any, key string) (string, error) {
	if key != "" {
		entries, _ := v.(map[string]any)
		v = entries[key]
		if v == nil {
			return "", fmt.Errorf("holds no entry %q", key)
		}
	}
	switch s := v.(type) {
	case nil:
		return "", nil
	case string:
		return s, nil
	}
	return "", fmt.Errorf("holds no text")
}

func containerWord(p *vocabulary.Property) string {
	switch {
	case p.Repeated:
		return ", repeated"
	case p.Keyed:
		return ", keyed"
	}
	return ""
}

// readsAllow holds a kind to the agent's reads allowlist and to what the run
// may read (run.go), as `query` does.
func (l *agentLoop) readsAllow(ctx context.Context, kind string) (bool, error) {
	if l.ag.Reads == nil {
		return false, nil
	}
	scope, err := l.ds.heldScope(ctx, queryScope{kinds: l.ag.Reads.Kinds})
	if err != nil {
		return false, err
	}
	return scope.allows(kind), nil
}
