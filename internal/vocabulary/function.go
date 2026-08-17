package vocabulary

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"

	"github.com/geoah/substrate/internal/substrate"
)

// A function is the seventh manifest kind: a pure reusable CALLABLE — a named
// piece of real code, inline Python or Go source on the manifest (`runtime:`
// + `source:`) — with a model-facing `description`, optional `arguments:` and
// `returns:` shapes, and a `permissions:` grant (reads, writes, call, network,
// mutations). A function has NO subscription:
// what fires it is a `trigger` data record (core.substrate.reamde.dev), which
// owns the cursor, retries, parking and replay. The body executes in the
// shared runner child process (functions/runner) and returns effects the
// engine applies plus an output value for callers; CEL survives ONLY as the
// trigger's `when:` guard dialect — load-compiled, read-free.

// The two inline runtimes. Python source is exec'd into the shared runner
// host at registration; Go source compiles at registration to a cached
// binary the runner supervises.
const (
	RuntimePython = "python"
	RuntimeGo     = "go"
	// RuntimeHost is the third runtime, and the one with no body: the ENGINE is
	// the implementation. A host function's declaration is its whole
	// card — the description, `arguments:` and `returns:` — and it never reaches
	// the child-process runner. It is admissible ONLY in a builtin build
	// (BuildAuthorities' SourceBuiltin), because the engine can only implement
	// what the engine ships: a bundle or an owner declaring one would name a
	// body nothing has.
	RuntimeHost = "host"
)

// FunctionRuntimes lists the runtimes in the order the errors name them.
var FunctionRuntimes = []string{RuntimePython, RuntimeGo, RuntimeHost}

var functionRuntimes = map[string]bool{
	RuntimePython: true, RuntimeGo: true, RuntimeHost: true,
}

// The four host functions the engine implements, by identity. They are ordinary
// function records — seeded, browsable, pickable as agent tools by reference —
// and these constants are what the dispatch switch and the grant checks key on.
const (
	// The declared effect classes and confirmation floors
	// (docs/plans/thread-interactions.md): the author's objective facts the
	// policy layer reads.
	FunctionEffectRead         = "read"
	FunctionEffectWrite        = "write"
	FunctionEffectExternal     = "external"
	FunctionEffectIrreversible = "irreversible"
	FunctionConfirmPolicy      = "policy"
	FunctionConfirmAlways      = "always"

	HostFunctionQuery   = AuthorityCore + "/query"
	HostFunctionPropose = AuthorityCore + "/propose"
	HostFunctionGraphQL = AuthorityCore + "/graphql"
	HostFunctionMutate  = AuthorityCore + "/mutate"
	HostFunctionAsk     = AuthorityCore + "/ask"
)

// The capability-gated identity mutations (`permissions.mutations`). The
// five ordinary effects — put, patch, delete, link, unlink — are granted by
// `permissions.writes` alone; merge and split need this explicit grant.
const (
	MutationMerge = "merge"
	MutationSplit = "split"
)

var functionMutations = map[string]bool{MutationMerge: true, MutationSplit: true}

// The op vocabulary a trigger's `source.record.ops` declares. The dispatcher
// maps changelog ops onto these three: a put whose payload says `created` is
// a create, delete (and gc) is a delete, everything else is an update.
const (
	FunctionOpCreate = "create"
	FunctionOpUpdate = "update"
	FunctionOpDelete = "delete"
)

var functionOps = map[string]bool{
	FunctionOpCreate: true, FunctionOpUpdate: true, FunctionOpDelete: true,
}

// ValidFunctionOp reports whether op is one of create/update/delete — the
// vocabulary trigger sources declare.
func ValidFunctionOp(op string) bool { return functionOps[op] }

// Function is one parsed function: the callable and nothing more.
type Function struct {
	Name      string
	Authority string
	// Description is model-facing and REQUIRED: the function is its own tool
	// card.
	Description string
	// Effect is the author's declared effect class — read, write, external or
	// irreversible — an objective fact the policy layer reads, never a grant.
	Effect string
	// Confirmation is the declared floor: "always" means the function's
	// effects are NEVER auto-applied, whatever any policy or judge says;
	// "policy" (and absent) leaves the owner's policies to decide. Floors
	// only tighten.
	Confirmation string
	// Runtime names the body's language; Source is the inline body itself.
	// Python's entrypoint is `main(input, host)`; Go's is
	// `Main(in *substratefn.Input, host *substratefn.Host) (*substratefn.Result, error)`.
	// On RuntimeHost there is no body at all: Source is empty and the engine is
	// the implementation.
	Runtime string
	Source  string
	// TimeoutMs bounds one invocation's wall clock; a timeout rides the
	// normal retries then parks.
	TimeoutMs int
	// Input and Output are the optional shape schemas CheckValue validates
	// against: call-mode arguments are held to Input, a declared Output checks
	// the returned value. Each is COMPILED from the flat `arguments:`/`returns:`
	// list the declaration carries, so the schema is one level of named arguments
	// and every model-facing card is valid by construction.
	Input  map[string]any
	Output map[string]any
	// Caps is the capability envelope every function carries.
	Caps FunctionCaps

	// Definition is the declaration's own data map, exactly as authored — what
	// the row stores as its properties (engine/vocabularywrite.go
	// authorityDeclarations). The retired `definition` blob is a different thing
	// and has no spelling left: this is the document, not a wrapper around it.
	Definition map[string]any
}

// FunctionCaps is the capability envelope: what the body's effects may address,
// what its host reads may touch, which functions it may Call, what network it
// declares, and which identity mutations it is granted.
type FunctionCaps struct {
	// Emit is the allowlist of full type identities the effects may address.
	Emit []string
	// Reads is the host read capability; nil means the function declared no
	// reads and every host read call trips.
	Reads *FunctionReads
	// Call is the allowlist of function identities the body's host Call may
	// invoke; empty means every sub-call trips.
	Call []string
	// Network is the declared egress allowlist (URL patterns). Declared for
	// review; the V1 same-host runner does not yet enforce it.
	Network []string
	// Mutations grants the capability-gated effects: merge, split.
	Mutations []string
}

// AllowsMutation reports whether the envelope grants a gated mutation.
func (c FunctionCaps) AllowsMutation(m string) bool {
	for _, v := range c.Mutations {
		if v == m {
			return true
		}
	}
	return false
}

// AllowsCall reports whether the envelope grants a sub-call to a function.
func (c FunctionCaps) AllowsCall(ident string) bool {
	for _, v := range c.Call {
		if v == ident {
			return true
		}
	}
	return false
}

// FunctionReads is the read capability: the allowlist of types the body's
// Get/List/Search may touch, and the per-invocation budget — declared for
// review, enforced host-side per call.
type FunctionReads struct {
	Kinds []string
	Calls int
	Rows  int
}

// The inline-source bounds: the loader refuses a manifest outside them, so a
// manifest that loaded always registers.
const (
	SourceMaxBytes      = 256 << 10
	DefaultRunTimeoutMs = 5000
	MaxRunTimeoutMs     = 60000
	DefaultReadCalls    = 16
	MaxReadCalls        = 1000
	DefaultReadRows     = 500
	MaxReadRows         = 10000
)

// Identity is "<authority>/<name>".
func (f *Function) Identity() string { return KindRef(f.Authority, f.Name) }

// IsHost reports whether the engine is this function's body. Every runner path
// asks before it reaches for a spec: there is nothing to compile, register,
// warm or reconcile, and the caller's own grants are what scope the call.
func (f *Function) IsHost() bool { return f.Runtime == RuntimeHost }

// Actor is the function's own writing hand: `function:<authority>:<name>`,
// the actor its effects are attributed to and the one trigger self-exclusion
// keys on.
//
// It carries the DECLARING AUTHORITY, not the local name alone. Two
// authorities declaring a function of one name used to write under one actor,
// so each one's trigger excluded the other's writes as its own echo and
// dropped them silently (record 0025). The colon is the separator because
// `<actor>/<name>` metadata keys reserve the slash.
func (f *Function) Actor() string { return string(substrate.FunctionActor(f.Authority, f.Name)) }

// MatchTypeGlob matches one trigger `source.record.kinds` pattern against a
// kind reference: `*` matches every kind, `<authority>/*` every kind that
// authority publishes, anything else is an exact reference.
func MatchTypeGlob(pat, ident string) bool {
	switch {
	case pat == "*":
		return true
	case strings.HasSuffix(pat, "/*"):
		return strings.HasPrefix(ident, pat[:len(pat)-1])
	default:
		return pat == ident
	}
}

// ValidTypeGlob accepts the three trigger-source spellings: `*`,
// `<authority>/*`, or a kind reference — qualified, or the bare name of a
// repository-local kind.
func ValidTypeGlob(pat string) bool {
	switch {
	case pat == "*":
		return true
	case strings.HasSuffix(pat, "/*"):
		return ValidAuthority(pat[:len(pat)-2])
	case strings.Contains(pat, "*"):
		return false
	default:
		authority, name := SplitKindRef(pat)
		if authority != "" && !ValidAuthority(authority) {
			return false
		}
		return ValidName(name)
	}
}

// --- the when-guard CEL contract ----------------------------------------------

// The bindings a trigger's `when:` guard sees, all read-only: the triggering
// change, the record's CURRENT state (null after delete), and the repository.
// Declared here so triggers compile against exactly what the engine binds.
var (
	celEnvOnce sync.Once
	celEnvVal  *cel.Env
	celEnvErr  error
)

// FunctionCELEnv is the one environment `when:` guards compile and evaluate
// in. CEL is the guard dialect and nothing more: bodies are Python/Go.
func FunctionCELEnv() (*cel.Env, error) {
	celEnvOnce.Do(func() {
		celEnvVal, celEnvErr = cel.NewEnv(
			cel.Variable("change", cel.MapType(cel.StringType, cel.DynType)),
			// record is null after a delete, so it stays dyn.
			cel.Variable("record", cel.DynType),
			cel.Variable("repository", cel.MapType(cel.StringType, cel.DynType)),
		)
	})
	return celEnvVal, celEnvErr
}

// Runaway-evaluation bounds: a guard is one bounded computation, never a loop
// the host waits on.
const (
	celInterruptFrequency = 128
	celCostLimit          = 1_000_000
)

// CompileWhen parses and checks one guard against the declared bindings.
// The guard returns a boolean; dyn satisfies it, since most map accesses
// erase static types. The engine compiles a trigger's `when:` through this
// at write admission and again (cached) at dispatch.
func CompileWhen(src string) (cel.Program, error) {
	env, err := FunctionCELEnv()
	if err != nil {
		return nil, err
	}
	ast, iss := env.Compile(src)
	if iss != nil && iss.Err() != nil {
		return nil, iss.Err()
	}
	if out := ast.OutputType().String(); out != "dyn" && out != "bool" {
		return nil, fmt.Errorf("must return a boolean, returns %s", out)
	}
	return env.Program(ast,
		cel.InterruptCheckFrequency(celInterruptFrequency),
		cel.CostLimit(celCostLimit),
	)
}

// --- the flat argument dialect -------------------------------------------------

// The argument types `arguments:`/`returns:` declare. `int` and `float` are two
// words for one wire number, kept apart because the declaration is also
// documentation; `enum` is a string closed over its `values`; `json` is the
// named escape hatch for a value whose shape the function does not own.
const (
	ArgumentString = "string"
	ArgumentInt    = "int"
	ArgumentFloat  = "float"
	ArgumentBool   = "bool"
	ArgumentEnum   = "enum"
	ArgumentJSON   = "json"
)

// ArgumentTypes lists the argument types in the order the errors name them.
var ArgumentTypes = []string{
	ArgumentString, ArgumentInt, ArgumentFloat, ArgumentBool, ArgumentEnum, ArgumentJSON,
}

// argumentSchemaTypes maps each argument type onto the shape dialect's own
// type — the compiled schema is what CheckValue and the model-facing tool card
// read, so a flat declaration cannot produce a schema either of them refuses.
//
// `json` maps to the EMPTY string, which compiles to a schema with no `type` key
// at all: that is JSON Schema's own spelling of "any value", and the compiled
// schema is handed to a provider verbatim as the tool card. A literal
// `{type: any}` was neither — no validator admits it — so the escape hatch used
// to compile to a card a strict provider refuses.
var argumentSchemaTypes = map[string]string{
	ArgumentString: "string",
	ArgumentInt:    "number",
	ArgumentFloat:  "number",
	ArgumentBool:   "boolean",
	ArgumentEnum:   "string",
	ArgumentJSON:   "",
}

// functionArgKeys is one argument's closed key set.
var functionArgKeys = map[string]bool{
	"name": true, "type": true, "repeated": true, "required": true,
	"description": true, "values": true,
}

// parseArguments compiles a flat argument list into the object schema the
// engine holds calls to. The list is the authored form and the compiled schema
// is what every consumer reads: one level of named arguments, so a function's
// card is valid by construction and CheckValue never walks a recursion.
func (l *loader) parseArguments(where string, v any) map[string]any {
	list, ok := v.([]any)
	if !ok {
		l.errf("%s: a LIST of named arguments ({name, type}) — the shape is flat", where)
		return nil
	}
	props := map[string]any{}
	var required []any
	for i, av := range list {
		awhere := fmt.Sprintf("%s[%d]", where, i)
		ad := asMapOrNil(av)
		if ad == nil {
			l.errf("%s: an argument is a {name, type} map, got %T", awhere, av)
			return nil
		}
		l.checkKeys(awhere, ad, functionArgKeys)
		name := mstr(ad, "name")
		if !ValidCamel(name) {
			l.errf("%s.name: %q must be %s", awhere, name, camelRule)
			return nil
		}
		if _, dup := props[name]; dup {
			l.errf("%s.name: %q is declared twice", awhere, name)
			return nil
		}
		ty := mstr(ad, "type")
		schemaType, known := argumentSchemaTypes[ty]
		if !known {
			l.errf("%s.type: %q — one of %s", awhere, ty, strings.Join(ArgumentTypes, ", "))
			return nil
		}
		leaf := map[string]any{}
		if schemaType != "" {
			leaf["type"] = schemaType
		}
		values, hasValues := ad["values"]
		switch {
		case ty == ArgumentEnum && !hasValues:
			l.errf("%s: type enum declares its values — an enum without them is a string", awhere)
			return nil
		case ty != ArgumentEnum && hasValues:
			l.errf("%s: values belongs to type enum, not %s", awhere, ty)
			return nil
		case hasValues:
			enum := l.parseArgumentValues(awhere, values)
			if enum == nil {
				return nil
			}
			leaf["enum"] = enum
		}
		schema := leaf
		if mbool(ad, "repeated") {
			schema = map[string]any{"type": "array", "items": leaf}
		}
		// The description belongs to the argument, so it rides the outer schema:
		// a repeated argument is described once, not once per item.
		if desc := l.parseDescription(awhere, ad); desc != "" {
			schema["description"] = desc
		}
		props[name] = schema
		if mbool(ad, "required") {
			required = append(required, name)
		}
	}
	out := map[string]any{"type": "object", "properties": props}
	// An absent `required` is an object with no required argument, which is not
	// the same document as one listing none: the compiled schema carries the key
	// only when something is required.
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// parseArgumentValues reads an enum argument's admitted values. They are wire
// values a model echoes back verbatim, not declared names, so the casing rule
// every declared name holds to does not apply — only that each is a non-empty
// string, which is what the compiled `string` type promises.
func (l *loader) parseArgumentValues(where string, v any) []any {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		l.errf("%s.values: a non-empty LIST of the values the argument admits", where)
		return nil
	}
	out := make([]any, 0, len(list))
	for i, ev := range list {
		s, ok := ev.(string)
		if !ok || s == "" {
			l.errf("%s.values[%d]: %v — a non-empty string", where, i, ev)
			return nil
		}
		out = append(out, s)
	}
	return out
}

// parseFunctionIO reads one side of a function's IO: the named argument list, or
// nothing at all, which is a side the function does not constrain.
func (l *loader) parseFunctionIO(where string, data map[string]any, key string) (map[string]any, bool) {
	raw, declared := data[key]
	if !declared {
		return nil, true
	}
	schema := l.parseArguments(where+": data."+key, raw)
	return schema, schema != nil
}

// CheckValue holds one value to a declared input/output schema: shape only.
// Objects refuse undeclared keys — the schema is the contract, not a hint.
func CheckValue(schema map[string]any, v any) error {
	return checkValue("", schema, v)
}

func checkValue(path string, schema map[string]any, v any) error {
	at := func() string {
		if path == "" {
			return "value"
		}
		return path
	}
	ty, _ := schema["type"].(string)
	// No type is no constraint — the flat dialect's `json` argument, whose value
	// shape the function does not own. It is spelled as the ABSENCE of a type
	// because the same map is the model-facing card, and JSON Schema says "any
	// value" by saying nothing.
	if ty == "" {
		return nil
	}
	switch ty {
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected an object, got %T", at(), v)
		}
		// Key PRESENCE closes the object: `properties: {}` declares "no
		// properties" and refuses every key; only a schema with NO
		// properties key at all is the bare open object.
		propsRaw, hasProps := schema["properties"]
		props, _ := propsRaw.(map[string]any)
		for _, nv := range mslice(schema, "required") {
			name := fmt.Sprint(nv)
			if _, has := m[name]; !has {
				return fmt.Errorf("%s: missing required property %q", at(), name)
			}
		}
		for _, k := range sortedKeys(m) {
			sub, declared := props[k].(map[string]any)
			if !declared {
				if !hasProps {
					continue // a bare object schema constrains shape only
				}
				return fmt.Errorf("%s: unknown property %q", at(), k)
			}
			if err := checkValue(path+"."+k, sub, m[k]); err != nil {
				return err
			}
		}
		return nil
	case "array":
		items, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s: expected an array, got %T", at(), v)
		}
		sub, has := schema["items"].(map[string]any)
		if !has {
			return nil
		}
		for i, it := range items {
			if err := checkValue(fmt.Sprintf("%s[%d]", path, i), sub, it); err != nil {
				return err
			}
		}
		return nil
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s: expected a string, got %T", at(), v)
		}
		return nil
	case "number":
		switch v.(type) {
		case float64, float32, int, int64:
			return nil
		}
		return fmt.Errorf("%s: expected a number, got %T", at(), v)
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: expected a boolean, got %T", at(), v)
		}
		return nil
	default:
		return fmt.Errorf("%s: schema declares no usable type", at())
	}
}

// --- the loader's half -------------------------------------------------------

var functionDataKeys = map[string]bool{
	"authority": true, "description": true, "runtime": true, "source": true,
	"timeoutMs": true,
	// The IO shapes are `data`'s own; the grant is ONE key beside them, holding
	// the five of functionPermissionKeys.
	"arguments": true, "returns": true, "permissions": true,
	// The author's OBJECTIVE facts for the policy layer
	// (docs/plans/thread-interactions.md): what class of effect the body has,
	// and a confirmation floor no policy or judge loosens.
	"effect": true, "confirmation": true,
}

// deletedFunctionKeys are the removed keys, each naming what replaced it: the
// CEL and wasm bodies are removed (POC verdicts, ticket 009), the subscription
// moved onto trigger records, and the typed core retired the wrapper and the
// recursive IO schemas. No compatibility shim for any of them — the rows written
// that way are translated by the dialect rung
// (engine/dialectonegrammar.go), which is the last reader of those spellings.
var deletedFunctionKeys = map[string]string{
	"run":          "runtime + source — the CEL and wasm run arms are removed; CEL survives only as the trigger's when: guard",
	"on":           "a trigger record (core.substrate.reamde.dev) — the subscription lives on the trigger, the function is a pure callable",
	"when":         "trigger source.record.when — the guard lives on the trigger record",
	"coalesce":     "trigger source.record.coalesce — coalescing lives on the trigger record",
	"capabilities": "permissions: the grant is one object, and its keys are reads, writes, call, network and mutations",
	"input":        "arguments — a flat LIST of named arguments ({name, type}), so the tool card is valid by construction",
	"output":       "returns — the same flat list on the result side",
	"emit":         "permissions.writes: the grants group under `permissions:`, and the permission to write is named for writing",
	"reads":        "permissions.reads: the grants group under `permissions:`",
	"call":         "permissions.call: the grants group under `permissions:`",
	"network":      "permissions.network: the grants group under `permissions:`",
	"mutations":    "permissions.mutations: the grants group under `permissions:`",
}

// functionPermissionKeys is the grant object's five keys. The sorted order of
// the set is the order the loader reads them in, so a document with two
// problems reports the same one on every run.
var functionPermissionKeys = map[string]bool{
	"reads": true, "writes": true, "call": true, "network": true, "mutations": true,
}

var functionReadsKeys = map[string]bool{"kinds": true, "budgets": true}

var functionBudgetKeys = map[string]bool{"calls": true, "rows": true}

// parseFunction parses one function document. Emit, reads and call targets
// resolve against the registry in Finalize/Install, like edge targets.
func (l *loader) parseFunction(d Document) *Function {
	g := l.authority
	where := DocFunction + " " + d.ID
	for k := range d.Data {
		if replacement, gone := deletedFunctionKeys[k]; gone {
			l.errf("%s: key %q is deleted — %s", where, k, replacement)
			return nil
		}
	}
	l.checkKeys(where, d.Data, functionDataKeys)
	if _, has := d.Data["after"]; has {
		l.errf("%s: data.after is reserved and unimplemented — order is never implied, and no dependency edge exists yet", where)
		return nil
	}
	local, ok := l.localName(where, d.ID, g.Name)
	if !ok {
		return nil
	}
	fn := &Function{
		Name: local, Authority: g.Name,
		Description: l.parseDescriptionMax(where+": data", d.Data, maxCallableDescription),
		Definition:  d.Data,
	}
	if fn.Description == "" {
		l.errf("%s: data.description is required — the function is its own tool card", where)
		return nil
	}

	fn.Runtime = mstr(d.Data, "runtime")
	if !functionRuntimes[fn.Runtime] {
		l.errf("%s: data.runtime: %q — one of %s", where, fn.Runtime, strings.Join(FunctionRuntimes, ", "))
		return nil
	}
	fn.Effect = mstr(d.Data, "effect")
	switch fn.Effect {
	case "", FunctionEffectRead, FunctionEffectWrite, FunctionEffectExternal, FunctionEffectIrreversible:
	default:
		l.errf("%s: data.effect: %q — read, write, external or irreversible", where, fn.Effect)
		return nil
	}
	fn.Confirmation = mstr(d.Data, "confirmation")
	switch fn.Confirmation {
	case "", FunctionConfirmPolicy, FunctionConfirmAlways:
	default:
		l.errf("%s: data.confirmation: %q — \"policy\" (the default) or \"always\"", where, fn.Confirmation)
		return nil
	}
	if !l.parseFunctionBody(where, d.Data, fn) {
		return nil
	}
	if fn.Input, ok = l.parseFunctionIO(where, d.Data, "arguments"); !ok {
		return nil
	}
	if fn.Output, ok = l.parseFunctionIO(where, d.Data, "returns"); !ok {
		return nil
	}
	if fn = l.parseFunctionCaps(where, d.Data, fn); fn == nil {
		return nil
	}
	return fn
}

// parseFunctionBody reads what the runtime implies about the body: an inline
// runtime carries one, bounded and timed, and `host` carries none at all.
//
// The host arm refuses three things rather than ignoring them, because each one
// would look obeyed: a `source` (the engine is the body, so nothing would ever
// run what was written), a `timeoutMs` (nothing supervises an in-process
// built-in on the loop's own clock), and the whole declaration when the build is
// not the shipped one — a bundle or an owner cannot hand the engine an
// implementation, so a `host` runtime from any other source names a body that
// does not exist.
func (l *loader) parseFunctionBody(where string, data map[string]any, fn *Function) bool {
	if fn.IsHost() {
		if l.source != SourceBuiltin {
			l.errf("%s: data.runtime: host is the ENGINE's own implementation and only a shipped declaration may name one — an installed function declares its body (%s or %s)",
				where, RuntimePython, RuntimeGo)
			return false
		}
		if _, declared := data["source"]; declared {
			l.errf("%s: data.source: a host function has no inline body — the engine is the implementation, and the declaration is its card", where)
			return false
		}
		if _, declared := data["timeoutMs"]; declared {
			l.errf("%s: data.timeoutMs: a host function runs in process under its caller's budgets — there is no child invocation to bound", where)
			return false
		}
		return true
	}
	fn.Source = mstr(data, "source")
	if strings.TrimSpace(fn.Source) == "" {
		l.errf("%s: data.source is required — the inline %s body", where, fn.Runtime)
		return false
	}
	if len(fn.Source) > SourceMaxBytes {
		l.errf("%s: data.source is %d bytes — the inline cap is %d", where, len(fn.Source), SourceMaxBytes)
		return false
	}
	var ok bool
	if fn.TimeoutMs, ok = l.boundedInt(where+": data.timeoutMs", data, "timeoutMs",
		DefaultRunTimeoutMs, MaxRunTimeoutMs); !ok {
		return false
	}
	return true
}

// parseFunctionCaps reads the capability envelope out of `permissions:`.
// `writes` is OPTIONAL, and an absent one is a function that writes nothing: a
// pure function returns its output and stages no effect, which the emit gate
// then refuses every effect against. It used to be required and non-empty, which
// taught authors to declare a kind they never wrote to (firecrawl's websearch
// declared `webdocument` and apologized for it in a comment).
//
// ONE object holds all five, because a bare `emit:` beside `returns:` said
// nothing about being a permission and read as the output shape. The
// `capabilities:` wrapper and the five hoisted spellings are deleted keys
// (deletedFunctionKeys), so a refusal names `data.permissions.<key>` and there
// is one place to look.
func (l *loader) parseFunctionCaps(where string, data map[string]any, fn *Function) *Function {
	perms, ok := l.permissionsObject(where, data, functionPermissionKeys)
	if !ok {
		return nil
	}
	if _, isMap := perms["writes"].(map[string]any); isMap {
		l.errf("%s: data.permissions.writes: a LIST of full type identities", where)
		return nil
	}
	for i, t := range ReferentIDs(mslice(perms, "writes"), KindRef(AuthorityCore, DocKind)) {
		if !ValidKindReference(t) {
			l.errf("%s: data.permissions.writes[%d]: %q is not a kind; writes names them, bare or authority-qualified, no globs", where, i, t)
			continue
		}
		fn.Caps.Emit = append(fn.Caps.Emit, t)
	}
	for i, ident := range ReferentIDs(mslice(perms, "call"), KindRef(AuthorityCore, DocFunction)) {
		if !Qualified(ident) || strings.Contains(ident, "*") {
			l.errf("%s: data.permissions.call[%d]: %q is not a full function identity; call names them, no globs", where, i, ident)
			continue
		}
		fn.Caps.Call = append(fn.Caps.Call, ident)
	}
	for i, nv := range mslice(perms, "network") {
		pat := fmt.Sprint(nv)
		if pat == "" {
			l.errf("%s: data.permissions.network[%d]: empty pattern", where, i)
			continue
		}
		fn.Caps.Network = append(fn.Caps.Network, pat)
	}
	for i, mv := range mslice(perms, "mutations") {
		m := fmt.Sprint(mv)
		if !functionMutations[m] {
			l.errf("%s: data.permissions.mutations[%d]: %q is not one of merge, split, the gated mutations; put/patch/delete/link/unlink ride permissions.writes alone", where, i, m)
			continue
		}
		fn.Caps.Mutations = append(fn.Caps.Mutations, m)
	}
	if !l.parseReads(where, perms, fn) {
		return nil
	}
	return fn
}

// permissionsObject reads the `permissions:` grant off a declaration's data,
// held to the key set its kind admits. An absent one is the empty grant, which
// is what a declaration that asks for nothing means; a value that is not an
// object refuses, since a list of words would name grants nothing enforces.
func (l *loader) permissionsObject(where string, data map[string]any, keys map[string]bool) (map[string]any, bool) {
	raw, declared := data["permissions"]
	if !declared {
		return map[string]any{}, true
	}
	perms := asMapOrNil(raw)
	if perms == nil {
		l.errf("%s: data.permissions: an OBJECT of grants (%s), got %T", where, strings.Join(sortedKeys(mapOfAny(keys)), ", "), raw)
		return nil, false
	}
	l.checkKeys(where+": data.permissions", perms, keys)
	return perms, true
}

// parseReads reads the optional read capability off a grant. Both callers carry
// it at `data.permissions.reads` (a function's and an agent's are the same
// shape), so the path a refusal names is that one, spelled here.
func (l *loader) parseReads(where string, perms map[string]any, fn *Function) bool {
	const path = "data.permissions.reads"
	rv, has := perms["reads"]
	if !has {
		return true
	}
	r := asMap(rv)
	l.checkKeys(where+": "+path, r, functionReadsKeys)
	reads := &FunctionReads{}
	for i, t := range ReferentIDs(mslice(r, "kinds"), KindRef(AuthorityCore, DocKind)) {
		if !ValidKindReference(t) {
			l.errf("%s: %s.kinds[%d]: %q — reads names kinds, bare or authority-qualified, no globs", where, path, i, t)
			continue
		}
		reads.Kinds = append(reads.Kinds, t)
	}
	if len(reads.Kinds) == 0 {
		l.errf("%s: %s.kinds is required and non-empty — the allowlist the host holds every read to", where, path)
		return false
	}
	budgets := mmap(r, "budgets")
	l.checkKeys(where+": "+path+".budgets", budgets, functionBudgetKeys)
	var ok bool
	if reads.Calls, ok = l.boundedInt(where+": "+path+".budgets.calls", budgets, "calls",
		DefaultReadCalls, MaxReadCalls); !ok {
		return false
	}
	if reads.Rows, ok = l.boundedInt(where+": "+path+".budgets.rows", budgets, "rows",
		DefaultReadRows, MaxReadRows); !ok {
		return false
	}
	fn.Caps.Reads = reads
	return true
}

// boundedInt reads an optional positive integer with a default and a cap.
func (l *loader) boundedInt(where string, m map[string]any, key string, def, limit int) (int, bool) {
	v, has := m[key]
	if !has {
		return def, true
	}
	f, isNum := mfloat(m, key)
	if !isNum || f != float64(int(f)) || int(f) < 1 || int(f) > limit {
		l.errf("%s: %v — an integer between 1 and %d", where, v, limit)
		return 0, false
	}
	return int(f), true
}

// resolveFunction validates a function against the loaded registry: every
// written kind and read kind must exist, and every call target must be a
// registered function (same-batch installs count — resolution runs over the
// whole candidate).
//
// It also CANONICALIZES the two kind allowlists to identities, so a
// declaration written in the bare spelling and one written qualified become
// the same allowlist. Every gate downstream — the emit check on an effect, the
// runner's reads check on a host call — resolves the name it is handed and
// compares identities, so resolving the allowlist here is what makes the two
// spellings one kind rather than two.
func (r *Registry) resolveFunction(f *Function) []string {
	var problems []string
	where := DocFunction + " " + f.Identity()
	for i, t := range f.Caps.Emit {
		ty, err := r.Resolve(t)
		if err != nil || ty == nil {
			problems = append(problems, fmt.Sprintf("%s: data.permissions.writes: unknown type %q", where, t))
			continue
		}
		f.Caps.Emit[i] = ty.Identity
	}
	if f.Caps.Reads != nil {
		for i, t := range f.Caps.Reads.Kinds {
			ty, err := r.Resolve(t)
			if err != nil || ty == nil {
				problems = append(problems, fmt.Sprintf("%s: data.permissions.reads.kinds: unknown type %q", where, t))
				continue
			}
			f.Caps.Reads.Kinds[i] = ty.Identity
		}
	}
	for _, ident := range f.Caps.Call {
		target, err := r.ResolveFunction(ident)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: data.permissions.call: unknown function %q", where, ident))
			continue
		}
		// A host function runs under the GRANTS OF ITS CALLER, and a function body
		// has none to lend: its own envelope is what bounds its effects, and the
		// built-ins are bounded by an agent's reads and emit instead. So a body
		// cannot sub-call one, and the refusal names where the tool does work.
		if target.IsHost() {
			problems = append(problems, fmt.Sprintf(
				"%s: data.permissions.call: %q is a host function: the engine runs it under a CALLER's grants and a function body has none to lend, so carry it as an agent tool instead",
				where, ident))
		}
	}
	return problems
}

// --- registry lookups ----------------------------------------------------

// Functions lists every loaded function, ordered by identity.
func (r *Registry) Functions() []*Function {
	var out []*Function
	for _, g := range r.AuthorityList() {
		for _, n := range g.FunctionOrder {
			out = append(out, g.Functions[n])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

// ResolveFunction accepts a full identity or a bare name unique across
// authorities — except for a HOST function, which answers its identity alone.
// The four of them are named for what they do (`query`, `graphql`, `mutate`,
// `propose`), which is exactly what a repository's own function is likeliest to
// be called, so a bare name that resolved to the user's function before they
// shipped has to keep resolving to it.
func (r *Registry) ResolveFunction(nameOrIdentity string) (*Function, error) {
	var cands []*Function
	var host *Function
	for _, f := range r.Functions() {
		if f.Identity() == nameOrIdentity {
			return f, nil
		}
		if f.Name != nameOrIdentity {
			continue
		}
		if f.IsHost() {
			host = f
			continue
		}
		cands = append(cands, f)
	}
	switch len(cands) {
	case 0:
		if host != nil {
			return nil, fmt.Errorf("unknown function %q — the built-in of that name is %s, and a host function answers its full identity",
				nameOrIdentity, host.Identity())
		}
		return nil, fmt.Errorf("unknown function %q", nameOrIdentity)
	case 1:
		return cands[0], nil
	default:
		names := make([]string, 0, len(cands))
		for _, f := range cands {
			names = append(names, f.Identity())
		}
		sort.Strings(names)
		return nil, fmt.Errorf("ambiguous function %q: %s", nameOrIdentity, strings.Join(names, ", "))
	}
}
