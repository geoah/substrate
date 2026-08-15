---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0002. Declaration versions are integers the API maintains

## Context and Problem Statement

This record was written retrospectively, from commit `33d86d6`
(`feat(core)!: declaration versions are incremental integers the API
maintains`, PR #63, closing issue #48) and the code that landed with it. It
reconstructs the reasoning from that evidence and nothing else: no discussion
is quoted, because none is recoverable.

Every document under `kinds/` projects with a `version`, and the boot upgrade,
the catalog's upgrade preview and the console's upgrade offer all key on it.
Before this change that version was a Kubernetes-style string, `v1alpha3` and
kin, ordered `v1alpha1` before `v1beta1` before `v1` before `v2` with a string
fallback, and every door that wrote a declaration expected a hand to have
written it. Issue #48 put both halves of the problem in two lines: the string
"really makes no sense" as a version for a record, and the API and the console
"should not allow the user to manually specify, should just increment".

Hand-written versions fail in a way nobody sees. A definition that changes
under an unmoved version is an upgrade no repository ever receives, silently,
and the person who forgot the bump is the person least placed to notice.

## Considered Options

- Keep the Kubernetes-style version string, written by hand at every door.
- Move to an incremental integer, still written by hand at every door.
- Move to an incremental integer that the API maintains, while the shipped
  tree under `kinds/` continues to pin versions explicitly.

## Decision Outcome

Chosen: an incremental integer the API maintains, with the shipped tree still
pinning explicitly.

A declaration's version is an `int64`: 1, 2, 3, and zero is never a version
but the ABSENT value, ordering below everything so a row written before
versions were mandatory upgrades on the next open rather than sticking
(`internal/vocabulary/version.go`). Through the API an incoming version is
honored only when it moves PAST the stored one; absent, echoed or lower
resolves server-side, so a changed definition lands at stored+1, an unchanged
one keeps its stored version, and a new one rides its authority's
(`resolveDeclarationVersions` in `internal/engine/vocabularywrite.go`). A
declaration that cannot pin a version of its own, a trait or a function or
anything but a kind, moves its AUTHORITY forward when it changes, and so does
a delete, so a prune reads as an upgrade.

The tree keeps its explicit pins because the boot upgrade needs one total
order across binaries: two servers on two versions of this repository have to
agree which shipped vocabulary is newer, and stored+1 cannot tell them.
`mise run kinds:check` is what holds that second door, diffing `kinds/`
against the merge base through the one comparator.

### Consequences

- Good, because `get -o yaml | apply -f` is a no-op: an unchanged re-apply
  keeps the stored version, which the API side could not promise while a hand
  had to supply the number.
- Good, because ordering is integer comparison in one function,
  `vocabulary.CompareVersions`, and the boot upgrade, the upgrade preview and
  `cmd/vocabularydiff` all diff through it, so "newer" means one thing
  everywhere. The string ordering it replaced had a fallback, which is another
  way of saying it had cases nobody had decided.
- Good, because the version property is declared `type: int` and
  `managed: true` on the declaration kinds, so the retired spelling cannot
  come back through a document and a hand-typed value never reaches the wire.
- Bad, because it was a break. The property retyped from string to int, so
  migration `0004_declaration_version_int.up.sql` had to backfill stored rows
  (`v1alpha3` to 3, a digit string to its number, anything else to 1), and the
  narrowing guard's retype count had to become value-based for the retype to
  land over the backfilled numbers instead of being refused.
- Bad, because the changelog is append-only and is not rewritten, so a rebuild
  refolds the old strings into `records`, where they read as the absent
  version 0 until the next open's shipped-vocabulary upgrade rewrites them as
  numbers.
- Bad, because there are two doors and a contributor has to know which one
  they are at: through the API nobody bumps by hand, in this tree everybody
  does. `AGENTS.md` states both, and `kinds:check` catches the tree half.

### Confirmation

- `TestApplyMaintainsDeclarationVersions`
  (`internal/engine/versionresolve_db_test.go`) holds the API half: a first
  declaration is 1, an unchanged re-apply keeps the stored version, a changed
  one lands at stored+1.
- `mise run kinds:check` (`.mise/kindscheck.sh`, over `cmd/vocabularydiff`)
  holds the tree half and refuses the merge otherwise.
- `vocabulary.VersionValue` reads an int, an int64 and a whole float64 and
  nothing else, so a string version is not a version.

## More Information

- The house rule this became: "A changed declaration ships a changed version"
  in [AGENTS.md](../../AGENTS.md).
- What a reader is told: [vocabulary.md](../vocabulary.md).
- Revisit this if a declaration version ever has to carry a compatibility
  statement rather than an order. An integer can say which came later; it
  cannot say which two are compatible.
