---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis (via the issue-109 agent session)
---

# 0014. Authorities may widen only with characters the id alphabet excludes

## Context and Problem Statement

`SplitRecordPath` parses a stored record path with no registry, and the whole
trick is two character facts: an authority always carries a dot and never a
slash, and a kind name carries neither
(internal/vocabulary/ref.go, naming.go). Moving kind identity to URLs
(AGENTS.md, [Kind identity](../../AGENTS.md#kind-identity)) would hand an
authority such as `github.com/geoah/vocab` a raw slash and break both facts,
and splitting on the last slash instead lands inside the id, whose alphabet
deliberately admits `/` (a declaration's id is itself a kind reference).
Changelogs are filling with paths written under today's rules, so the
character budget must be reserved before the freeze even though the URL move
stays future work
([issue #109](https://github.com/geoah/substrate/issues/109)).

## Considered Options

- Reserve the grammar boundary in this record and change no code
- Split record paths on the last slash so an authority may contain `/`
- Resolve paths against the registry instead of by grammar
- Do the URL move now and widen everything at once

## Decision Outcome

Chosen: reserve the boundary and change no code. The rule in one line:
characters inside the id alphabet are data wherever they appear, forever;
structure may only ever be spelled with characters that alphabet excludes.
Three reservations follow.

**The record id alphabet is frozen, and it never gains `%`.** The alphabet
stays exactly `reID` (naming.go): an alphanumeric first character, then
letters, digits, `.` `_` `~` `:` `@` `/` `-`, at most `MaxIDLen` long. `%`
is excluded for two reasons that must both keep holding. On the wire, an id
is a URL path segment and `%` is its escape, so a `%`-free alphabet is what
lets the API decode a path exactly once (api/rest.go `pathParam`). In
storage, a `%`-free alphabet means any `%` in a stored string can be given
meaning later without re-reading anything stored today: if ids could carry a
literal `%`, a stored id `github.com%2Fgeoah%2Fvocab` would already exist as
data, and a grammar that later spells an authority's slashes as `%2F` would
re-read it as structure.

**An authority may gain characters only from outside the id alphabet.** It
keeps its mandatory dot, it never gains a raw `/`, and a kind name never
gains a dot (those three facts are the split). A future URL authority is
therefore stored with its inner slashes spelled by a reserved character, and
`%` (percent-encoding, RFC 3986's own answer) is the designated candidate.
Any replacement must also come from outside the alphabet.

**First-label keying is a placeholder and must move before the grammar
widens.** Three things key on an authority's first DNS label alone: the
GraphQL prefix of an installed kind (`GraphQLName`, ref.go), the actors
(`bundle:<name>` via `BundleName`, `connector:<label>` via
`AuthorityActor`), and bundle-name uniqueness (`bundleNameProblems`,
load.go). Under URL authorities every `github.com/...` bundle shares the
label `github`: all but one is uninstallable, and two bundles sharing an
actor write as each other, which the trigger loop guard would read as its
own echo and silently drop. Each keying must move to the full authority or a
fixed-length hash of it before authorities widen, and no new code may key on
the first label.

The alternatives lose stored data or the property the data relies on.
Last-slash splitting fails on legal paths today:
`core.substrate.reamde.dev/kind/tasks.substrate.reamde.dev/task` is one
four-segment path whose last slash is inside the id. Registry-dependent
resolution gives up the property every consumer relies on, that a changelog
is readable without loading the vocabulary that wrote it. The URL move
itself is real design work (routing separator, reference splitting, the
validator) that AGENTS.md already says not to half-do.

### Consequences

- Good, because every path a changelog stores today stays parseable by
  inspection under any grammar this record permits.
- Good, because the URL move has a defined lane: encode structure outside
  the alphabet, migrate the three first-label keyings first.
- Bad, because a writer's provider key containing `%` must be re-encoded
  into the alphabet before it can be an id. This is already true and now
  cannot be relaxed.
- Bad, because a percent-spelled authority can never itself be a record id,
  while today a declaration's id is its kind reference. The URL move must
  give such declarations a different id shape (a hash or a minted id);
  admitting `%` into ids for that one case would reopen the double-decode
  hole for every id.
- Bad, because percent-encoded authorities double-encode on the wire (a
  client sends `%252F` for a stored `%2F`). If that proves too hostile, the
  replacement separator still has to come from outside the alphabet.
- Bad, because the three first-label keyings stay live and wrong-shaped
  until the move, and nothing but this record forces the migration.

### Confirmation

`TestSplitRecordPath` and `TestKindGrammarSeparatesAuthorityFromName`
(internal/vocabulary/recordpath_test.go) pin the split; `TestValidID`
(internal/vocabulary/vocabulary_test.go) refuses `a%2Fb`, holding `%` out of
ids. The reservation itself, which characters a future grammar may claim, is
held by review only.

## More Information

Reopen trigger: the URL move. Its design record must fit this budget or
supersede this record, and it inherits the migration obligation on the three
first-label keyings either way.
