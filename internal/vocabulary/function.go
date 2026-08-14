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
// `returns:` shapes, and a capability envelope (emit, reads, call, network,
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
)

var functionRuntimes = map[string]bool{RuntimePython: true, RuntimeGo: true}

// The capability-gated identity mutations (`mutations:`). The
// five ordinary effects — put, patch, delete, link, unlink — are granted by
// `emit:` alone; merge and split need this explicit grant.
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
	// Runtime names the body's language; Source is the inline body itself.
	// Python's entrypoint is `main(input, host)`; Go's is
	// `Main(in *substratefn.Input, host *substratefn.Host) (*substratefn.Result, error)`.
	Runtime string
	Source  string
	// TimeoutMs bounds one invocation's wall clock; a timeout rides the
	// normal retries then parks.
	TimeoutMs int
	// Input and Output are the optional shape schemas CheckValue validates
	// against: call-mode arguments are held to Input, a declared Output checks
	// the returned value. Either is compiled from a flat `arguments:`/`returns:`
	// list or read from an `input:`/`output:` schema map — one representation
	// whichever spelling declared it.
	Input  map[string]any
	Output map[string]any
	// Caps is the capability envelope every function carries.
	Caps FunctionCaps

	// Definition is the manifest's data map, exactly as authored.
	Definition map[string]any
	// SourceYAML is the verbatim manifest; installed authorities have no original
	// text, so theirs is derived.
	SourceYAML string

	// capsPath remembers where each capability key was authored. Resolution
	// against the registry happens long after the parse (a call target may be
	// installed in the same batch), and its refusals have to name the line the
	// author wrote, not the one this loader would have preferred.
	capsPath map[string]string
}

// capPath is the authored location of one capability key, for an error message.
func (f *Function) capPath(key string) string {
	if p, ok := f.capsPath[key]; ok {
		return p
	}
	return "data." + key
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

// Actor is the function's own writing hand: `function:<name>`, the actor its
// effects are attributed to and the one trigger self-exclusion keys on.
//
// It carries the LOCAL name, not the identity: the actor grammar is a closed
// set of flat words (naming.go reActor) and a slash or a dot is not in it. So
// two authorities declaring a function of the same name write under one actor,
// and each one's trigger excludes the other's writes as though they were its
// own. The shipped bundles keep their local names distinct for exactly this
// reason; a repository that installs two bundles colliding on a name is
// known-issue territory, not something this line can fix.
func (f *Function) Actor() string { return substrate.FunctionActorPrefix + f.Name }

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

// --- the input/output shape dialect --------------------------------------------

// The minimal shape dialect `input:`/`output:` declare — deliberately tiny:
// `type` (object/array/string/number/boolean/any), `properties` (object),
// `items` (array), `required` (object), `description`. Shape only, no
// formats, no unions, no refs.
var ioSchemaKeys = map[string]bool{
	"type": true, "description": true, "properties": true, "items": true, "required": true,
}

var ioSchemaTypes = map[string]bool{
	"object": true, "array": true, "string": true, "number": true, "boolean": true, "any": true,
}

// parseIOSchema validates one input/output schema at load; every problem is a
// loader hard error.
func (l *loader) parseIOSchema(where string, v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		l.errf("%s: a schema is a map with a type", where)
		return nil
	}
	for k := range m {
		if !ioSchemaKeys[k] {
			l.errf("%s: unknown key %q — type, description, properties, items, required", where, k)
			return nil
		}
	}
	ty, _ := m["type"].(string)
	if !ioSchemaTypes[ty] {
		l.errf("%s: type %q — object, array, string, number, boolean or any", where, m["type"])
		return nil
	}
	props, hasProps := m["properties"]
	if hasProps && ty != "object" {
		l.errf("%s: properties only belongs on type object", where)
		return nil
	}
	if hasProps {
		pm, ok := props.(map[string]any)
		if !ok {
			l.errf("%s: properties is a map of name → schema", where)
			return nil
		}
		for _, name := range sortedKeys(pm) {
			if l.parseIOSchema(where+".properties."+name, pm[name]) == nil {
				return nil
			}
		}
	}
	if items, has := m["items"]; has {
		if ty != "array" {
			l.errf("%s: items only belongs on type array", where)
			return nil
		}
		if l.parseIOSchema(where+".items", items) == nil {
			return nil
		}
	}
	if req, has := m["required"]; has {
		if ty != "object" {
			l.errf("%s: required only belongs on type object", where)
			return nil
		}
		names, ok := req.([]any)
		if !ok {
			l.errf("%s: required is a list of property names", where)
			return nil
		}
		pm, _ := props.(map[string]any)
		for i, nv := range names {
			name := fmt.Sprint(nv)
			if _, declared := pm[name]; !declared {
				l.errf("%s: required[%d]: %q is not a declared property", where, i, name)
				return nil
			}
		}
	}
	return m
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
var argumentSchemaTypes = map[string]string{
	ArgumentString: "string",
	ArgumentInt:    "number",
	ArgumentFloat:  "number",
	ArgumentBool:   "boolean",
	ArgumentEnum:   "string",
	ArgumentJSON:   "any",
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
		leaf := map[string]any{"type": schemaType}
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

// parseFunctionIO reads one side of a function's IO. `flat` is the named
// argument list; `nested` is the schema map. One side is declared once: both
// spellings on one function leaves the reader to pick which shape is the
// contract.
func (l *loader) parseFunctionIO(where string, data map[string]any, flat, nested string) (map[string]any, bool) {
	flatRaw, hasFlat := data[flat]
	nestedRaw, hasNested := data[nested]
	switch {
	case hasFlat && hasNested:
		l.errf("%s: data.%s and data.%s both declare one shape — %s is the flat spelling and it is enough", where, flat, nested, flat)
		return nil, false
	case hasFlat:
		schema := l.parseArguments(where+": data."+flat, flatRaw)
		return schema, schema != nil
	case hasNested:
		schema := l.parseIOSchema(where+": data."+nested, nestedRaw)
		return schema, schema != nil
	}
	return nil, true
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
	if ty == "any" {
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
	"timeoutMs": true, "capabilities": true, "input": true, "output": true,
	// The flat IO spelling (`arguments:`/`returns:`) and the capability envelope
	// hoisted onto data — the five keys of functionCapsKeys, admitted here too.
	"arguments": true, "returns": true,
	"emit": true, "reads": true, "call": true, "network": true, "mutations": true,
}

// deletedFunctionKeys are the removed keys, each naming what replaced it: the
// CEL and wasm bodies are removed (POC verdicts, ticket 009), and the
// subscription moved onto trigger records. No compatibility shim.
var deletedFunctionKeys = map[string]string{
	"run":      "runtime + source — the CEL and wasm run arms are removed; CEL survives only as the trigger's when: guard",
	"on":       "a trigger record (core.substrate.reamde.dev) — the subscription lives on the trigger, the function is a pure callable",
	"when":     "trigger source.record.when — the guard lives on the trigger record",
	"coalesce": "trigger source.record.coalesce — coalescing lives on the trigger record",
}

// functionCapsKeys is the capability envelope's five keys, each declared on
// `data` itself or under the `capabilities:` wrapper. The sorted order of the
// set is the order the loader reads them in, so a document with two problems
// reports the same one on every run.
var functionCapsKeys = map[string]bool{
	"emit": true, "reads": true, "call": true, "network": true, "mutations": true,
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
		Description: l.parseDescription(where+": data", d.Data),
		Definition:  d.Data, SourceYAML: d.Source,
	}
	if fn.Description == "" {
		l.errf("%s: data.description is required — the function is its own tool card", where)
		return nil
	}

	fn.Runtime = mstr(d.Data, "runtime")
	if !functionRuntimes[fn.Runtime] {
		l.errf("%s: data.runtime: %q — python or go", where, fn.Runtime)
		return nil
	}
	fn.Source = mstr(d.Data, "source")
	if strings.TrimSpace(fn.Source) == "" {
		l.errf("%s: data.source is required — the inline %s body", where, fn.Runtime)
		return nil
	}
	if len(fn.Source) > SourceMaxBytes {
		l.errf("%s: data.source is %d bytes — the inline cap is %d", where, len(fn.Source), SourceMaxBytes)
		return nil
	}
	if fn.TimeoutMs, ok = l.boundedInt(where+": data.timeoutMs", d.Data, "timeoutMs",
		DefaultRunTimeoutMs, MaxRunTimeoutMs); !ok {
		return nil
	}
	if fn.Input, ok = l.parseFunctionIO(where, d.Data, "arguments", "input"); !ok {
		return nil
	}
	if fn.Output, ok = l.parseFunctionIO(where, d.Data, "returns", "output"); !ok {
		return nil
	}
	if fn = l.parseFunctionCaps(where, d.Data, fn); fn == nil {
		return nil
	}
	canonicalFunctionIO(d.Data)
	canonicalFunctionCaps(d.Data)
	return fn
}

// parseFunctionCaps reads the capability envelope. `emit` is required and
// non-empty: a function that writes nothing is not a function yet.
//
// Each of the five keys is read from `data` itself or from the `capabilities:`
// wrapper, and one key in both places is refused: two declarations of one grant
// would leave a reader of the document guessing which one the engine enforces.
// Every refusal names the location the key was AUTHORED at, so the fix goes
// where the author is already looking.
func (l *loader) parseFunctionCaps(where string, data map[string]any, fn *Function) *Function {
	wrapped := mmap(data, "capabilities")
	l.checkKeys(where+": data.capabilities", wrapped, functionCapsKeys)
	_, hasWrapper := data["capabilities"]
	caps := map[string]any{}
	fn.capsPath = map[string]string{}
	for _, k := range sortedKeys(mapOfAny(functionCapsKeys)) {
		hoisted, isHoisted := data[k]
		nested, isNested := wrapped[k]
		switch {
		case isHoisted && isNested:
			l.errf("%s: %q is declared at data.%s AND at data.capabilities.%s — one grant, declared once", where, k, k, k)
			return nil
		case isHoisted:
			caps[k], fn.capsPath[k] = hoisted, "data."+k
		case isNested:
			caps[k], fn.capsPath[k] = nested, "data.capabilities."+k
		case hasWrapper:
			// Absent: the path a missing key WOULD be written at, which is the
			// spelling the rest of this document uses.
			fn.capsPath[k] = "data.capabilities." + k
		default:
			fn.capsPath[k] = "data." + k
		}
	}
	if _, isList := caps["emit"].(map[string]any); isList {
		l.errf("%s: %s: a LIST of full type identities", where, fn.capPath("emit"))
		return nil
	}
	for i, ev := range mslice(caps, "emit") {
		t := fmt.Sprint(ev)
		if !ValidKindReference(t) {
			l.errf("%s: %s[%d]: %q — emit names kinds, bare or authority-qualified, no globs", where, fn.capPath("emit"), i, t)
			continue
		}
		fn.Caps.Emit = append(fn.Caps.Emit, t)
	}
	if len(fn.Caps.Emit) == 0 {
		l.errf("%s: %s is required and non-empty — the allowlist of types the effects may address", where, fn.capPath("emit"))
		return nil
	}
	for i, cv := range mslice(caps, "call") {
		ident := fmt.Sprint(cv)
		if !Qualified(ident) || strings.Contains(ident, "*") {
			l.errf("%s: %s[%d]: %q — call names full function identities, no globs", where, fn.capPath("call"), i, ident)
			continue
		}
		fn.Caps.Call = append(fn.Caps.Call, ident)
	}
	for i, nv := range mslice(caps, "network") {
		pat := fmt.Sprint(nv)
		if pat == "" {
			l.errf("%s: %s[%d]: empty pattern", where, fn.capPath("network"), i)
			continue
		}
		fn.Caps.Network = append(fn.Caps.Network, pat)
	}
	for i, mv := range mslice(caps, "mutations") {
		m := fmt.Sprint(mv)
		if !functionMutations[m] {
			l.errf("%s: %s[%d]: %q — merge and split are the gated mutations; put/patch/delete/link/unlink ride emit alone", where, fn.capPath("mutations"), i, m)
			continue
		}
		fn.Caps.Mutations = append(fn.Caps.Mutations, m)
	}
	if !l.parseReads(where, fn.capPath("reads"), caps, fn) {
		return nil
	}
	return fn
}

// parseReads reads the optional read capability off an envelope, at the path
// the envelope was authored at: a function's is a capability key, an agent's is
// `data.reads` beside its tools.
func (l *loader) parseReads(where, path string, caps map[string]any, fn *Function) bool {
	rv, has := caps["reads"]
	if !has {
		return true
	}
	r := asMap(rv)
	l.checkKeys(where+": "+path, r, functionReadsKeys)
	reads := &FunctionReads{}
	for i, tv := range mslice(r, "kinds") {
		t := fmt.Sprint(tv)
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
// emit kind and read kind must exist, and every call target must be a
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
			problems = append(problems, fmt.Sprintf("%s: %s: unknown type %q", where, f.capPath("emit"), t))
			continue
		}
		f.Caps.Emit[i] = ty.Identity
	}
	if f.Caps.Reads != nil {
		for i, t := range f.Caps.Reads.Kinds {
			ty, err := r.Resolve(t)
			if err != nil || ty == nil {
				problems = append(problems, fmt.Sprintf("%s: %s.kinds: unknown type %q", where, f.capPath("reads"), t))
				continue
			}
			f.Caps.Reads.Kinds[i] = ty.Identity
		}
	}
	for _, ident := range f.Caps.Call {
		if _, err := r.ResolveFunction(ident); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %s: unknown function %q", where, f.capPath("call"), ident))
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

// ResolveFunction accepts a full identity or a bare name unique across authorities.
func (r *Registry) ResolveFunction(nameOrIdentity string) (*Function, error) {
	var cands []*Function
	for _, f := range r.Functions() {
		if f.Identity() == nameOrIdentity {
			return f, nil
		}
		if f.Name == nameOrIdentity {
			cands = append(cands, f)
		}
	}
	switch len(cands) {
	case 0:
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
