---
status: proposed
date: 2026-11-03
decision-makers: George Antoniadis
---

# 0219. An app's token reads windows, aggregates, semantic search, blobs and the feed's history over the kinds it reads, and calls the functions its call scope names

## Context and Problem Statement

[0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)
opened the records route, the record path and the unit reads to an app's
token and left "the change feed, aggregate and window reads, blobs,
functions and agents" the owner's until each had a design;
[0195](0195-an-app-talks-to-agents-decides-their-requests-and-tails-the-feed-it-reads.md)
opened the agents and the feed's live tail, and
[0202](0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md)
blob storage. A web app that shows a schedule, trends, files and what its
agents did needs the rest: a window read for the schedule, an aggregate for
the trends, a semantic search, the bytes behind a record's `blobref`, the
owner's functions, and the feed's history to catch up from
([the plan](../plans/authorities.md#what-a-full-app-needs-from-the-approval-page),
[phases 2 and 3](../plans/authorities/phases-2-3.md#the-wider-scoped-surface)).
Each answers `403` today, so such an app asks for `full` or the password.

## Considered Options

- Keep each surface the owner's; an app that needs one asks for `full`.
- Open each surface under the scopes the token already holds, cut to the
  kinds it reads, with a new `call:<pattern>` verb for functions held to
  the function's own reach.
- Open functions by name alone (`call:` without a reach check), trusting
  the owner to approve a function knowing what it does.
- A new `search:` verb for the semantic arm, so a read scope never spends
  the owner's embedding budget.

## Decision Outcome

Chosen: open each surface under the scopes the token holds, and `call:`
held to the function's reach, because every one of these reads is a read of
records the token may already list, so a second rule for the same records
would only be a second place to get the first rule wrong, and because a
call runs the owner's installed code, which may read and write far past the
app, so the code's declared reach is what the approval must cover. `full`
was rejected for the reason 0194 rejected it. A `call:` without the reach
check was rejected because the approval page would then show a function's
name and the owner would have to read its code to know what the app may
reach through it. A `search:` verb was rejected because the semantic arm
answers the same records a lexical search does and costs the owner a
fraction of a cent, which the approval page says in the read scope's own
sentence rather than in a scope nobody would decline.

The rules this binds:

- **A window read and an aggregate read are held as a list is**: a kind
  named that the token cannot read is unknown (`404`); naming none is cut
  to the kinds it reads, never widened, and `implements` and `purposes`
  narrow those alone. A metric read naming no kind is cut further to the
  readable kinds the observed index holds, and each metric's own record is
  of a kind the token reads. Every record the window hands back (rows,
  series, overrides, expanded referents) is cut as a list's are, an
  override of a kind the token cannot read included, so the window reads
  as if that override were never written. The sources an aggregate names,
  on the answer, on each row and in a group, are cut to readable kinds. No
  head is carried; the window's generation stays, since its cursor is held
  to it and the tail's bookmark discloses it (0195).
- **Every search mode runs over the kinds the token reads.** The semantic
  arm embeds the query with the owner's provider, and the sentence beside
  every read and write scope on the approval page says the app may search
  by meaning at the owner's embedding budget. The embed queue's pending
  count is left out of the answer, as the head is, since it counts the
  whole repository.
- **A blob is read where a live record of a kind the token reads names it
  through a `blobref` property.** A token reading no such kind reaches no
  blob (`403`); one that does is answered, for a blob no record it reads
  names, exactly what a blob nobody stored answers (`404`), so the read
  says nothing about blobs it cannot reach. The engine filters each kind
  before it reads, so a record the token cannot read is never looked at.
  The manifest's name and media type stay what 0202 made them: the
  token's only when it reads the blob manifests. An app's blob reads wait
  for slots of their own.
- **`call:<pattern>` takes the kind pattern grammar over function ids**
  (`<authority>/<package>/<name>`), with `~` resolved before storing as in
  0194. It never matches a function of `substrate.reamde.dev/core` or
  `…/llm`: naming one outright is refused, a glob skips them. A function
  the pattern does not match, or the repository does not hold, is unknown.
- **A call is admitted only where the token's read and write scopes cover
  the function's reach**: every kind or glob its `permissions.reads.kinds`
  and `permissions.writes` name, and the reach of every function its
  `permissions.call` may invoke and every agent its `permissions.agents`
  may run, including agents' function tools, offered tools and subagents
  transitively, since a call runs whatever runs under it and each callee's
  effects are held to the callee's own grant. An exact kind is covered
  when the grant reads or writes it under every rule a scope keeps (a glob
  never reaches an internal kind, and a kind the repository lacks needs an
  exact scope because its purpose is unknown); a glob is covered by a scope at least
  as wide; a write of a glob that reaches `core` or `llm` is never
  covered, since no scope writes them. The refusal names what is missing
  (`read <kind>`, `write <kind>`). The function then runs under its own
  permissions, as a trigger's or a chat's turn does (0195): the app's actor
  is the caller and the scoped mark stops at the call.
- **The approval page shows a `call:` scope with the functions it matches
  today**, each with its resolved reach and whether the request's read and
  write scopes cover it, and the scope's sentence says the app runs the
  owner's code only as far as the app already reaches.
- **The change feed opens to an app on every route**: the tail, the
  history pages, the run summaries and the forward read, each cut to the
  records the token reads as 0195's tail is, each row carrying names alone.
  A history page is read again below the rows it held back until it is
  full or the bottom is reached, so a short page means the bottom. `q` and
  `values=1` stay refused, and trigger states are never carried.

### Consequences

- Good, because a web app shows a schedule, trends, files and its agents'
  history with the scopes it already asked for, and the owner reads what
  each adds in the scope's own sentence.
- Good, because a function an app may call is bounded by what the owner
  let the app read and write, whatever the function's code does, and the
  page shows that bound before the owner approves.
- Bad, because every read and write sentence on the approval page grew a
  clause about the embedding budget, in the console and in SubstrateKit
  alike, and a semantic search by an app spends real money the owner only
  bounds through their provider.
- Bad, because a `call:` glob reaches a function installed after the
  approval, as a read glob reaches a kind, and a function's reach is read
  at every call: a function whose reach grows past the scopes stops being
  callable without the app being told why until it calls.
- Bad, because a function writing `*` or any `core` or `llm` kind is never
  an app's to call, whatever the owner would approve, so a package whose
  functions declare wide writes cannot be driven from an app.
- Bad, because an aggregate's group keyed by a source the token cannot read
  loses its source, and a window whose override the token cannot read
  shows the occurrence the owner sees moved: each reads as if the hidden
  record did not exist, which is true of every hidden record and still
  surprising beside the owner's view.
- Bad, because the history walk over row-gated kinds reads past the rows
  it hides, so a page's cost is the rows scanned, not the rows shown.

### Confirmation

`internal/scope/call_test.go` holds the grammar, the matching and the
reach-coverage rule (`TestACallScopeTakesTheKindGrammarOverFunctionsAndNeverCoreOrLLM`,
`TestAGrantCallsTheFunctionsItsPatternsMatchAndNeverCoreOrLLM`,
`TestReachCoveredHoldsAFunctionsReachToTheGrant`).
`internal/api/appscoped_test.go` holds each surface against the fake:
`TestAnAppsWindowReadIsHeldToTheKindsItReads`,
`TestAnAppsAggregateReadIsCutToTheKindsItReads`,
`TestAnAppReadsABlobARecordItReadsNamesAndNoOther`,
`TestAnAppCallsTheFunctionsItsScopeNamesWhereTheirReachIsCovered`,
`TestTheApprovalPageShowsTheFunctionsACallScopeMatchesWithTheirReach` and
`TestAnAppReadsTheFeedsHistoryCutToWhatItReads`;
`TestAnAppSearchesInEveryModeOverWhatItReadsAndSeesNoScores`
(`internal/api/authorize_test.go`) the search. The console's sentence
fixture (`web/console/src/lib/app-scopes.fixture.json`) carries the call
scope and the embedding clause, and SubstrateKit's `ConsoleSentences` suite
reads it.

## More Information

Amends 0194 (its scoped surface and its "out of scope" list) and 0195 (its
tail-only feed and its "reopen when an app needs the call API, a history
page of the feed" trigger). 0202's blob rules stand: storing is unchanged
and the manifest's name and media type are read as it says. Reopen when an
app needs a function whose reach no scope covers (a `core` or `llm` write),
a sources policy or a grouping over kinds it does not read, or a search
whose cost the owner wants to cap per app.
