---
status: proposed
date: 2026-11-03
decision-makers: George Antoniadis
---

# 0214. An app's approval installs published packages, and `declare:` and the `agent` detail are retired

## Context and Problem Statement

An app that brings its own kinds had one way in: a `declare:~/<package>`
scope under which its token applied the package's declarations into the
repository's own authority ([0203](0203-an-app-keeps-its-own-package-under-a-declare-scope.md)),
a lock that made the package the app's by client id until the owner took it
over ([0205](0205-an-app-owns-the-package-it-declares.md)), and an `agent`
detail that approved the one agent the app could declare, held to a live
approval at every run ([0207](0207-an-app-declares-one-agent-held-to-its-own-reach.md)).
Each was the smallest change available while a server read packages from
its binary alone. Phase 2 of [the authorities plan](../plans/authorities.md)
gives a publisher an authority document that lists the packages it
publishes, and [the design](../plans/authorities/phases-2-3.md) makes the
catalog resolve a package id through the binary and those documents. With
that door open, the three workarounds cost more than they give: an app's
kinds differ per repository, a lock the owner cannot undo without taking
the package over, a token that applies vocabulary, and an agent loop that
reads token records at every turn.

## Considered Options

- Keep `declare:` and the `agent` detail beside the published door, so an
  app chooses.
- Retire both: an app's package arrives through a `package` detail like a
  shipped provider's, and its agent ships inside that package.
- Keep `declare:` for a loopback build with no document, and retire it for
  verified apps alone.

## Decision Outcome

Chosen: retire both. A `package` detail is the one way an approval brings
code or kinds, whatever ships them, so the owner reads one kind of row and
the server runs one install path. Keeping the old door beside the new one
would keep every rule 0205 and 0207 needed and give the owner two ways to
hold one package. A loopback exception keeps the lock and the scoped apply
for the one case nothing can verify, which is the case least worth them: a
developer points a local build at a development document instead.

The rules this binds:

- **A `package` detail resolves through the catalog**: the binary's closure
  where the binary ships the id, else the newest version the publisher's
  authority document lists at the detail's `version` or later, which stays
  the floor. A document-published row names its publisher (`publisher`:
  the authority, the document's name for it, the document's URL and whether
  a file this server reads stood in for DNS) and the version approving would
  install. Its requirements resolve the same way, each at the floor the
  chain puts under it, before what needs them. A package no source offers at
  the floor is the `not-published` block, carrying the catalog's words. A
  package the binary ships keeps the rows and blocks of
  [0202](0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md),
  and the set up installs both through the catalog's one install door.
- **`declare:` is retired.** A request naming it is refused at
  `/oauth/authorize` and at the set up check, pointing at a `package`
  detail; a stored token carrying it fails closed, so the app signs in
  again. No scoped token applies vocabulary, previews an apply or uninstalls
  a bundle. 0205's lock is gone: `authorizeAppOwnedWrite`, the dependency
  guard, the purge refusal and the console's "Managed by" views, and no
  path writes `declaredByClient`. `POST /api/v1/vocabulary/takeover` stays,
  rewriting the stamps of a row an earlier binary left naming an app; the
  catalog extends it to a document-published package.
- **The `agent` detail is retired.** The token's `agents` is no longer
  minted or read, and the engine's live-approval, priced-model, root-run and
  person's-entry checks of 0207 go with it: an agent of a published package
  runs as an agent of that package, bounded by its own declared reach and
  its own daily cap, until the owner pauses or uninstalls the package.
- **An app reaches an agent only when its own reads cover every kind the
  agent reads**, as 0207 held for an app's agent, now for every agent an
  `agents:` scope matches: each entry of the agent's `permissions.reads.kinds`
  is covered by a read or write scope at least as wide, an `internal` kind
  or one the repository lacks by its exact scope alone. An agent is no
  channel to data the caller could not read itself.
- **A check sees a package through any scope that reaches into it.** The
  set up check tells a token a package's versions where one of its scopes
  names the package: a read or write over its kinds, as 0202 had it, and
  now an `agents:` or `decide:` scope over its agents, so a package of
  agents alone (Workouts' coach) is not `unknown` to the app that talks to
  it.
- **A build says its version.** `/oauth/authorize` reads an optional
  `software_version` (RFC 7591's name), at most 64 printable characters;
  the token the approval mints records it with the floor of each package
  detail (`appSoftwareVersion`, `appPackages`), and the set up check takes
  `softwareVersion` in its body and records both on the calling token
  before it answers (`Dataset.RecordAppBuild`). A check that names a floor
  above the version the repository holds of a document-published package
  refreshes that package through the catalog first, so an upgrade the
  catalog applies on its own lands at once.
- **What an approval installed stays after the app is revoked**, as 0202
  says; the page says so on each row and names the package's page, where
  the owner pauses or removes it.

This amends 0202: a `package` detail is no longer "nothing outside the
binary's catalog", and the row's versions are what the catalog resolved.

### Consequences

- Good, because an app's kinds have one name everywhere
  (`substrate.reamde.dev/training/session`), published and versioned by the
  app's authority rather than rehomed per repository.
- Good, because the owner approves one kind of row, the server runs one
  install path, and no token ever applies vocabulary.
- Good, because the engine's agent loop knows nothing of apps again, and an
  app's agent is a package's agent like any other.
- Bad, because an app that shipped against 0203 or 0207 is refused until it
  publishes its package and asks for it with a `package` detail; a token it
  already holds with `declare:` stops authenticating.
- Bad, because a loopback build with no authority document cannot bring its
  own kinds until a development document stands in for one.
- Bad, because the row's version is read from the publisher's document at
  every plan, so a document that cannot be read blocks a re-authorization
  of a package the repository already holds, until it can be read again.
- Bad, because the upgrade preview of a document-published package the
  repository holds below the floor waits on the catalog resolving it: until
  it does, that row blocks as `upgrade-needs-review` rather than running.

### Confirmation

`internal/appsetup`: `TestADocumentPublishedPackageRowNamesItsPublisherAndWhatItBrings`,
`TestAPackageNothingPublishesAtTheFloorBlocksTheApproval`,
`TestAPublishedUpgradeIsPreviewedThroughTheCatalog`,
`TestWhatTheCatalogCannotInstallIsBlocked`. `internal/scope`:
`TestADeclareScopeIsRefusedInARequestAndFailsClosedWhenStored`.
`internal/authorize`: `TestAuthorizationDetailsAreReadStrictly` (the agent
detail), `TestASoftwareVersionIsBoundedAndPrintable`,
`TestPackageFloorsAreThePackageDetailsVersions`. `internal/api`:
`TestADeclareScopeAndAnAgentDetailAreRefusedAtTheDoor`,
`TestABuildsVersionAndFloorsRideTheToken`,
`TestACheckRecordsTheBuildAndRefreshesAPackageBelowItsFloor`,
`TestTheApprovalPageShowsADocumentPublishedPackage`,
`TestAnAppReachesAnAgentOnlyWhenItReadsEverythingTheAgentReads`,
`TestAnAppsTokenNeverAppliesVocabulary`. `internal/engine`:
`TestTheOwnerTakesOverAnAppsPackage` (the package every hand writes, the
stamps a takeover rewrites), `TestTheShippedCoachLoads`. The console's
`setup-row.published.test.tsx`, `scope-row.test.tsx`, `app-setup.test.ts`
and `packages.test.ts` hold the page.

## More Information

Supersedes 0203, 0205 and 0207. The design is in
[phases 2 and 3](../plans/authorities/phases-2-3.md); the app author's side
is in [apps](../apps.md#keep-a-package-of-your-own). Revisit when a loopback
build needs its own kinds without a document, or when an owner wants to
extend a published package without taking it over.
