# Typed core declarations: the target schemas

*Predates decision 0044: edges have since been replaced by reference
properties. Written before decision record 0047, so kind references here are in
the old two-segment grammar (`{authority}/{name}`).*

Companion to [typed-core.md](typed-core.md). These are the settled property
declarations the flip writes into `kinds/core.substrate.reamde.dev/`. Sketches,
not verbatim YAML: descriptions, displayNames and doc comments are the
implementer's to write in the house voice. Rules that apply to every kind:

- `definition` is dropped everywhere. The properties ARE the declaration.
- `name` is dropped everywhere (id-derived; display templates use the derived
  `{localName}` token, never `{name}`, which stays a real property on
  llmprovider alone).
- `authority` stays authored on every declaration kind (an actor's id is the
  bare name, so it is not derivable; one uniform rule).
- `version` stays declared, marked `managed: true`: stamped when absent, a
  supplied mismatch refused. `source` and the quarantine/lifecycle properties
  are `managed: true` too.
- Identity SELECTOR lists are `{type: string, repeated: true}` with a
  `refersTo:` marker, never references. Single-valued record POINTERS are
  `reference` with a pinned `to:`.
- Every declaration and every touched authority bumps its version per the
  house rules; the sweep is one release.

## core/agent (v1alpha6)

```yaml
version:     {type: string, managed: true}
authority:   {type: string, required: true}
description: {type: string, required: true}
prompt:      {type: text, fts: false}          # required; never FTS-indexed
provider:    {type: reference, to: core.substrate.reamde.dev/llmprovider, required: true}
model:       {type: string, required: true}
params:      {type: object, fields: {temperature: float, maxTokens: {type: int, min: 1}}}
tools:                                          # exactly one of builtin/callable per entry
  type: object
  repeated: true
  fields:
    builtin:     {type: enum, values: [query, propose, graphql, mutate]}
    callable:    {type: string}                 # full function identity; refersTo function
    name:        {type: string}                 # alias, callables only
    description: {type: string}
agents:      {type: string, repeated: true, refersTo: agent}
budgets:
  type: object
  fields:
    maxTurns:        {type: int, min: 1, max: 64}
    maxToolCalls:    {type: int, min: 1, max: 256}
    deadlineSeconds: {type: int, min: 1, max: 600}
    depth:           {type: int, min: 1, max: 3}
emit:        {type: string, repeated: true, refersTo: kind}
reads:
  type: object
  fields:
    kinds:   {type: string, repeated: true, refersTo: kind}
    budgets: {type: object, fields: {calls: {type: int, min: 1, max: 1000}, rows: {type: int, min: 1, max: 10000}}}
subagentOnly: {type: bool}
```

The mirrors `functions`/`subagents` die with `definition`.

## core/function (next version)

```yaml
version/authority/description  # as agent
runtime:   {type: enum, values: [python, go]}   # host arrives in PR2, additive
source:    {type: text, fts: false}
timeoutMs: {type: int, min: 1, max: 60000}
arguments:                                       # replaces input: json
  type: object
  repeated: true
  fields:
    name:        {type: string, required: true}
    type:        {type: enum, values: [string, int, float, bool, enum, json]}
    repeated:    {type: bool}
    required:    {type: bool}
    description: {type: string}
    values:      {type: string, repeated: true}  # enum only
returns:   # same shape as arguments
emit:      {type: string, repeated: true, refersTo: kind}
reads:     # as agent's reads block
call:      {type: string, repeated: true, refersTo: function}
network:   {type: string, repeated: true}        # URL patterns, never references
mutations: {type: enum, repeated: true, values: [merge, split]}
```

The `capabilities:` wrapper dies (hoisted); `input`/`output` die (flat).
Stored old-form rows are translated by the rung: the wrapper unwraps, and a
stored recursive `input:`/`output:` schema converts to `arguments`/`returns`
when it is flat (all shipped ones are), else the argument list is one
`{name: input, type: json}` escape entry (none shipped needs this).

## core/bundle (next version)

```yaml
version/authority/description
inputs:     # stays a MAP in authored form, typed as keyed
  type: object
  keyed: true                                   # key = input name, camel
  fields:
    kind:        {type: reference, to: core.substrate.reamde.dev/kind}
    inject:      {type: enum, values: [functions]}
    description: {type: string}
installs:  {type: string, repeated: true, refersTo: kind}
requires:  {type: string, repeated: true, refersTo: kind}
modules:   {type: object, keyed: true, fields: {source: {type: text, fts: false}}}  # key = filename... see note
oauth2:
  type: object
  fields:
    clientInput:           {type: string}
    authorizationEndpoint: {type: url}
    tokenEndpoint:         {type: url}
    revocationEndpoint:    {type: url}
    emailEndpoint:         {type: url}
    emailProperty:         {type: string}
featureScopes: {type: object, keyed: true, fields: {scopes: {type: string, repeated: true}}}
    # hoisted out of oauth2 (two keyed levels in a row are refused); key = feature toggle
disabled/uninstalled/purging: {type: bool, managed: true}
```

Note on `modules`: the authored form today is filename to source-string. A
keyed STRING property (`{type: text, keyed: true, fts: false}`) is the closer
fit if the dialect admits keyed scalars (it does); prefer that over a
one-field object. The filename key contract is not camel; use the unrestricted
key contract and validate the filename in the loader as today.

## core/kind (the meta-kind)

```yaml
version:  {type: string, managed: true}
source:   {type: enum, values: [builtin, installed], managed: true}
authority: {type: string, required: true}
plural:   {type: string, required: true}
singular: {type: string, required: true}
description: {type: string}
displayTemplate: {type: string}
traits:   {type: string, repeated: true}        # "temporal(point: dueAt)" micro-syntax stays a string
indices:  {type: object, repeated: true, fields: {properties: {type: string, repeated: true}}}
edges:
  type: object
  keyed: true                                   # key = rel, camel
  fields: {to: string, many: bool, required: bool, ownerRef: bool,
           description: string, inverse: string, inverseDescription: string}
properties:
  type: object
  keyed: true                                   # key = property name, camel
  fields:
    type:        {type: string, required: true}
    description: {type: string}
    displayName: {type: string}
    repeated:    {type: bool}
    keyed:       {type: bool}
    keyPattern:  {type: enum, values: [camel, kindRef]}
    required:    {type: bool}
    managed:     {type: bool}
    embed:       {type: bool}
    fts:         {type: bool}
    pattern:     {type: string}
    min:         {type: float}
    max:         {type: float}
    writer:      {type: enum, values: [oauth, connector, owner]}
    renamedFrom: {type: string}
    to:          {type: string}
    refersTo:    {type: enum, values: [kind, function, agent, authority, provider]}
    inverse:     {type: string}
    inverseDescription: {type: string}
    values:      {type: object, repeated: true, fields: {value: {type: string, required: true}, label: string}}
    states:      {type: string, repeated: true}
    initial:     {type: string}
    transitions:
      type: object
      repeated: true
      fields:
        from: string
        to: string
        onEnter: string
        stamps: {type: string, keyed: true}     # stamp property -> "now"
    fields:                                      # an object property's fields: a SMALLER declared set
      type: object
      keyed: true
      fields:
        type: {type: string, required: true}
        description: string
        displayName: string
        repeated: bool
        keyed: bool
        keyPattern: {type: enum, values: [camel, kindRef]}
        required: bool
        pattern: string
        min: float
        max: float
        to: string
        refersTo: {type: enum, values: [kind, function, agent, authority, provider]}
        values: {type: object, repeated: true, fields: {value: {type: string, required: true}, label: string}}
```

Depth check: `properties{}.transitions[].stamps{}` and
`properties{}.fields{}.values[]` both sit at 3, inside the widened cap of 4.
The nested `fields` set is deliberately smaller than a property's (no writer,
no fts/embed, no inverse pair, no states) because the loader refuses those at
field level; the declaration says so.

## core/trait

```yaml
version/source/authority/description  # as kind
properties: {type: string, keyed: true}          # name -> datatype word
oneOf:
  type: object
  repeated: true                                  # the variant LIST form; the map form died
  fields:
    name:       {type: string, required: true}
    properties: {type: string, keyed: true}
```

`kinds/core.substrate.reamde.dev/core.yaml`'s temporal trait is the one
authored document that changes shape (map to list).

## core/propertytype

```yaml
version/source/authority/description
base:    {type: enum, values: [<the scalar built-in datatypes>]}
pattern: {type: string}
min/max: {type: float}
values:  {type: object, repeated: true, fields: {value: {type: string, required: true}, label: string}}
```

## core/recordmapping

```yaml
version/source/authority/description
from:  {type: reference, to: core.substrate.reamde.dev/kind}
to:    {type: reference, to: core.substrate.reamde.dev/kind}
edge:  {type: string}
match: {type: object, repeated: true, fields: {from: string, to: string}}
map:   {type: object, keyed: true, fields: {path: {type: string, required: true}, merge: {type: enum, values: [atomic, union]}}}
```

The bare-string map value dies; the rung wraps stored ones as `{path: <s>}`.

## The rest of core (previously untyped json, per the settled design)

- `trigger.source`: an object with `kind: {type: enum, values: [record, schedule, webhook]}`
  as the discriminator and one optional object field per arm (the engine's
  existing arm-exclusivity check stays the semantic gate).
- `run.effects`: `{type: int, keyed: true}`, keys held to the action set by
  the engine as today.
- `llmmessage.toolCalls`: `{type: object, repeated: true, fields: {id: string,
  name: string, arguments: {type: text, fts: false}}}` (the arguments carried
  as the verbatim wire string).
- `recordpatchrequest.diff`: STAYS `json` this release (the value's shape is
  the target kind's business; admission validates it against the target,
  which is stronger than any structural schema). Same for the user-kind value
  leaves in recordmerge.moved / recordsplit.result / recordmergerequest
  evidence; type their containers only where it is free, else leave whole.
- `authority.actors`: keep its current shape, typed as far as it goes
  (repeated string), plus `quarantined`/`quarantineReason` marked managed.
- Kinds already fully typed (token, credential, blob, repository, llmprovider,
  llmthread, recoverykey, actor, recordmerge, recordsplit,
  recordmergerequest): only the cross-cutting rules apply (drop name where
  projected, managed markers, displayTemplate token change where it used
  {name} for the id-derived value).
