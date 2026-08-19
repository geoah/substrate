---
status: accepted
date: 2026-08-19
decision-makers: George Antoniadis
---

# 0042. Every kind carries an authority; the URL disambiguates by segment count

## Context and Problem Statement

Two grammars still admitted an authority-less kind before v1 froze. A stored
kind could be a bare name (`task`), the REST surface had authority-less shapes
(`/api/v1/{kind}` and `/api/v1/{kind}/{id}`), and a stored `reference` value
could be a one-slash `kind/id` path. All three were told apart from the
qualified form by inspecting the first segment for a dot: an authority is a DNS
name and carries a dot, a kind name never does ([0033](0033-the-path-grammar-has-no-separators.md)).

No door still admits an authority-less kind: every declaration names an
authority, and every stored `record_kind` is already fully qualified. The dot
rule and the authority-less shapes are dead weight that a client and every path
reader still carry, and the dot rule is the one thing standing between the URL
grammar and moving kind identity to URLs
([0014](0014-authorities-widen-only-outside-the-id-alphabet.md)).

## Considered Options

- Keep the dot rule and the authority-less shapes, inert.
- Remove the authority-less kind: make the authority mandatory in the stored
  identity, the URL and the stored reference value, and tell a collection from
  a record by segment count.

## Decision Outcome

Chosen: every kind carries an authority.

1. **The stored kind identity is always `{authority}/{name}`.** There is no
   bare stored kind. `Record.Kind` is qualified on every row.

2. **A REST path disambiguates by SEGMENT COUNT, not by a dot.** After the
   version prefix, `{authority}/{kind}` (two segments) is a collection and
   `{authority}/{kind}/{id}` (three) is a record. `addressed`
   (`internal/api/rest.go`) reads the count and nothing else; a one-segment
   path names no kind and answers `404`. The reserved top-level words and the
   record sub-resources (`incoming`, `edges`) are unchanged, and a record whose
   id would collide with a sub-resource word stays reserved in both directions
   (`reservedRecordID`, [0033](0033-the-path-grammar-has-no-separators.md)).

3. **A stored reference value is always `{authority}/{kind}/{id}`.** The
   polymorphic (unpinned) reference value requires the qualified form;
   `SplitRecordPath` refuses a dotless first segment.

What does NOT change: the **bare-name resolve-against-declaring-authority
shorthand**. An edge `to:` target, a trait pin, a callable ref, a glob selector
and a keyed-map `kindRef` key may still be written bare and resolve at load to
`<declaring-authority>/<name>`. That sugar produces a qualified identity; it
never yields an authority-less kind, so it is untouched. A **pinned reference
still accepts a bare id** completed from the pin.

This amends rule 1 of [0033](0033-the-path-grammar-has-no-separators.md) (the
`{kind}/{id}` repository-local arm of the path grammar); 0033 otherwise stands.
It does not do the authority-grammar widening reserved by
[0014](0014-authorities-widen-only-outside-the-id-alphabet.md).

### Consequences

- Good: the URL grammar and the reference grammar stop inspecting a segment;
  segment count carries the whole rule, and the dot rule is gone.
- Good: `substrate.reamde.dev` aside, nothing in the path grammar now assumes a
  bare kind, which unblocks moving kind identity to URLs later (0014).
- Bad: a path that was a valid authority-less shape now answers `404` (or `405`
  on a wrong method). This is one breaking change (`refactor(api)!`), taken
  before v1 so no shipped client is owed a migration.
- Neutral follow-on: giving a repository its OWN authority, derived from its
  signing public key, so a user's own kinds have a home authority, is tracked
  as [#285](https://github.com/geoah/substrate/issues/285) (P0, v1.0.0). It is
  not this change.

### Confirmation

`internal/api/grammar_test.go` asserts a one-segment path answers `404` on
every method, a record path is three segments, and the reserved-id corner still
refuses both directions. `internal/vocabulary`'s `TestSplitRecordPath` asserts
a dotless first segment is no stored path. The engine reference and bundle
suites, and `TestURLHarvesterBundleConformance`, install real closures with
bare `to:` and bare selectors, so the shorthand's survival is under test.

## More Information

Relates to [0033](0033-the-path-grammar-has-no-separators.md) and
[0014](0014-authorities-widen-only-outside-the-id-alphabet.md).
