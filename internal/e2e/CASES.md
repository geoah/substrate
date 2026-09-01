# The live end-to-end cases

This is the case list for `mise run test:e2e`: one suite that drives a LIVE
substrate over HTTP exactly as a user's client would, registers a fresh user,
and leaves that user's repository in place so a human can open the console (or
`substratectl`) and look at what the run built. Every run writes a markdown
report under `.dev/e2e/` naming each case, what it tests, the steps it took,
and what came back.

The suite mocks the world, never the substrate: a fake LLM answers the OpenAI
wire, a fake OAuth provider answers the token endpoint, but every substrate
call crosses the real HTTP door of the real binary against the real database.

Status: `slice` is implemented today, `planned` is not yet. Needs: `totp` runs
only against the enforced door (`mise run dev:totp`), `egress` needs
`SUBSTRATE_EGRESS_ALLOW` pointed at loopback on the server, `dsn` needs the
operator hat (`SUBSTRATE_E2E_DSN` and a built `substratectl`).

Beside these endpoint-level cases, [STORIES.md](STORIES.md) holds the
story-level cases: whole user scenarios (people, teams, projects, calendar
events and transcripts, with the automation over them) that compose many of
the rows below into one coherent repository.

| id | story | needs | status |
| --- | --- | --- | --- |
| STORY-01 | the graph exists: owner, organizations, teams, people, projects, tasks, navigable from every end | | implemented |
| STORY-02 | attendee emails become people deterministically; the meeting-room address never becomes a person | | implemented |
| STORY-03 | a transcript finds its meeting: the `transcriptMatcher` agent decides over a scoring function tool, writes its audit, and an unmatched one attaches to nothing | egress | implemented |
| STORY-04 | reflection: action items become sourced tasks through the decision loop; a sourceless proposal is refused | egress | implemented |
| STORY-05 | the quiet window: a transcript with nothing in it proposes nothing, and the absence is asserted | egress | implemented |
| STORY-06 | the world holds together: attribution audit, chain verify, rebuild refolds identically | dsn | implemented |

## Discovery and health

| id | case | needs | status |
| --- | --- | --- | --- |
| DISC-01 | `/healthz` answers `{"status":"ok"}` | | slice (in AUTH-01) |
| DISC-02 | `server.json` states `registration.open`, `totpRequired` and the changelog horizon | | slice (in AUTH-01) |

## Registration, login, credentials

| id | case | needs | status |
| --- | --- | --- | --- |
| AUTH-01 | register with the invite code, mint and use the first token, log in, revoke | | slice |
| AUTH-02 | a wrong invite code is a 401; a closed door (no code configured) is a 501 | | implemented |
| AUTH-03 | a taken username is refused with a 422 naming it | | slice (in AUTH-01) |
| AUTH-10 | a refused registration writes nothing: the loser's changelog gains no row | | implemented |
| AUTH-04 | a username outside `[a-z][a-z0-9]{1,29}` and an out-of-bounds password are refused at validation | | implemented |
| AUTH-05 | full TOTP registration: enroll a seed, register with a live code, the code that registered cannot also log in | totp | planned |
| AUTH-06 | password change with both factors; the old password stops working; bearer on `/password` is refused 403 | | planned |
| AUTH-07 | TOTP swap: enroll a candidate seed, prove it, the old seed stops working | totp | planned |
| AUTH-08 | recovery-key enrollment is one-shot: the second attempt is a conflict | | planned |
| AUTH-09 | login failures are one indistinguishable 401 (unknown user, wrong password, wrong code) | | implemented |

## Tokens

| id | case | needs | status |
| --- | --- | --- | --- |
| TOK-01 | mint via `POST /tokens`, list, revoke; the revoked secret is a 401, the others survive | | slice (in AUTH-01) |
| TOK-02 | a token with `expiresAt` in the past is refused at authenticate | | implemented |
| TOK-03 | tokens are records: `DELETE /api/v1/core.substrate.reamde.dev/token/{id}` revokes the same as `DELETE /tokens/{id}` | | implemented |
| TOK-04 | a garbage bearer is 401; a missing header is 401; the `X-Substrate-Actor` reserved spellings are 403 | | implemented |

## Records

| id | case | needs | status |
| --- | --- | --- | --- |
| REC-01 | the record lifecycle: create (201, version 1), read, put-merge (200, version up, nothing pruned), state transition with its stamp, delete (tombstone) | | slice |
| REC-02 | `ifVersion` optimistic concurrency: a stale write is a conflict | | implemented |
| REC-03 | a PATCH with `null` deletes the property; a state property in a PATCH is a transition; an undeclared transition is refused | | implemented |
| REC-04 | a write with an undeclared property is refused naming it | | implemented |
| REC-05 | the title derives from the declared property through `displayTemplate`; a written `title` on such a kind is ignored | | implemented |
| REC-06 | client-chosen ids: POST with `id`, PUT at the id; `incoming` is refused as an id and is the only reserved word left | | implemented |
| REC-07 | labels and annotations round-trip; annotations only appear with `withAnnotations=1` | | implemented |
| REC-08 | `propertyMeta` on a single GET names the managing actor and tier after two actors write the same property | | implemented |

## References

A reference is an ordinary property whose value is a `<kind>/<id>` path, so
there are no link verbs: put and patch write pointers the way they write
strings, and every list carries them inline.

| id | case | needs | status |
| --- | --- | --- | --- |
| REF-01 | a put writes a reference and a put that omits it keeps it, so a `null` in a PATCH is what clears one; the retired `…/{id}/edges/{rel}` route is a 404 | | implemented |
| REF-02 | a reference carrying a link property its declaration does not declare is refused naming it | | story (in STORY-01) |
| REF-03 | a reference outlives the target's tombstone and dies with its purge | | implemented |
| REF-04 | `/incoming` pages and narrows by `property` and `fromKind`; the retired `rel` parameter is a 400 | | implemented |

## Queries

| id | case | needs | status |
| --- | --- | --- | --- |
| QRY-01 | `filter` selects on properties; `orderBy` orders; `first`/`after` keyset-page without skips or repeats | | implemented |
| QRY-02 | `filter.kinds` on a collection list is a 400 (the path already names the kind) | | implemented |
| QRY-03 | an unknown query parameter is a 400 naming it, with the did-you-mean for singular/plural slips | | implemented |
| QRY-04 | list `head` hands off to `watch?from=head` with no skipped and no duplicated change | | implemented |

## The changelog

| id | case | needs | status |
| --- | --- | --- | --- |
| LOG-01 | every write is a row: forward read, live watch delivery, resume from a seq, and the operator's chain verify | dsn | slice |
| LOG-02 | the backward page (`before`/`first`) walks history newest-first to exhaustion | | implemented |
| LOG-03 | feed filters: `kinds`, `ops`, `actors` and their excludes; a singular spelling is a 400 naming the plural | | implemented |
| LOG-04 | the `recordId`+`recordKind` pair narrows to one record's history; either alone is a 400 | | implemented |
| LOG-05 | a per-collection watch delivers only that collection and refuses list parameters | | implemented |
| LOG-06 | heartbeats keep an idle stream alive (the error-frame framing is pinned by every feed reader, which fails on one) | | implemented |
| LOG-07 | `repository rebuild` refolds the changelog into identical records | dsn | story (in STORY-06) |

## Vocabulary

| id | case | needs | status |
| --- | --- | --- | --- |
| VOC-01 | `vocabulary/apply` admits a new kind; records of it write and read | | story (in STORY-02) |
| VOC-02 | an additive upgrade (new optional property, new enum value) lands; the version moves | | implemented |
| VOC-03 | a narrowing (drop, retype, add required) is refused while live records hold the old shape | | implemented |
| VOC-04 | an unknown dialect key is refused at apply naming it (quarantine is the repository-open path, unreachable over the API) | | implemented |
| VOC-05 | a kind reference as a record id round-trips percent-encoded (`%2F`) | | implemented |

## Bundles and the catalog

| id | case | needs | status |
| --- | --- | --- | --- |
| BUN-01 | the catalog lists the shipped closures; install admits one; the collection exists after and not before | | slice (in REC-01) |
| BUN-02 | `requires:` ordering: installing a dependent before its dependency is refused naming what is missing | | slice (in REC-01) |
| BUN-03 | disable, enable, uninstall, purge through the bundle PATCH lifecycle; records survive uninstall and die with purge | | implemented |
| BUN-04 | a bundle input binds through `…/bundle/{id}/bind`; resolution order is the bound reference, the id `default`, then the sole record | | implemented |
| BUN-05 | an up-to-date bundle offers no `upgrade`; a moved closure needs a second binary, which one live server cannot stage | | implemented (no-motion half) |
| BUN-06 | trait endpoints: `…/trait/{id}/implementors` and `…/trait/{id}/records` see through installed kinds | | implemented |

## Functions

| id | case | needs | status |
| --- | --- | --- | --- |
| FN-01 | a python function declared in vocabulary runs via `…/function/{name}/call` and answers its output | | implemented |
| FN-02 | a raising function is a 500 `function_failed`; its stdout lands in the run record | | implemented |
| FN-03 | the host `query` function reads records; `propose`/`mutate` refuse the direct call | | implemented |
| FN-04 | a function's own egress is governed by the sandbox allowlist | egress | planned |

## Triggers

| id | case | needs | status |
| --- | --- | --- | --- |
| TRG-01 | a trigger on a kind fires its function on write; the run record shows the delivery | | story (in STORY-02) |
| TRG-02 | `…/trigger/{id}/run` synthesizes one delivery; `wake` scans now instead of on the 5s tick | | story (in STORY-02) |
| TRG-03 | a failing delivery parks; `…/parked/{fid}/retry` re-runs it | | implemented |
| TRG-04 | `replay` resets the cursor and re-delivers | | implemented |

## Agents, against a fake LLM

| id | case | needs | status |
| --- | --- | --- | --- |
| AGN-01 | an `llmprovider` record pointing at a scripted OpenAI-wire stub resolves and the agent loop completes | egress | story (in STORY-03) |
| AGN-02 | `…/agent/{name}/chat` streams ndjson events (`thread`, `delta`, `done`); the transcript persists as `llmthread`/`llmmessage` records | egress | implemented |
| AGN-03 | a tool round-trip: the stub asks for a tool, the agent runs it, the second turn completes | egress | planned |
| AGN-04 | without a resolvable provider the agent refuses at dispatch naming the row it wanted | | implemented |
| AGN-05 | the interaction records carry the priced usage from the stub's usage block | egress | implemented |

## OAuth, against a fake provider

| id | case | needs | status |
| --- | --- | --- | --- |
| OAU-01 | `oauth/start` answers the consent URL from the account's config; the callback with the HMAC state lands the tokens | egress | planned |
| OAU-02 | a tampered `state` is refused | egress | planned |
| OAU-03 | the refresh grant renews an expiring token on the sweep | egress | planned |

## Blobs

| id | case | needs | status |
| --- | --- | --- | --- |
| BLOB-01 | PUT bytes, read them back by digest with the `ETag`; a second PUT of the same bytes is the same digest | | implemented |
| BLOB-02 | a wrong-digest PUT at `/blobs/{digest}` is refused | | implemented |

## GraphQL

| id | case | needs | status |
| --- | --- | --- | --- |
| GQL-01 | the generated per-repository schema answers a records query over an installed kind | | story (in STORY-06) |
| GQL-02 | the schema follows a vocabulary apply: the new kind is queryable without a restart | | implemented |

## Merge and split

| id | case | needs | status |
| --- | --- | --- | --- |
| MRG-01 | `/merge` folds two records of one kind; the loser's id becomes a `formerId` and reads land on the canonical record | | implemented |
| MRG-02 | `/split` reverses the merge | | implemented |

## Errors and strictness

| id | case | needs | status |
| --- | --- | --- | --- |
| ERR-01 | an unknown or miscased body key is a 400 naming it | | slice (in REC-01) |
| ERR-02 | the wrong-shape routes answer 405 naming the working spelling (PUT at the collection, POST at an id) | | planned |
| ERR-03 | an unknown collection is a 404 naming it; the body cap is a 413 | | planned |
| ERR-04 | every published error code is reachable (the conformance suite already pins this in-process; here over the live door) | | planned |

## Rate limiting

| id | case | needs | status |
| --- | --- | --- | --- |
| RL-01 | a second auth attempt inside the window is a 429 with `Retry-After`; waiting it out succeeds | | implemented |

## Isolation and durability

| id | case | needs | status |
| --- | --- | --- | --- |
| ISO-01 | two registered users: each token reads only its own repository, and neither sees the other's records or changelog | | implemented |
| ISO-02 | one user's vocabulary install does not appear in the other's collections or catalog `installed` flags | | implemented |
| DUR-01 | a server restart loses nothing: the records, the changelog head and the signing key are identical before and after | dsn | planned |

## Recurrence and the calendar

The substrate stores a recurrence rule and never expands it into rows
(decision 0039). Occurrences reach a reader two ways: a connector explodes
them as `calendarevent` rows pointing at their `calendareventseries`, and
`GET /occurrences` computes the rest from the stored rules at read time
(decision 0043), staying silent inside a series' stamped
[`materializedFrom`, `materializedUntil`) span, where the rows are the truth.
These cases play that connector exactly the way Google Calendar behaves, make
the range queries a calendar client would, and read the computed half beside
them.

| id | case | needs | status |
| --- | --- | --- | --- |
| CAL-01 | every rule Google Calendar emits stores verbatim and compiles under the engine's RFC 5545 parser, which guards the series gate too; RDATE extras live in `rdates`, the anchor in `startsAt` | | implemented |
| CAL-02 | a long every-weekday series with a moved-and-retitled instance (override in place, `originalStartAt` naming the replaced slot) and a canceled one (retracted + exdate); windows return exactly the survivors | | implemented |
| CAL-03 | every-2nd-Tuesday and last-Friday series resolve to the right instants per month window; definitions never appear in a time window; /incoming lists a series' occurrences | | implemented |
| CAL-04 | this-and-following: the old series truncates with UNTIL, a new one carries the rest, the window shows the time change at the boundary | | implemented |
| OCC-01 | the occurrences read (decision 0043): a daily-forever medication schedule computes its 7 slots a week beside the calendar's rows, stamped series stay silent inside their materialized span, a logged dose annotates without suppressing, and a travel week moves via exdates + rdates | | implemented |

## The operator hat

| id | case | needs | status |
| --- | --- | --- | --- |
| OPR-01 | `repository verify` walks every hash and signature clean after a run | dsn | slice (in LOG-01) |
| OPR-02 | `repository list` names the run's repository | dsn | planned |
| OPR-03 | `user reset` re-keys the credential and old tokens die | dsn | planned |

## Embeddings

| id | case | needs | status |
| --- | --- | --- | --- |
| EMB-01 | an `embed: true` property queues on write; `embeddings/reembed` requeues; the fake provider's embedding lands | egress | planned |
