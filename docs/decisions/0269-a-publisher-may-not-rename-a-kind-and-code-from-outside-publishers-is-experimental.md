---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0269. A publisher may not rename a kind, and code from outside publishers is experimental

## Context and Problem Statement

[0078](0078-a-kind-move-is-ordinary-record-writes.md) moves a kind's records
when a declaration names the old kind in `movedFrom`, and honors it only on
the boot upgrade of the packages the binary ships; every other door stores
the key and refuses the move (`internal/engine/move.go`). Whether a
publisher may rename a kind was never written down, the refusal said only
that the key "is honored for the shipped vocabulary only in this build",
and `substratectl publish --check` let a release that declared one through
with `--allow-narrowing`. Separately, the code an outside publisher ships
runs under review and confinement that are new
(`internal/engine/outsidecode.go`). The finding is F11, point 4, with rows
41 and 42, in [pull request 1313](https://github.com/geoah/substrate/pull/1313).

## Considered Options

- Let a publisher rename a kind through `movedFrom`, moving each
  repository's records at its upgrade.
- Refuse a rename at every door a published package comes through, and say
  what to do instead.

## Decision Outcome

Chosen: refuse the rename, because other repositories hold records of a
published kind under its name, and a rename one repository takes on its next
refresh breaks every record, reference and app that names the kind in all
the others.

What this sets:

- **A kind's name is permanent once published.** A publisher keeps the old
  kind, marks it `deprecated: true`, and declares the new one beside it.
- **Every door but the boot upgrade of the shipped packages refuses a
  move**: the apply door, a catalog install, import and upgrade, a
  publisher's refresh and an app's approval, all through
  `stageVocabularyBatch`. The refusal names both kinds and says what to do
  instead. A publisher's refresh holds a release that adds a kind
  declaring `movedFrom` for review, whether or not the repository holds the
  kind it names. Where it does not, an install the owner approves stores
  the key and moves nothing.
- **`substratectl publish --check` refuses a release that declares
  `movedFrom`**, with or without `--allow-narrowing`.
- **Code from outside publishers is experimental at 1.0**, whatever its
  package's label: the functions of a package installed from another
  authority's document. No wire field carries this; `publishedFrom` on the
  package record marks the code it covers.

### Consequences

- Good, because a record of a published kind keeps its name in every
  repository, and an author learns at the publisher's own check that a
  rename is not the way.
- Bad, because a publisher that chose a poor name carries the deprecated
  kind for as long as anybody holds records of it.
- Bad, because a package that requires another is not checked when that
  requirement changes or is uninstalled.
  `TestADependentPackageIsNotCheckedWhenItsRequirementChanges` records
  that both are admitted and the dependent is quarantined at the next open.
  That is a defect this record does not fix.

### Confirmation

`TestARenameIsRefusedAtThePublishersDoor` (`internal/catalog`) installs a
published package, lists a version that renames its kind, and finds the
refresh held for review, the upgrade preview blocked and the install
refused. `TestARenamingReleaseIsNeverAdditive` (`internal/catalog`) holds
the refresh's comparison, `TestKindMoveIsRefusedAtTheApplyDoor`
(`internal/engine`) the apply door, and `TestPublishCheckRefusesARename`
(`cmd/substratectl/commands`) the publisher's check.

## More Information

[Vocabulary evolution](../vocabulary.md#vocabulary-evolution-and-the-dialect-contract)
describes `movedFrom`, and [stability](../stability.md#a-packages-code) the
label on outside code.
