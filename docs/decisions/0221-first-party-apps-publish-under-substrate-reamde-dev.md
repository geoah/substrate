---
status: proposed
date: 2026-11-03
decision-makers: George Antoniadis
---

# 0221. First-party apps publish under substrate.reamde.dev, and the binary wins for what it ships

## Context and Problem Statement

The first-party apps (the Console app, Sync, Chat and Workouts) were named
by wherever their client documents were hosted: the release bucket's host,
`s3.rxtx.dev`, a shared endpoint the publisher does not own. Under
[0209](0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md)
an app is verified only through its authority's `_substrate` record and
document, and under
[0212](0212-a-package-is-published-by-its-authoritys-document-and-installed-from-it.md)
a package an app needs is installed from its publisher's document. The
apps need a publisher of their own, and the server must not let whoever can
change a hosted file replace what the binary ships under the same name.
This is the plan's section "First-party apps publish under
substrate.reamde.dev" ([the domains plan](../plans/authorities.md)).

## Considered Options

- Keep the release bucket's host as the apps' publisher.
- A new authority for the apps (`apps.reamde.dev`).
- `substrate.reamde.dev`, the authority core and the first-party catalogues
  already use, served as a static site.

## Decision Outcome

Chosen: `substrate.reamde.dev`. It is already the name every first-party
package carries, so an app and the packages it brings read as one
publisher, and `app:substrate.reamde.dev` is the actor of all four apps.
A new authority would split one publisher into two names; the bucket's
host is a shared endpoint, which 0209 never verifies.

- **The record and the site.** `_substrate.substrate.reamde.dev` carries
  `v=substrate1 doc=https://substrate.reamde.dev/.well-known/substrate/authority.json`.
  The site serves the authority document, Apple's association file, each
  app's client document at `/apps/<app>/client.json` and its claimed
  callback at `/apps/<app>/callback`, and the closures of the packages the
  apps bring at `/packages/<package>-<version>.yaml`. `mise run apple:site`
  writes it; the release builds themselves stay in their own folder.
- **The binary wins.** A package the binary ships (`core`, `llm`, `console`,
  `observations` and every catalogue under `substrate.reamde.dev`, and every
  package under `providers.substrate.reamde.dev` and
  `samples.substrate.reamde.dev`) is never installed or upgraded from the
  fetched document, so the hosted file can add packages the binary does not
  ship (`training`, `trainingcoach`) and list apps, and nothing else.
- **The placeholder sentence.** `substrate.reamde.dev` stops being a
  placeholder in this repository's guide: kind identity is unchanged (the
  authority is a DNS name), but the name is now a served domain whose
  document other servers read.

### Consequences

- Good, because the approval page names one publisher for every
  first-party app, proven by DNS the publisher controls.
- Good, because a package the apps bring has the same name in every
  repository (`substrate.reamde.dev/training/set`).
- Bad, because the four apps share one actor; the token still names the
  exact client.
- Bad, because the site is one more thing to keep up: a lapsed record makes
  every first-party app unverified at its next approval, and stops the
  published packages from upgrading.

### Confirmation

`internal/catalog`'s tests hold that a document never replaces a shipped
package; `.mise/apple.sh site` builds the site and keeps every published
version; the apps' release builds claim their callbacks on the site's host
(apps/apple/project.yml, the `-site` entitlements).
