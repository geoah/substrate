package catalog

import (
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The facts an approval reads about what installing a closure brings, read
// once from the documents the install applies, so a reader never walks the
// shipped YAML itself. Every map they carry is the catalog's, shared by every
// repository it serves: a reader never changes one.

// KindFact is one kind a closure declares.
type KindFact struct {
	Identity string
	// Label is the kind's declared label, nil when it declares none.
	Label *substrate.KindLabel
	// Purpose is the declared purpose, empty where it declares none, which
	// reads as primary.
	Purpose string
	// Definition is the declaration's data as shipped.
	Definition map[string]any
}

// FunctionFact is one function a closure ships: code that runs on the
// owner's server once installed, with the kinds it may read and write, each
// named in full or as a glob, and the hosts its `permissions.network`
// declares, none for a body the sandbox denies every socket.
type FunctionFact struct {
	ID      string
	Reads   []string
	Writes  []string
	Network []string
}

// TriggerFact is one trigger a closure ships, as the install writes it.
type TriggerFact struct {
	ID string
	// Callable is the reference path of what it fires.
	Callable string
	// Enabled is how it ships: an enabled record or schedule trigger runs on
	// its own once installed.
	Enabled bool
	// Webhook is whether its source is a webhook.
	Webhook bool
	// Kinds are the kinds a record trigger watches, each a reference or a
	// glob; none for a schedule or a webhook.
	Kinds []string
	// Properties are its properties as shipped.
	Properties map[string]any
}

// Kinds are the kinds the closure declares, in document order.
func (b *Bundle) Kinds() []KindFact { return b.kinds }

// Functions are the functions the closure ships, in document order.
func (b *Bundle) Functions() []FunctionFact { return b.functions }

// Triggers are the triggers the closure ships, in document order.
func (b *Bundle) Triggers() []TriggerFact { return b.triggers }

// Kind is one kind the closure declares, by its full reference.
func (b *Bundle) Kind(identity string) (KindFact, bool) {
	for _, k := range b.kinds {
		if k.Identity == identity {
			return k, true
		}
	}
	return KindFact{}, false
}

// Trigger is one trigger the closure ships, by its id.
func (b *Bundle) Trigger(id string) (TriggerFact, bool) {
	for _, t := range b.triggers {
		if t.ID == id {
			return t, true
		}
	}
	return TriggerFact{}, false
}

// readFacts fills b's facts from its documents: the declarations for kinds
// and functions, the data documents for triggers.
func (b *Bundle) readFacts() {
	for _, d := range b.vocabularyDocs {
		data := mmap(d, "data")
		switch vocabulary.KindName(mstr(d, "kind")) {
		case vocabulary.DocKind:
			b.kinds = append(b.kinds, KindFact{
				Identity: docID(d), Label: kindLabel(data), Purpose: mstr(data, "purpose"), Definition: data,
			})
		case vocabulary.DocFunction:
			perms := mmap(data, "permissions")
			b.functions = append(b.functions, FunctionFact{
				ID:      docID(d),
				Reads:   kindGrants(docID(d), mslice(mmap(perms, "reads"), "kinds")),
				Writes:  kindGrants(docID(d), mslice(perms, "writes")),
				Network: strs(mslice(perms, "network")),
			})
		case vocabulary.DocAgent:
			perms := mmap(data, "permissions")
			b.agents = append(b.agents, AgentFact{
				ID:     docID(d),
				Reads:  kindGrants(docID(d), mslice(mmap(perms, "reads"), "kinds")),
				Writes: askless(data, kindGrants(docID(d), mslice(perms, "writes"))),
			})
		case vocabulary.DocRecordMapping:
			b.mappings = append(b.mappings, MappingFact{
				ID:   docID(d),
				From: vocabulary.ReferentID(data["from"], vocabulary.CoreKind(vocabulary.DocKind)),
				To:   vocabulary.ReferentID(data["to"], vocabulary.CoreKind(vocabulary.DocKind)),
			})
		}
	}
	for _, d := range b.dataDocs {
		if mstr(d, "kind") != kindTrigger {
			continue
		}
		props := mmap(mmap(d, "data"), "properties")
		enabled, _ := props["enabled"].(bool)
		b.triggers = append(b.triggers, TriggerFact{
			ID: docID(d), Callable: vocabulary.ReferencePath(props["callable"]), Enabled: enabled,
			Webhook: vocabulary.TriggerWebhook(props) != nil, Properties: props,
			Kinds: strs(mslice(mmap(mmap(props, "source"), "record"), "kinds")),
		})
	}
	b.readReach()
}

// strs reads a list of scalars as strings, blanks dropped.
func strs(values []any) []string {
	var out []string
	for _, v := range values {
		if s, _ := v.(string); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// kindLabel is a declaration's label, nil unless it names both forms.
func kindLabel(data map[string]any) *substrate.KindLabel {
	l := mmap(data, "label")
	singular, plural := mstr(l, "singular"), mstr(l, "plural")
	if singular == "" || plural == "" {
		return nil
	}
	return &substrate.KindLabel{Singular: singular, Plural: plural}
}

// kindGrants is one kind grant list of a function or an agent (its
// `permissions.writes` or `permissions.reads.kinds`), each named in full: a
// bare name is a kind of the declaring package, and a glob stays one.
func kindGrants(id string, values []any) []string {
	entries := vocabulary.ReferentIDs(values, vocabulary.CoreKind(vocabulary.DocKind))
	authority, pkg := vocabulary.SplitPackageRef(vocabulary.KindPackage(id))
	out := make([]string, 0, len(entries))
	for _, w := range entries {
		switch {
		case w == "":
			continue
		case !vocabulary.Qualified(w) && !vocabulary.IsTypeGlob(w):
			w = vocabulary.KindRef(authority, pkg, w)
		}
		out = append(out, w)
	}
	return out
}

// AgentProviderInputs reads the provider inputs consumed by this closure's agents.
func (b *Bundle) AgentProviderInputs() []substrate.AuthorizationProviderInput {
	return AgentProviderInputs(b.ID, b.vocabularyDocs)
}

// AgentProviderInputs groups the agents in declarations by their provider input.
func AgentProviderInputs(pkg string, docs []map[string]any) []substrate.AuthorizationProviderInput {
	var out []substrate.AuthorizationProviderInput
	for _, d := range docs {
		if vocabulary.KindName(mstr(d, "kind")) != vocabulary.DocAgent {
			continue
		}
		data := mmap(d, "data")
		input := mstr(data, "providerInput")
		if input == "" {
			continue
		}
		var models []string
		entries, _ := data["models"].([]any)
		for _, entry := range entries {
			if s, ok := entry.(string); ok {
				models = append(models, s)
			}
		}
		cap, _ := vocabulary.VersionValue(mmap(data, "budgets")["spendCentsPerDay"])
		agent := substrate.AuthorizationProviderAgent{Agent: docID(d), Models: models, SpendCentsPerDay: int(cap)}
		found := false
		for i := range out {
			if out[i].Input == input {
				out[i].Agents = append(out[i].Agents, agent)
				found = true
				break
			}
		}
		if !found {
			out = append(out, substrate.AuthorizationProviderInput{Package: pkg, Input: input, Agents: []substrate.AuthorizationProviderAgent{agent}})
		}
	}
	return out
}

// askless drops the interaction kind from an agent's writes when the agent
// asks: the `ask` tool needs it granted, and what it writes is the agent's
// questions to the owner, which the owner answers, not the owner's data. No
// app scope writes `llm`, so counting it would block every app whose agent
// asks, the case 0207 already admitted beside `ask`.
func askless(data map[string]any, writes []string) []string {
	asks := false
	for _, t := range mslice(data, "tools") {
		if tool, _ := t.(map[string]any); tool != nil && vocabulary.ReferentID(tool["function"], vocabulary.CoreKind(vocabulary.DocFunction)) == vocabulary.HostFunctionAsk {
			asks = true
		}
	}
	if !asks {
		return writes
	}
	out := writes[:0:0]
	for _, w := range writes {
		if w != vocabulary.KindLLMInteraction {
			out = append(out, w)
		}
	}
	return out
}
