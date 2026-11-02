---
status: proposed
date: 2026-11-02
decision-makers: George Antoniadis
---

# 0205. An app owns the package it declares

## Context and Problem Statement

Under [0203](0203-an-app-keeps-its-own-package-under-a-declare-scope.md) an
app keeps a package under the repository's own authority, and the owner
edits it like any package of theirs. Substrate Workouts installs `~/training`
that way, and the app has to notice the owner's edits by comparing kind
versions and then ask before it applies over them. The owner decided on
2026-11-02 that this is backwards: "This kind should be managed by the
substrate workout authority, I should not be able to modify the kinds", and
"I should not be able to edit kinds that are not mine. The app owns these
kinds." This amends 0203: what the package is, who may change it, and how
the owner gets it back.

## Considered Options

For who owns the package:

- Leave it the owner's, as 0203 has it, and keep detecting edits in the app.
- Make it the app's, keyed on the `declaredBy` the package row already
  carries ([0109](0109-a-package-row-names-the-actor-that-declared-it.md)).
- Make it the app's, keyed on a new managed property written at the
  declare-scoped apply.

For the owner's way back:

- Unlock the package by itself when the owner revokes the app's tokens.
- Keep it locked until the owner deliberately takes it over, confirmed by
  typing the package's identity.
- Let the owner delete the package, its kinds and their records.

## Decision Outcome

Chosen: the package is the app's, keyed on `declaredBy`, and stays locked
until the owner takes it over. `declaredBy` is stamped by the engine at the
write that creates the package row, no document may write it, and an
`app:<host>` actor arises only from a scoped token
([0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)).
Of the scoped tokens, only a `declare:` one can write a declaration, so
`declaredBy: app:<host>` means exactly "declared through a declare grant".
A new property would need a core kind upgrade and a backfill to cover the
repositories that already hold the package, which `declaredBy` does not.

**Who may change the package.** `authorizeAppOwnedWrite`, at the one
declaration chokepoint (`internal/engine/seed.go`), refuses with 403, naming
the app, any write to the package's declarations by a hand other than the
`app:<host>` that declared it: the owner's doors (the console,
`substratectl`, the bare API), an agent, a function, another app, and the
substrate's own paths, so an uninstall or a catalog install is refused too.
Every door reaches it: `/vocabulary/apply` and its preview, the generic put,
patch and delete of a `package`, `bundle` or `kind` row, and the bundle
lifecycle's uninstall. The same app writes as before, and the scoped surface
still holds it to the package its `declare:` scope names, to package, bundle
and kind documents, and to additions only. A full-access token of the same
app writes as a door, not as the app, so it is refused too. The rule runs
the other way as well: an app writes only into a package it declared or a
new one, never into a package the owner holds.

**What stays the owner's.** The records of the package's kinds: reading,
writing and deleting them is held to the owner's token and an app's scopes
exactly as before. The lock is on the declarations alone.

**The way back.** `POST /api/v1/vocabulary/takeover` with
`{"package": "<authority>/<package>", "confirm": "<authority>/<package>"}`,
from the console's package page (**Take over…**, which asks for the
identity to be typed) or `substratectl takeover <pkg> --confirm <pkg>`. Only
the owner's own hand may take a package over, never an app, agent, function,
bundle or the substrate; the confirmation must repeat the identity; and a
package no app manages answers 409. The takeover rewrites the row's
`declaredBy` to the hand that took it, in one changelog entry, and moves
nothing else: no kind, no record, no token. From then on the owner writes
the package as any of theirs, and the app's next apply into it is refused,
which is how the app learns. There is no hand-back: an app's ownership can
only begin with the app's own first declaration. Revoking the app leaves the
package locked: a revoke is also how an app is signed out and back in, and
an unlock that came with it would let the next sign-in find the package the
owner's. Nothing ever deletes records on the way.

This amends 0109: `declaredBy` is who declared the package, unless the owner
took it over from an app, in which case it is who took it over, and the
changelog keeps who declared it.

**Repositories that already hold an app's package** need no migration. The
package row written by the declare-scoped apply already carries
`declaredBy: app:<host>`, and ownership is read from that row on every
declaration write, never cached, so such a package is the app's from the
first request under this binary. A rebuild, an export and a restore carry
the row and so the ownership.

**The app's edit check becomes a safety net.** 0203 has the app record each
kind's version after its own applies and stop before an upgrade over one
whose version moved. With the owner unable to move it, that check only fires
for a package taken over (where the apply is refused anyway) or one that
predates the app's install; the app keeps it as a second line, and the
choice it offered to apply anyway goes.

### Consequences

- Good, because the owner cannot break the app's kinds by accident, and the
  app no longer has to detect and arbitrate the owner's edits.
- Good, because no repository needs a migration, and the console says where
  the kinds come from: "Managed by <app>" on the package, its collections
  and its kind records, whose edits are hidden.
- Good, because the approval page says what the scope now gives away: "The
  app will own these kinds: only it can change them, and you can't edit them
  unless you take the package over. The records in them stay yours."
- Bad, because an owner who wants a property the app does not ship has to
  take the whole package over, and the app stops updating it from then on.
- Bad, because a takeover cannot be undone: the app cannot reclaim the
  package, and the owner cannot hand it back.
- Bad, because a second app approved for the same `declare:` package passes
  the approval and is refused only at its first apply.
- Bad, because the declare sentence SubstrateKit shares with the console
  through `app-scopes.fixture.json` does not mention ownership yet; the
  approval page says it in a line beneath.

### Confirmation

`internal/engine/appowned_db_test.go` holds every refusal and the takeover
(`TestAnAppsPackageChangesOnlyThroughThatApp`,
`TestAnAppsKindsRecordsStayTheOwners`,
`TestAnAppDoesNotWriteTheOwnersPackage`,
`TestTheOwnerTakesOverAnAppsPackage`);
`internal/testenv/appowned_db_test.go` (`TestAnAppOwnsThePackageItDeclares`)
runs them over the real server with an app's token, the console's and
`substratectl`'s doors, and a second app's; `internal/api` tests the route
(`TestTheOwnerTakesOverAnAppsPackage`); the console's
`lib/packages.test.ts` and `components/packages/app-managed.test.tsx` hold
the read-only doors and the typed confirmation.

## More Information

The scope is in [apps](../apps.md#keep-a-package-of-your-own), the command in
[substratectl](../substratectl.md). Revisit when an owner needs to extend an
app's package without taking it over, or when a takeover needs undoing.
