---
status: accepted
date: 2026-09-24
decision-makers: George Antoniadis
---

# 0101. A kind, trait or callable is named in full on every surface

## Context and Problem Statement

Record [0098](0098-a-declaration-names-a-kind-or-trait-in-full.md) removed
the bare-name shorthand from declarations and left the read-time surfaces
with it: a records filter's `kinds`, a put's `kind`, the change feed, a
trigger's or a policy's selector, `filter.implements`, the function and
agent call routes, `substratectl get task`, and a function body's
`host.records.list(["task"])`. Each resolved a bare word to the one kind,
trait, function or agent anywhere carrying it, and refused as ambiguous when
two did. That is the same search 0098 rejected, one door over: the same
shipped closure, the same client script and the same function body meant
different things in different repositories, and a repository that gained a
second `person` broke every reader that had spelled the first one bare.

## Considered Options

- Keep the read-time shorthand: a convenience for a human at a terminal.
- Refuse a bare word on every server surface and in the SDK, and have the
  CLI complete a bare word client-side before the wire.
- Refuse a bare word everywhere, the CLI included, with every refusal
  naming the full spellings the repository declares under the word.

## Decision Outcome

Chosen: refuse everywhere. `Registry.Resolve`, `ResolveFunction`,
`ResolveAgent` and `ImplementingStrict` take a full identity and refuse a
bare word with the same refusal a declaration gets: the word, the shape
`<authority>/<package>/<name>`, and every identity the repository declares
under it. A trigger's `source.record.kinds` refuses a bare entry at
admission rather than admitting a selector that would never fire. The
function host refuses a bare kind before the call leaves the body.
`substratectl get`, `patch`, `delete` and `search --kinds` take the full
reference and lose `--package`; a bare argument is refused with the
spellings the registry lists. The API routes that take a function or agent
name take the full identity, percent-encoded.

It beat the client-side completion because the CLI is one client of many,
and a shorthand that works in one client and is refused by the wire is a
second grammar. It beat keeping the shorthand because a name that means one
thing everywhere is the only kind of name a script, a body or a closure can
carry, and the refusal hands the reader the exact string to paste.

What stays bare is not a kind: a record id under a pinned reference
([0042](0042-every-kind-carries-an-authority.md)), a bundle's `installs`
entries' package word, a trait's variant, and the Go-side `Kind.Implements`
helper the engine calls with its own constants.

### Consequences

- Good, because every surface agrees on what a name is, and a repository
  that gains a second kind under a word breaks no reader of the first.
- Good, because every refusal is a copy away from the fix.
- Bad, because every command a person types carries the authority and the
  package, and every existing script, function body and agent tool call
  that spelled a kind bare is refused until it is rewritten.
- Bad, because the shipped agents' tools now depend on the model spelling a
  kind in full from the refusal text; the tool descriptions say "kind
  reference" and the refusal names the spelling, and that is what a model
  has to work with.

### Confirmation

`internal/vocabulary`'s `TestResolveRefusesABareName` holds the four
lookups; `internal/engine`'s `TestABareKindIsRefusedOnEveryDoor` holds a
put, a list, a change feed, a search, a trigger selector and a policy
selector; `cmd/substratectl`'s CLI tests hold `get` and `apply`; the live
end-to-end suite runs every case with full references.

## More Information

Amends [0098](0098-a-declaration-names-a-kind-or-trait-in-full.md): its
read-time exception no longer holds; the rest stands. Rests on
[0042](0042-every-kind-carries-an-authority.md) and
[0047](0047-a-kind-lives-in-a-package.md). Reopen if a client wants a
bare-name convenience: it belongs in that client's own completion, never on
the wire.
