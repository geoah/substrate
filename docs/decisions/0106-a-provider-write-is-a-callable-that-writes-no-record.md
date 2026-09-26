---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-644 agent session)
---

# 0106. A provider write is a callable that writes no record

## Context and Problem Statement

Every shipped provider was read only: one sync function each, and
[the catalog](../bundles-catalog.md) said none writes back. A consumer such
as Mneme could not reply in Slack or Beeper or review a GitHub pull request,
and since it takes its providers from this repository it could not add the
write itself ([issue #644](https://github.com/geoah/substrate/issues/644)).
Shipping writes means choosing what a write function takes, what fires it,
and what it does to the mirror.

## Considered Options

- A callable per write that takes the provider's own ids, sends one request
  with the credential the host injects, and writes no record
- The same callable, also writing the sent object into the mirror from the
  provider's answer
- Taking a mirror record reference (a `slack/message`, a
  `github/pullrequest`) instead of the provider's ids
- An outbox kind the owner or an agent writes rows into, with a trigger that
  sends each row

## Decision Outcome

Chosen: a callable per write that takes the provider's own ids and writes no
record. Slack ships `postmessage`, Beeper `sendmessage`, GitHub
`submitreview`. Each is reachable through `POST …/core/function/{name}/call`
and as an agent tool, and no trigger names it, so nothing sends on its own.
Each reads its credential from the same injected `config` the provider's
sync reads (the connector input's token for Slack and Beeper, the
host-resolved `config.accounts[].token` for GitHub) and holds the same origin
pin before a request is made.

The arguments are the provider's ids because every one of them is already a
property on the mirror row (`conversationId` and `ts`, `chatId` and
`messageId`, a repository's `fullName` and a pull request's `number`), so a
caller that holds a mirror row has them, and the function needs no read
grant and no lookup that could miss.

Writing no record keeps one writer per mirror: the sync is the only hand
that shapes a `slack/message` or a `github/review`. A posted object reaches
the mirror only when the sync's own discovery reads it back, and two gaps
are known. The Slack sync never reads a first reply in a thread that had
none, because `conversations.history` returns no replies and `threadWatch`
holds only parents already seen with replies
([issue #711](https://github.com/geoah/substrate/issues/711)). The GitHub
sync loses a pull request the owner was only asked to review once they
review it, because GitHub drops them from the requested reviewers and
`involves:` does not cover reviewers
([issue #710](https://github.com/geoah/substrate/issues/710)). A caller
must not treat the mirror as confirmation of a send. A second
writer would duplicate the sync's mapping and drift from it. An outbox kind
adds a kind and a trigger for what one call already does; it is the answer
when a write must be queued, reviewed or retried by the engine, and nothing
asks for that yet.

### Consequences

- Good, because a write is one request with a declared argument list, so the
  tool card a model sees and the check the engine runs are the same schema.
- Good, because the credential never leaves the host's injection: no caller
  passes a token and no function returns one.
- Bad, because a posted message or review is absent from the mirror until
  a sync reads it back, and in the two gaps above no sync does: a caller
  that waits for the mirror to confirm a send and then retries posts twice.
- Bad, because the agent loop's policy door and its `confirmation: always`
  floor act only on the record effects a function returns, and these return
  none: an agent granted one of these functions as a tool sends without
  review.
- Bad, because a retried call posts twice unless the caller sends the call
  API's `Idempotency-Key`: none of the three provider routes takes a key of
  its own.
- Bad, because a function call writes no audit record of its own; a record
  per direct call is [issue #645](https://github.com/geoah/substrate/issues/645).

### Confirmation

`internal/providere2e` drives each function against the recorded mock:
`providers/slack/scenario.py` section 17, `providers/beeper/scenario.py`
section 14, and the `submitreview` pass at the end of
`providers/github/scenario.py`. Each asserts the request the function sent,
its output, that it wrote no record, that the provider's refusal reaches
the caller, and that an `apiBase` outside the pin is refused unsent.

## More Information

Reopen when a write must be queued or approved before it is sent, which is
where the outbox kind fits, or when a caller needs the sent object in the
mirror before a sync reads it back.
