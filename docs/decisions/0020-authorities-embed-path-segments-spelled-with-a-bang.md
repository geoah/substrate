---
status: proposed
date: 2026-08-16
decision-makers: George Antoniadis
---

# 0020. An authority spells its path segments with `!`

## Context and Problem Statement

[Issue #194](https://github.com/geoah/substrate/issues/194) asks whether an
authority may contain path segments. Two wants drive it: publishing kinds from
a URL nobody owns DNS for (`github.com/geoah/vocab`), and grouping one
domain's vocabularies by path instead of by subdomain. The shipped tree
carries 22 sibling authorities (`tasks.substrate.reamde.dev`,
`firecrawl.bundles.substrate.reamde.dev`, ...) because DNS labels are the only
hierarchy the grammar has.

Identity has two levels. A kind is an authority plus a name. A record is a
kind plus an id, and an id may embed the record's own publisher: a
declaration's id is a kind reference, so one flat string can carry two
authorities:

```
core.substrate.reamde.dev/function/firecrawl.bundles.substrate.reamde.dev/websearch
[kind authority          ][kind    ][record's own authority               ][record name]
```

The string splits with no registry because of two character facts: an
authority always carries a dot and never a slash, and a kind name carries
neither. Reference values and declaration ids store such strings, the
changelog hashes their payload bytes as stored, the split is re-implemented
outside the engine (the console, the generated decoders), and the kind and id
grammars are copied further still (both function SDKs, a Postgres regex). A
string in which the authorities, the kind name and the id all spell hierarchy
with `/` cannot be split without a registry, so some character must mark the
structure; the open choices are which character and where it sits.

The constraint is not REST. The API percent-decodes a path segment exactly
once (api/rest.go `pathParam`), so an escaped authority already routes, and
GraphQL carries kind identity as a string argument, never in a path.
[0014](0014-authorities-widen-only-outside-the-id-alphabet.md)
reserved the budget (structure may only be spelled with characters the id
alphabet excludes) and named this move its reopen trigger. Pre-v1 is the
cheap moment: no client outside this tree is known to have copied the split.

## Considered Options

- Reaffirm dotted authorities and close #194 with no change
- Percent-encode an authority's inner slashes (`github.com%2Fgeoah%2Fvocab`),
  0014's designated candidate
- Raw `/` inside the authority, with a reserved `!` before the kind name
  (`github.com/geoah/vocab!note`)
- Spell the authority's inner slashes with `!`
  (`github.com!geoah!vocab/note`), so the record-path split never changes
- Drop the kind from a reference, so a record id addresses a record alone

## Decision Outcome

Chosen: spell the authority's inner slashes with `!`. An authority becomes a
DNS host optionally followed by `!`-joined path segments, each under the
label grammar; the URL it names is the same string with `!` read as `/`. The
kind reference and record path grammars keep their shapes,
`<authority>/<name>` and `<kind>/<id>`, and their split: a widened authority
still carries a dot and still carries no slash, so every stored path, old or
widened, parses by the split logic that exists today.

```
URL of the publisher      github.com/geoah/vocab
authority (stored form)   github.com!geoah!vocab
kind reference            github.com!geoah!vocab/note
a record's path           github.com!geoah!vocab/note/n1
its declaration's path    core.substrate.reamde.dev/kind/github.com!geoah!vocab/note
```

The record id alphabet gains `!`, its first character since the freeze, so a
declaration's id stays its kind reference, and an id that embeds the record's
own publisher may embed a widened one. None of the machinery built on that
fact (pin completion, `ReferentID`, titles from the id's last segment) grows
a second case, and the split never leans on `!` being absent from ids:
segments are cut by `/` first, and `!` marks structure only inside an
authority. `%` stays excluded, so the single-decode rule holds.

Why `!`: the separator must come from outside today's id alphabet, be legal
raw in a URL path segment, and stay inert where the grammar travels. Of the
legal set, `,` splits YAML flow sequences, `+` is a metacharacter in the two
regex dialects the grammar ships to (RE2 and Postgres), `;` and `$` and `&`
are shell structure, `*` is the type-glob wildcard. `!` is none of these
(interactive bash expands it inside double quotes; single quotes silence it),
and UUCP spelled paths through hosts with it first. The mapping to a URL is a
bijection only because a segment never contains `!`: the character is spent,
and a URL path segment carrying a literal `!` can never be an authority.
Uppercase or `_` in a segment cannot be spelled yet either, but those can be
admitted later without ambiguity.

Three fences bound the widening:

- **Length.** A declaration's id is `<authority>/<name>` and an id caps at
  `MaxIDLen` (128), so an authority too long for its declarations to store is
  refused when the authority is declared, not when its first kind fails. The
  implementing change picks the split of the budget between authority and
  name.
- **One spelling.** An authority without inner segments keeps its dotted
  form, and the validators refuse the `!` form where the dotted form is
  legal. The `/`-spelled URL is display and import syntax only, never a
  stored reference: `github.com/geoah/vocab/note/n1` already parses today
  (authority `github.com`, kind name `geoah`, id `vocab/note/n1`) and must
  keep meaning that, so a pasted `/` spelling is not detectable as a URL and
  keeps its old meaning. The surfaces that know they hold a URL (an import
  door, a console input) map `/` to `!` before the value enters a record;
  nothing else ever converts.
- **The dialect ladder.** The grammar widens behind the next vocabulary
  dialect rung (engine/dialect.go): a server binary that predates it refuses
  the repository at its next open, with a named reason, instead of failing
  later inside a fold whose validators refuse the authority. An already-open
  process is not fenced, and the ungated grammar copies (the console, the
  SDKs) simply refuse widened spellings until updated, so the rollout
  replaces binaries before the first widened authority is accepted.

What widens with the grammar is enumerable, and it is validators, never split
logic: the authority and id regexes (vocabulary/naming.go), their copies in
the console (record-schema.ts), the Python SDK (host.py), the Go SDK
(substratefn/substratefn.go) and the generated decoders (corekinds,
kindsgen), the keyed-map `kindRef` pattern that ships to Postgres
(engine/schemadiff.go), and the `kinds/` embed contract, whose
`all:*.substrate.reamde.dev` glob misses a widened directory name. The
procedural splits (vocabulary, the generated decoders, the console's
record-path, REST's `addressed()`) change nowhere; that asymmetry is the
point of this spelling.

The other options lose more:

- Reaffirming dots leaves both wants unmet, and this is the last cheap
  moment: the grammar can widen later only by breaking every client that has
  copied the split by then.
- Percent-encoding double-encodes on the wire (a client sends `%252F`), is
  unreadable everywhere, and still forces a new id story for declarations,
  since `%` may never enter ids. Its one edge, round-tripping a URL whose
  segments carry `!` or uppercase, buys nothing this tree needs.
- Raw `/` in the authority changes the split itself, so every implementation
  of it gains a second grammar, stored-data compatibility rests on `!` being
  new rather than on the split being unchanged, and REST loses the
  authority-in-one-segment fact it routes by.
- Id-only references give up the `(repository, kind, id)` identity that the
  records table and every record-scoped table key on, give up typed
  references, and reopen the settled flat `<kind>/<id>` reference value
  model.

### What each wire surface looks like

The widened authority is one raw path segment (`!` is a legal pchar), so
nothing needs escaping and no surface forces subdomains:

```
REST      GET /api/v1/github.com!geoah!vocab/notes/n1
POST-only POST /api/v1/get   {"kind": "github.com!geoah!vocab/note", "id": "n1"}
GraphQL   { record(kind: "github.com!geoah!vocab/note", id: "n1") { title } }
```

REST keeps working unmodified because `addressed()` (api/rest.go) tells an
authority by its dot, which the widened form keeps. The POST-only door does
not exist today; it is here to show that identity carried in a body never
cared about the grammar, and GraphQL is already that door. The GraphQL type
name for the example is whatever the migrated prefix rule yields; today's
first-label rule would name every `github.com!...` kind `Github_...`, which
is the collision the preconditions below exist to remove. Keeping or
dropping REST is a product choice this record does not make.

### Consequences

- Good, because publishing a vocabulary needs a URL, not a DNS zone.
- Good, because one domain can group by path: `substrate.reamde.dev!tasks`
  displays as `substrate.reamde.dev/tasks`. Regrouping the shipped tree is
  its own migration and is not decided here.
- Good, because no stored byte changes: the chain's preimages stand, no
  reseal, and the split logic is untouched everywhere it is spelled; only the
  validators enumerated above widen.
- Good, because an older server refuses the repository loudly at its next
  open (the dialect ladder) instead of failing mid-fold.
- Bad, because the stored form is not literally the URL: a surface owes the
  `!`/`/` mapping exactly where it knows the string is an authority or a kind
  reference, must never blanket-map ids (an id may carry `!` as plain data
  once the alphabet admits it), and cannot detect a pasted `/` spelling,
  which keeps its old meaning.
- Bad, because the id alphabet unfreezes by one character, which spends `!`
  forever and supersedes 0014's frozen-alphabet clause; future structure must
  come from what remains outside the alphabet, and a publisher whose URL path
  carries a literal `!` can never be an authority.
- Bad, because the first-label keyings 0014 flagged become blocking work:
  the GraphQL prefix, the `bundle:<name>` actor and bundle-name uniqueness,
  the `connector:<label>` actor, and a fourth 0014 missed, a callable's actor
  being its bare local name (vocabulary/function.go `Actor`), held unique
  today only by a shipped-tree test. Under widened authorities every
  `github.com!...` bundle shares the first label `github`. Each keying moves
  to a fixed-length hash of the full authority or to a declared name refused
  on collision at declaration time; the full authority itself cannot be the
  actor, because the actor grammar (which a metadata key's namespace half
  must equal) admits neither `.` nor `!`, and widening it is an unpriced
  second grammar change.
- Bad, because `!` in an interactive shell wants single quotes, and every
  example in the docs has to write them.

### Confirmation

When the code lands: `TestSplitRecordPath` and
`TestKindGrammarSeparatesAuthorityFromName` grow the `!` cases and keep every
existing case byte-identical; `TestValidID` accepts `!` and still refuses
`%`; the authority validators' cases pin the one-spelling rule and the length
budget; the grammar copies in the console, both SDKs and the generated
decoders widen in the same change, each behind its own test; a
widened-authority fixture under `kinds/` holds the embed glob; and the
dialect suite holds the refusal of an older binary. Until then: review only.

## More Information

On acceptance this record supersedes
[0014](0014-authorities-widen-only-outside-the-id-alphabet.md), whose other
reservations it restates and keeps: `%` never enters the id alphabet, no new
code keys on an authority's first label, and the keying migrations land
before the grammar widens. The remote install door (install a bundle by URL)
is enabled by this grammar but is its own work, as is any regrouping of the
shipped tree and any GraphQL-only or POST-only surface. Reopen triggers: a
consumer that needs the stored form to be byte-identical to the URL, or a
publisher whose URL path carries a literal `!`.
