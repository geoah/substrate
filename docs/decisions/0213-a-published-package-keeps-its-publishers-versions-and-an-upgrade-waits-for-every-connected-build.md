---
status: proposed
date: 2026-11-03
decision-makers: George Antoniadis
---

# 0213. A published package keeps its publisher's versions, and an upgrade waits for every connected build

## Context and Problem Statement

A publisher can release a vocabulary change while an older connected app
still depends on a declaration it removes. Server-maintained declaration
versions alone do not say which publisher release or app build is held.

## Considered Options

- Require an owner click for every release without tracking builds.
- Always install the newest release.
- Refresh additive vocabulary automatically and hold narrowing releases
  for connected builds.

## Decision Outcome

Chosen: managed token properties record each app build's package floors,
software version and last check-in. The calling token alone may record a
check-in. `appPackages` is a keyed integer property: a JSON object on the wire,
with package ids validated in Go and positive integer floors. Software versions contain
at most 64 printable characters. A check-in writes immediately when the
package floors or software version change; otherwise it writes only when
the stored check-in is at least a day old (or absent). The timestamp has
daily granularity: the hold depends on a live token and its floor, not on
the minute it last checked in.

A release narrows when it removes or renames declarations, properties or
enumerated values. Every live token recording a floor below that release
holds it back. Offers name those apps, their client ids, versions, floors
and last check-ins. Expired and revoked tokens do not hold releases.

A refresh tries versions newest first. Kinds-only additive changes apply as
the bundle; execution changes and other changes wait for the owner.
The runner starts a minute after boot and repeats every 24 hours, processing
repositories sequentially. A publisher lookup failure leaves the installed
package intact and appears on its upgrade offer.

`substratectl publish --check` checks the candidate against a local or live
authority document, rejects a changed published digest or a lower version,
and refuses narrowing unless `--allow-narrowing` is explicit. It prints the
version entry; publishing the files remains the publisher's action.

### Consequences

- New releases cannot silently remove vocabulary an older connected build
  still asks for.
- A connected app that never checks in again can hold a release until its
  token expires or the owner revokes it.
- Conservative comparisons can leave harmless edits for owner review.
- Publisher versions remain distinct from the engine's declaration versions.
- The held version's closure is read again from the publisher's document
  whenever the package is planned, previewed or upgraded, so a publisher
  keeps every version it published listed. A held version the document no
  longer lists blocks the package's upgrades, saying so, until the owner
  takes the package over; keeping each installed closure in the repository
  instead is later work.
- A refresh follows the document named on the package row, and a document
  that moved (the record now names another one) holds the upgrade for the
  owner rather than following it.

### Confirmation

`TestPublishedComparisonAndHolders`,
`TestPublishedInstallRefreshTakeover` and
`TestPublishCheckPrintsEntryAndRefusesMutations`.
