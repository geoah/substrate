---
status: superseded
superseded-by: 0214
date: 2026-11-13
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
- Make it the app's, keyed on the app's full client id, stamped beside
  `declaredBy` at the declare-scoped apply.

For the owner's way back:

- Unlock the package by itself when the owner revokes the app's tokens.
- Keep it locked until the owner deliberately takes it over, confirmed by
  typing the package's identity.
- Let the owner delete the package, its kinds and their records.

## Decision Outcome

Chosen: the package is the app's, keyed on the app's **client id**, and stays
locked until the owner takes it over.

**Who the app is.** `declaredBy` names the app as `app:<host>`
([0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)),
the host of its client id, and stays that way so a person can read it. A
host is not an app: two apps served from one https host, and every loopback
build on the owner's machine, write as one `app:<host>` actor. So the
declare-scoped apply that creates the package row also stamps
`declaredByClient`, the full client id (the app's client id metadata
document URL) the owner approved, and ownership is compared on that. Both
are managed properties of the core `package` kind, stamped by the engine and
never by a document; the client id rides the request only for a scoped
token's request (`substrate.WithClientID`), so a full-access token never
carries one. Two apps on one host, or two loopback apps, are two owners. A
renewed token for the same client id is the same app.

**A local build is another app.** A build that signs in with a loopback
client id (`http://127.0.0.1:<port>/…`) and a release build with its https
client id are two client ids, so whichever declares the package first owns
it, and the other is refused. A developer who wants both points the local
build at a separate repository, or takes the package over and lets neither
manage it.

**Packages stamped before the client id.** A package an app declared under
an earlier binary carries `declaredBy: app:<host>` and no client id. The
first apply from a client id whose host matches claims it: the chokepoint
admits it, and the write stamps that client id, after which only that client
id is the owner. An app on another host is refused, as before. The claim is
the one moment a host-only stamp is trusted, and it can only narrow who
owns the package, never widen it. The approval page treats it the same way.

**Who may change the package.** `authorizeAppOwnedWrite`, at the one
declaration chokepoint (`internal/engine/seed.go`), refuses with 403, naming
the app, any write to the package's declarations by a hand other than the
client id that declared it: the owner's doors (the console, `substratectl`,
the bare API), an agent, a function, another app (from its own host or the
same one), and the substrate's own paths, so an uninstall or a catalog
install is refused too. Every door reaches it: `/vocabulary/apply` and its
preview, the generic put, patch and delete of a `package`, `bundle` or
`kind` row, and the bundle lifecycle's uninstall. The same app writes as
before, and the scoped surface still holds it to the package its `declare:`
scope names, to package, bundle and kind documents, and to additions only.
The rule runs the other way as well: an app writes only into a package it
declared or a new one, never into a package the owner holds.

Two hands are deliberately not the chokepoint's. A **full-access app token**
([0204](0204-an-app-may-ask-for-full-access-and-its-owner-approves-it-whole.md))
is the owner's own hand: it writes as a door, so it is refused the app's
package like any door, and it may take a package over, as the owner may. A
**repository migration or a changelog rebuild** (the boot migration that
qualifies bare names, a restore, an export) rewrites rows wholesale and is
not a hand at all: ownership survives it because the row, stamps and all,
does. Neither is an authenticated proof against the server's operator, who
can rewrite history.

**The bundle's runtime switch stays the owner's.** Disabling and enabling an
app's bundle changes no declaration: it stops and resumes execution, and the
app's next apply leaves the switch where it is. It stays the owner's, as an
explicit runtime switch, because the owner (or an agent acting for them, such
as the planned workout coach) relies on turning an app's bundle off as its
off switch. Purging the bundle's data is not a switch: it tombstones every
record of the app's kinds and its configuration, which the app cannot put
back, so it is refused, like an uninstall, until the owner takes the package
over. Anything that deletes or changes the declarations stays the owning
app's.

**What the package requires stays installed.** The owner's uninstall of a
package an app's package requires (its bundle's `requires:`, or a
`requiresAtLeast:` floor the candidate would break) is refused, with the
guard naming the app's package and the app. Without the guard the uninstall
lands, the app's package is not rebuilt to notice, and the next open
quarantines it, which neither the app nor the owner can mend. The owner takes
the app's package over, or removes it, first; after a takeover the uninstall
is theirs, and so is the quarantine that follows. A package the owner keeps
is not guarded. Disabling the required bundle is a runtime switch and is
admitted.

**Refused at approval.** A `declare:` scope over a package the owner keeps
(declared by hand, or taken over), or one another client id declared, is
refused on the approval page before a code or a token exists: the page shows
why on the scope and leaves it unchecked, and an approval that names it
answers 403. The rest of the request can still be approved. A renewal for
the client id that declared it, and the first approval for a host-only
package from the same host, pass. The chokepoint still refuses at apply, for
the race between an approval and a takeover.

**What stays the owner's.** The records of the package's kinds: reading,
writing and deleting them is held to the owner's token and an app's scopes
exactly as before, and so is the bundle's runtime switch. The lock is on the
declarations, and the data teardown that would empty them.

**The way back.** `POST /api/v1/vocabulary/takeover` with
`{"package": "<authority>/<package>", "confirm": "<authority>/<package>"}`,
from the console's package page (**Take over…**, which asks for the
identity to be typed) or `substratectl takeover <pkg> --confirm <pkg>`. Only
the owner's own hand may take a package over, never an app's scoped token,
an agent, a function, a bundle or the substrate; the confirmation must
repeat the identity; and a package no app manages answers 409. The takeover
rewrites the row in one changelog entry: `declaredBy` becomes the hand that
took it, `takenOverFrom` names the app it was taken from (its client id, or
its actor where none was stamped), and `declaredByClient` is cleared. The
answer is that row, so it says who the package was taken from, and so does
the changelog entry. Nothing else moves: no kind, no record, no token. From
then on the owner writes the package as any of theirs, and the app's next
apply into it is refused with 403, while its record writes still land.
There is no hand-back, and no reverse operation: an app's ownership can only
begin with the app's own first declaration. Revoking the app leaves the
package locked: a revoke is also how an app is signed out and back in, and
an unlock that came with it would let the next sign-in find the package the
owner's. Nothing ever deletes records on the way.

This amends 0109: `declaredBy` is who declared the package, unless the owner
took it over from an app, in which case it is who took it over and
`takenOverFrom` names the app.

**Repositories that already hold an app's package** need no migration step.
Ownership is read from the package row on every declaration write, never
cached, so a package with `declaredBy: app:<host>` is the app's from the
first request under this binary, and its first apply stamps the client id.
The core `package` kind moves from version 22 to 23 for the two new
properties, and the boot upgrade adds them.

**The app stops applying on a 403.** Substrate Workouts reads the package
row before it applies: a `takenOverFrom`, a `declaredBy` that is not an app,
or a client id that is not its own means the package is not its own, and it
neither applies nor checks for edits. A 403 on an apply it did send (the
race) ends the same way. Its records keep going, and Settings says who keeps
the package. 0203's edit check stays as a second line for a package that
predates the app's install.

### Consequences

- Good, because the owner cannot break the app's kinds by accident, and the
  app no longer has to detect and arbitrate the owner's edits.
- Good, because two apps on one host, and a local build beside a release
  build, are told apart, and an older install is claimed once.
- Good, because the console says where the kinds come from: "Managed by
  <app>" on the package, with its client id, on its collections and on its
  kind records, whose edits are hidden.
- Good, because the shared declare sentence, which SubstrateKit reads through
  `app-scopes.fixture.json`, says what the scope gives away: "The app will
  own these kinds: you can't edit them unless you take them over, and their
  records stay yours."
- Good, because a second app is refused at the approval page, not at its
  first apply.
- Good, because the owner keeps the bundle's off switch.
- Bad, because an owner who wants a property the app does not ship has to
  take the whole package over, and the app stops updating it from then on.
- Bad, because a takeover cannot be undone: the app cannot reclaim the
  package, and the owner cannot hand it back.
- Bad, because a developer's local build and the release build cannot share
  one repository's package.
- Bad, because the owner cannot uninstall what an app's package requires
  without taking that package over first.

### Confirmation

`internal/engine/appowned_db_test.go` holds every refusal and the takeover
(`TestAnAppsPackageChangesOnlyThroughThatApp`,
`TestAnAppsKindsRecordsStayTheOwners`,
`TestAnAppDoesNotWriteTheOwnersPackage`,
`TestTheOwnerTakesOverAnAppsPackage`, with the answer naming the app), the
client id (`TestAnotherAppOnTheSameHostIsNotTheOwner`), the claim of a
host-only stamp (`TestAHostOnlyStampIsClaimedByTheFirstClientOnThatHost`),
the dependency guard and the quarantine it prevents
(`TestAnAppsPackageKeepsThePackageItRequires`), and the bundle switch against
the purge (`TestTheOwnerSwitchesAnAppsBundleButDoesNotPurgeIt`);
`internal/testenv/appowned_db_test.go` (`TestAnAppOwnsThePackageItDeclares`)
runs them over the real server with an app's token, the console's and
`substratectl`'s doors, a second app's and a same-host app's; `internal/api`
tests the route and the approval refusal
(`TestADeclareScopeOverAPackageTheAppCannotKeepIsRefused`); the console's
`lib/packages.test.ts`, `components/packages/app-managed.test.tsx` and
`components/authorize/scope-row.test.tsx` hold the read-only doors, the
typed confirmation and the refused scope; WorkoutsKit's `OwnershipTests`
holds the app's answer to a 403.

## More Information

The scope is in [apps](../apps.md#keep-a-package-of-your-own), the command in
[substratectl](../substratectl.md). Revisit when an owner needs to extend an
app's package without taking it over, when a takeover needs undoing, or when
a developer needs a local build and a release build on one repository.
