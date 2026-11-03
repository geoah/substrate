---
status: proposed
date: 2026-11-03
decision-makers: George Antoniadis
---

# 0212. A package is published by its authority's document and installed from it

## Context and Problem Statement

An authority already publishes the apps it vouches for through its
`_substrate` TXT record. Packages outside the binary need the same
publisher proof and an immutable release, without allowing a repository to
claim another authority through a hand-written declaration.

## Considered Options

- Keep every published package in the server binary.
- Install any URL a client supplies.
- Resolve a package through its authority document and verify the listed
  closure's digest.

## Decision Outcome

Chosen: the authority document lists package names and immutable positive
integer versions, each with an HTTPS closure URL and SHA-256 digest. The
shared reader resolves DNS on every read, caches successful documents for
ten minutes, and bounds a closure to 4 MB. Unknown document keys are
ignored; known values are validated.

The binary wins for every package it ships. The provider and sample
authorities are binary-only. A repository never installs its own authority
from a document. The newest listed version at or above the requested floor
is resolved. Every declaration and data record must belong to the named
package; the package header supplies its version, as in shipped closures.

A published install stamps `publishedFrom`, `publishedDigest` and
`shippedVersion`. A changed digest at the held version and a downgrade are
refused inside admission. Requirements resolve through both sources.
Development documents may list relative closure paths confined to their
directory, including after resolving symlinks.

The owner can take over a document-published package. It becomes
`installed`, records its publisher in `takenOverFrom`, clears
`publishedFrom`, and stops following releases.

This amends [0048](0048-providers-are-published-samples-are-copied.md) and
[0202](0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md):
published packages may come from verified authority documents as well as
the binary. Neither record is superseded.

### Consequences

- Publishers release packages independently of server releases.
- A missing publisher document leaves installed declarations intact.
- A development file substitutes the operator's trust for DNS and is
  identified as such to the owner.
- The binary wins on a refresh too: a package a later binary ships is never
  upgraded from the document it was installed from.
- A document install adopts a package the repository holds without a
  publisher stamp (a copy declared by hand under another authority, which
  decision 0210 leaves possible only for data from before it): installing
  from the publisher's document is the owner's own act.
- A malformed package entry makes the whole document unreadable, which
  leaves the authority's apps unverified too, so a publisher checks a
  document with `substratectl publish --check` before serving it.
- The document's `apps[].packages` and `apple.appIDs` are for its readers;
  no server path reads them.

### Confirmation

`TestPublishedResolution`, `TestPublishedClosureBoundaries`,
`TestReaderFreshAndTolerant`, `TestAuthorityPackageValidation`,
`TestDevelopmentClosuresConfinedAndPinned` and
`TestPublishedInstallRefreshTakeover`.
