# Domains: status

As of 2026-11-03. [The plan](../authorities.md) is implemented in five
stacked pull requests, none merged, and runs on `substrate.rxtx.dev`. This
page is the handoff: what is open, the merge order, what review found, what
runs where, and what is left. [What phases 2 and 3 build](phases-2-3.md) is
the design the code holds.

## Pull requests

Merge in this order; each is based on the one before it.

| PR | Branch | What |
| --- | --- | --- |
| #1163 | `feat/authorities` | phase 1: the `_substrate` record, **Verify domain**, verified app publishers, new packages only under a token's own authority (0208 to 0210) |
| #1164 | `feat/authorities-phase2` | phase 2: packages installed from an authority document through an app's approval, versions and build tracking, `declare:` and the `agent` detail retired (0212 to 0214) |
| #1165 | `feat/authorities-phase3` | phase 3: installed code held to the approved scopes, add-ons, agent providers through inputs, settings on the page, the wider scoped surface, outside code (0215 to 0220) |
| #1166 | `feat/authorities-apps` | the Apple apps on published packages and claimed callbacks; Workouts' training 5 and trainingcoach 3 (0221) |
| #1167 | `feat/authorities-review` | the second review's fixes across the stack |

Separately, #1162 fixes the `internal/e2e` cases (#1161).

## Decisions

0208, 0209 and 0210 are accepted. 0212 to 0221 are proposed and are
accepted when the stack merges; 0211 is #1158's. 0214 supersedes 0203, 0205
and 0207.

## Review record

- Phase 1: Fable and Astra reviewed each pull request; every finding was
  fixed and the fixes reviewed again.
- Phases 2, 3 and the apps: Fable (design, console and apps), Astra (server)
  and Astra (the approval integration) reviewed the integrated stack. Their
  findings were each checked against the code; the ones that still held are
  fixed in #1167 and, for the apps, in #1166. The findings that no longer
  held concerned code from before the approval work landed.

## Where it runs

- `substrate.rxtx.dev` runs `deploy/authorities-20261103` (91b4a51f),
  rolled 2026-11-03 03:19 UTC with the snapshot pair
  `substrate-workouts-review-013e2fa2f35d-20261103T0318Z`. It lacks two later
  server commits of the stack: the listing of a token with a retired scope
  (the one such token there was revoked instead) and a CodeQL-only
  allocation change.
- `substrate-v1-beta.rxtx.dev` runs the same image: a throwaway server for
  testing against real DNS.
- `substrate.reamde.dev` serves the authority document (training 4 and 5,
  trainingcoach 2 and 3), the four apps' client documents and callback
  pages, and the association file, published by `mise run apple:site`.
- geoah.me's Substrate Workouts records (2380) moved from `geoah.me/training`
  to `substrate.reamde.dev/training` 5, with trainingcoach 3 bound to its
  `litellm` provider row; the old packages are uninstalled, and a local
  backup of the records was kept beside the snapshot pair. geoah.me
  verified its domain through `_substrate.geoah.me` (2026-11-03 03:57 UTC);
  `healthspan-demo.example.com` cannot, since nobody publishes under
  `example.com`.
- The four Apple apps are released on the prerelease channel from #1167's
  head: Substrate, Substrate Sync, Substrate Chat and Substrate Workouts.
  Their Mac builds are signed for development, so they run on the Macs the
  team has registered.

## Choices to confirm

Made while implementing or fixing; the plan did not settle them.

- A verified app's redirect may sit off its `client_id`'s origin, on the
  authority or a name under it, relaxing 0194's origin rule.
- `SUBSTRATE_INSECURE_AUTHORITY_DOCS` and
  `SUBSTRATE_INSECURE_ALLOW_FOREIGN_PACKAGES` are not limited to
  development. The first is disclosed on the approval page and the token.
- A full-access app token may verify a domain, because 0204 makes `full` a
  sign-in.
- A token keeps the actor it was minted with until the app is approved
  again.
- The Apple apps confirm a negative DNS answer through libresolv, which adds
  up to about 4 seconds before a failed lookup shows its error.
- A published agent must name a provider input; a sample's agents may still
  name a provider.
- A deferred add-on whose agents take a provider through an input installs
  only where the input resolves on its own; the approval asks no provider
  for it.

## Left to do

- **The owner:** merge the stack in order and accept 0212 to 0221; on a
  physical iPhone, the claimed `https` callback, DNS-SD discovery over a
  VPN, encrypted DNS and iCloud Private Relay, and the Workouts upgrade from
  the last release; the macOS apps by hand.
- **Later:** keep each installed closure in the repository, so a publisher
  that stops listing a version strands nobody (0213); a malformed package
  entry leaves an authority's apps unverified (0212); the approval page
  offers provider rows that have no key.

## Briefs

[briefs/](briefs/) holds the work-package briefs phase 1's implementing
agents were given, as given.
