---
status: proposed
date: 2026-10-20
decision-makers: George Antoniadis
---

# 0180. A refresh stores its rotated token, and an hour of refusals parks the credential

## Context and Problem Statement

WHOOP rotates the refresh token on every refresh and refuses the old one from
then on. The engine ran the provider call and the store under the caller's
context and returned a store error as its own, so three things lost the new
refresh token after WHOOP had issued it: a store that failed, a caller that
ended mid-call or mid-commit (an invocation, the two-minute pass budget, a
shutdown), and a commit refused for any other reason. Every later refresh was
answered `400 invalid_request`, and the owner's WHOOP account lost twelve days
of data (#913). The logs of 2026-10-08 show which one: the process lost its
database connection and its writer lease, the refresh pass ran anyway, WHOOP
rotated the token, and the store was refused because the process could not
write. Two more gaps sat beside it. A refusal of the old grant that
answered after a reconnect stamped `erroring` over the new grant's
`connected`, which also made WHOOP's on-request trigger skip the sync 0176
asks for. And only `invalid_grant` ever parked a credential, so a refusal like
WHOOP's was retried for days and the account never asked for a reconnect.

## Considered Options

- Keep the caller's context and retry the store
- Run the call and the store detached, each under its own bound, retry the
  store, and keep a token that still cannot be stored in memory until it lands
- Persist a refusal count on the account and park after a fixed count
- Park after a fixed number of refusals, however close together
- Park when the backoff step reaches an hour, the seventh refusal

## Decision Outcome

Chosen: the call and the store run detached and the unstored token is kept in
memory; a credential is parked when its backoff step reaches an hour. Retrying
under the caller's context still loses the token to a cancel; a persisted
count adds a column and a migration for a number the backoff already holds.

**No refresh without a write.** A process that cannot write sends no refresh:
the refresh pass ends at its start, and an invocation's refresh stops before
the provider call, while the service is open read-only or the writer lease
(0083) is lost. The provider would spend the refresh token and the store would
be refused.

**Bounds.** Once the turn is held, the backoff checked and the caller still
live, the provider call runs under `context.WithoutCancel` with
`oauthflow.CallTimeout` (30 s). The store, and the stamp of a failure, run
under their own `commitBudget` (30 s), which starts once the provider has
answered, so a call that ran out its bound is still stamped `erroring`. The
store is tried three times, pausing 250 ms then 500 ms, and a lost
compare-and-swap is an answer, not a failure. A read-only service and a lost
writer lease are not retried: neither passes inside a second.

**The kept token.** A store that fails every attempt keeps the fresh token on
the credential's refresh entry, logs at ERROR, and hands the caller its access
token. The next hand under the turn stores it before anything else; until it
lands, that token stands in for the stored one, so the provider is asked only
when it is due and with its refresh token, never the spent one. A reconnect
that replaced the credential drops it with a warning and does not revoke it,
because at some providers a revoke ends every token of the user's grant, the
reconnect's among them. Deleting the account revokes it beside the stored
token and drops it: the teardown takes each credential's turn, so a refresh
in flight finishes first.

**A refusal answers its own grant.** Before arming a backoff the engine reads
the credential's generation again; if a reconnect or a teardown replaced it,
the refusal records nothing and a waiting caller gets the new token. The
stamp itself checks the generation inside its transaction, under the account's
record lock, which the callback holds while it stores the new grant: either
the callback commits first and the stamp sees the new generation and writes
nothing, or the stamp commits first and the callback's patch writes
`connected` over it.

**The park.** A 4xx other than 408 and 429 doubles the backoff from a minute;
at the seventh refusal of one generation the step is 64 minutes, past
`oauthParkAfter`. The refresh loop then parks the credential the way it parks
`invalid_grant` (a compare-and-swap on the refused generation, so a reconnect
in flight keeps its credential listed) and writes `tokenError` as `refused N
times since <first refusal>, last answer: <status and code>; reconnect the
account`. An invocation that meets the seventh refusal does not park; the next
pass finds the hour-long backoff and parks without calling the provider. The
refusal that reaches the step stamps the same text, whoever sent it, and so
does every later refusal of that generation: an invocation still asks the
provider each time the backoff ends, and its refusal keeps the count, the first
refusal and the ask instead of the bare answer. A 5xx, a 408, a 429 and a
transport failure arm no backoff, so they never park.

### Consequences

- Good, because a rotated token survives a canceled caller, a shutdown and a
  store failure while the process lives.
- Good, because a reconnect is never marked `erroring` by the grant it
  replaced, so its sync request is answered.
- Good, because an account refused for an hour stops costing a call a pass and
  says on the record what happened and what to do.
- Bad, because a shutdown can wait up to 30 s for a call and 30 s for its
  store, longer than the default 30 s grace period; a kill in that window
  loses the token as before.
- Bad, because the kept token lives in process memory only: a crash or a
  restart before it lands loses the grant.
- Bad, because the count and the first refusal are per process: after a
  restart a dead grant takes another hour to park.
- Bad, because a pass can now run up to one call and one store past its
  two-minute budget, which #944's starvation does not need.
- Bad, because a refresh due while the writer lease is lost waits for the
  lease, so an access token can expire meanwhile and invocations fail until
  the lease is back.
- Bad, because the count moves with each refusal past the park, so the reason
  text changes and `tokenErrorAt` is the latest refusal, not the first; the
  first is in the text.
- Bad, because the park's reason is free text that the console shows as it
  is; a bundle cannot tell it apart from a provider's answer except by its
  words.

### Confirmation

`TestOAuthRefreshStoresARotatedTokenAfterItsCallerEnds`,
`TestOAuthRefreshKeepsAnUnstoredRotatedTokenUntilItLands`,
`TestOAuthRefreshStoreRetriesBeforeKeepingTheToken`,
`TestOAuthRefusalOfAReplacedCredentialStampsNothing`,
`TestOAuthRepeatedRefusalsParkTheCredential`,
`TestOAuthAnInvocationsSeventhRefusalParksAtTheNextPass`,
`TestOAuthTransientFailuresNeverPark`,
`TestOAuthRefreshSendsNothingWhileTheWriterLeaseIsLost`,
`TestOAuthRefreshThatRunsOutItsCallBoundStampsErroring`,
`TestOAuthRefusalStampChecksTheGenerationUnderTheLock`,
`TestOAuthReconnectDropsAKeptToken`,
`TestOAuthAKeptTokenThatIsDueRefreshesWithItsOwnRefreshToken` and
`TestOAuthTeardownRevokesAKeptToken` in
`internal/engine/oauthrotation_db_test.go`, and
`TestOAuthBackoffCountsRefusalsOfAGeneration` in
`internal/engine/oauthbackoff_internal_test.go`.

## More Information

Issues #913 (both questions; its 2026-10-20 comment traces the lost token to a
refresh sent while the writer lease was lost) and #944. Builds on #909 (parking `invalid_grant`), #929 (the backoff
and the 30 s call bound), [0083](0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)
(one writer per repository, which makes an in-process turn enough) and
[0176](0176-a-reconnect-requests-a-sync.md). Revisit if a kept token is lost
in practice: a second, durable store for it would close the gap.
