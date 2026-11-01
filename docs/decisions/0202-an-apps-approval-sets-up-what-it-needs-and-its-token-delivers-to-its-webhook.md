---
status: accepted
date: 2026-11-01
decision-makers: George Antoniadis
---

# 0202. An app's approval sets up what it needs, and its token delivers to its webhook

## Context and Problem Statement

Substrate Sync signs in with the owner's password because its set up is five
things an app's token is refused: installing two catalog packages, creating
its Health account, turning on a webhook trigger with a key, reading that
trigger's parked deliveries, and storing shared images as blobs. Its token
therefore reads every record, applies vocabulary and mints tokens, which
[0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)
set out to end. Nothing told any app, with its own token, that it was out of
date, and the approval page listed every protocol fact at once. The design
and its review round are in [the plan](../plans/app-setup.md).

## Considered Options

- Keep the password for apps that set things up.
- A scope verb that installs packages or writes accounts and triggers from
  the app's own token.
- The authorization request names what the app needs set up (RFC 9396
  `authorization_details`); the owner sees it beside the scopes and
  approving runs it, as the owner, before the code is issued.
- The same, with the set up returning the webhook's key to the app.

## Decision Outcome

Chosen: set up at approval, with the app's own token as its webhook
credential, because it is the only option where the app never holds more
than its scopes and the owner sees every change before it happens. The
password was rejected for the reason 0194 rejected it. An installing scope
was rejected because an install writes declarations, which no scope may
(0195 kept that rule), and because a token that installs is a token that
ships code whenever it likes. Returning the key was rejected in review: the
key outlives the token, so revoking the app or its access ending would not
end its deliveries, two apps would share one secret, and a replayed code
would leak a credential nothing rotates.

The rules this binds:

- **Three detail types.** `package`: a package of the binary's catalog, in
  its provider tier, at a version or later; nothing outside the binary's
  catalog, and no sample (its requirements are the console's door to
  import). `account`: one record of a kind that implements
  `accountconfig` and belongs to a package the same request names, under an
  id other than `default` (the record a package's inputs resolve first),
  created once with `ifVersion: 0`, with no `secret` or `managed` property
  and none restricted to a `writer` but `owner`, which the set up writes as.
  `webhook`: a trigger its provider does not sign, whose callable a package
  the same request names ships; approving enables it and sets a key only
  when it has none, and an existing key is never read, changed or returned.
- **One plan over the whole install closure.** The requested packages and
  every requirement the catalog would install first, as the catalog's one
  walk names them, meet the same checks. Whose a package is, the owner's or
  a publisher's, is read off its package record, whether it declares kinds
  or only functions or traits, so a package the owner declared is never
  installed over. A row that cannot run keeps the action it would run
  (`install`, `upgrade`, `create`, `enable`) and carries a block: a reason a
  client branches on (`server-update`, `not-shipped`, `sample`,
  `quarantined`, `declared-by-owner`, `upgrade-needs-review`,
  `not-requested`, `account-deleted`, `unsupported`), the server's one
  sentence for it, and the console path that clears it where the owner can.
  A blocked row keeps the request from being approved.
- **Approving runs the set up before the code exists**, in the background,
  detached from the HTTP request, one set up per repository at a time, as a
  state the parked request moves through under the store's lock after the
  repository that claimed it is checked. Setting up is the owner's act: an
  approval of a request that carries details comes through a human door,
  the console, `substratectl` or the API, whatever its rows would do, and
  writes as that door; any other actor is refused before the request is
  reserved or planned. The approval names the plan's hash and a plan that
  moved is refused; a set up that waited for its turn runs what was
  approved or less, and stops before writing when a row would do more or a
  webhook would run another package's code. Packages run first, then
  accounts, then webhooks, whatever the order of the rows. What landed
  stays when a set up stops; the flow's own state stays in memory, and an
  approved request is answered once, with the view built as its set up
  finished and nothing read again, its code living from that answer.
- **`webhook:<trigger>` is a scope only an approved detail puts on a token.**
  It admits the token at the webhook door in place of the key, for that
  trigger of that repository, and reads that trigger's parked deliveries. A
  revoked or lapsed token is refused like a wrong key.
- **No app's token reads a webhook key**, on any read path, whatever its
  scopes.
- **An app's token stores a blob** when it writes a kind that declares a
  `blobref` property; reading a blob stays the owner's, the answer never
  reveals another blob's name or media type, and neither does a blob
  reference the token reads unless it reads the blob manifests.
- **An app asks whether it is still set up** with its own token, and is told
  only what its scopes let it see. Asking is never refused for its pace: a
  check that must plan waits its turn, one at a time per token and a few at
  once on the server. Its scopes, and the scopes the page folds as already
  allowed, are compared by what they reach, never by how they are spelled.
  A local app's request is never compared with an earlier approval: nothing
  ties its client id to the app.
- **The approval page reads the console's one Technical details switch**
  ([0131](0131-the-console-writes-for-two-readers-behind-one-switch.md))
  and offers it in place: plain sentences and the set up by default, every
  protocol fact one switch away, the scope checkboxes in both.

### Consequences

- Good, because Substrate Sync no longer needs the password on a server that
  ships this, and every set up step is on the page the owner approves.
- Good, because an app's deliveries end with its access, and no app ever
  learns a webhook key.
- Good, because an app can tell, without guessing from failures, that it
  needs an approval, a renewal or a newer server, and sends the owner back
  for exactly that.
- Bad, because approving can now install code on the owner's server (a
  provider's functions and enabled triggers): the page says so, but the
  owner consents to the catalog's code, not to the app's.
- Bad, because a delivery may name any account the package's callable
  accepts, and the parked list shows every sender's failures for that
  trigger; binding an app's deliveries to its own account waits for the
  door to know accounts.
- Bad, because what a set up installed stays after the app is revoked, after
  a denial that followed a failed run, and after a rollback.
- Bad, because a binary from before this change refuses a token carrying
  `webhook:` (fail closed; the token works again once rolled forward, and
  there is nothing to revoke), and one from before 0194 reads every app
  token as full access, as 0194 already says.
- Bad, because a webhook its provider signs cannot be an app's, since the
  provider holds the secret the door checks.

### Confirmation

`internal/authorize`: `TestAuthorizationDetailsAreReadStrictly`,
`TestASetUpReservesTheRequestForTheRepositoryThatShowedIt`,
`TestAFailedSetUpIsPendingAgainWithWhatLanded`,
`TestAFinishedSetUpMintsOneCodeAndIsAnsweredOnce`,
`TestAFinishedSetUpsCodeLivesFromItsHandOver`,
`TestARefusedApprovalKeepsTheRequestsLapse`. `internal/catalog`:
`TestRequirementsNamesWhatTheCatalogCannotInstall`. `internal/appsetup`:
`TestTheChainCarriesEachPackagesOwnFacts`,
`TestAPackageTheOwnerDeclaredIsNeverReplaced`,
`TestWhoseAPackageIsComesFromItsPackageRecord`,
`TestAnAccountIsCreatedOnceAndNeverBroughtBack`,
`TestTheShippedSignedWebhookIsNeverAnApps`,
`TestARunSetsUpPackagesThenAccountsThenWebhooks`,
`TestAWebhookReboundWhileWaitingStopsTheRun`. `internal/api`:
`TestTheApprovalPageShowsWhatApprovingSetsUp`,
`TestASetUpThatCannotRunIsBlockedAndRefused`,
`TestApprovingRunsTheSetUpAndTheTokenCarriesIt`,
`TestAnApprovalOfAPlanThatMovedIsRefused`,
`TestAnApprovalWithDetailsComesThroughAHumanDoor`,
`TestAnApprovedRequestIsHandedOverWithoutReadingTheRepository`,
`TestAnAppAsksWhetherItIsStillSetUp`,
`TestACheckReadsDetailsAsTheAuthorizationEndpointDoes`,
`TestACheckPlanWaitsItsTurn`,
`TestAnAppsTokenDeliversToTheWebhookItsScopeNames`,
`TestAWebhookTokenThatDoesNotDeliverIsRefusedLikeAWrongKey`,
`TestATriggerAnAppsTokenReadsNeverCarriesItsKey`,
`TestAnAppsTokenStoresBlobsForAKindWithABlobRef`,
`TestABlobReferenceAnAppsTokenReadsCarriesNoManifestItCannotRead`.
`internal/engine`:
`TestWebhookGrantStandsInForTheKeyAlone`,
`TestAPutAtVersionZeroNeverResurrectsATombstone`. `internal/scope`:
`TestAWebhookScopeIsStoredNeverRequestedAndReachesOneTrigger`,
`TestAGrantCoversWhatItHoldsByReachNotSpelling`. `internal/e2e`: APP-01,
which approves Substrate Sync's request through the engine, exchanges the
code, delivers with the token and revokes it.

## More Information

Amends 0194 (an approval may run set up; the scope grammar gains `webhook:`;
the scoped surface gains the webhook door, the parked list, blob storage and
the set up check) and 0195's out-of-scope list. Reopen when an app needs a
package outside the binary's catalog, a sample set up, its deliveries bound
to its own account, a webhook its provider signs, or a secret the owner
types.
