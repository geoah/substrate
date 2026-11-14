---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis (via an agent session)
---

# 0255. A provider's writes ride toggles of their own, and a seeded secretary takes the providers' tools by pattern

## Context and Problem Statement

The Google provider was read only, and the owner wants Moss to search and
read Gmail, Calendar and Drive live, draft and send mail, and change events,
with a helper agent for the longer jobs. Three choices bind later code. Where
the consent for a write lives: the Google grant is derived from the account's
toggles, and each toggle so far was a sync stream. How a surface tells a
write toggle from a sync one, and an account whose grant predates a toggle it
has on. And where a provider-specific agent lives: Moss is seeded in every
repository, a provider is installed only in some, and a sub-agent reference
resolves at load ([agents](../agents.md#the-manifest)).

## Considered Options

- Writes behind toggles of their own, apart from the stream toggles.
- Write scopes added to the stream toggles (`enabledGmail` asks for
  `gmail.modify`).
- No toggle: a write tool works whenever the grant happens to cover it.
- A write toggle marked `access: write` on its `featureScopes` entry.
- A write toggle known by its name (`enabled…Write`), or by its scopes.
- The helper ships in the provider's package, and Moss names it.
- The helper is seeded with Moss, and takes the provider's offered tools by
  `toolsFrom` pattern.
- No helper: Moss takes the tools alone.

## Decision Outcome

Chosen: each write capability is an account toggle of its own, marked
`access: write` where its scopes are declared, and the helper is a seeded
`secretary` that takes the provider's tools by pattern.

**The toggles.** Google's account gains `enabledGmailWrite` (`gmail.modify`)
and `enabledCalendarWrite` (`calendar.events`). No stream syncs under them.
Every tool goes through one check: one of the toggles for what it does is on,
and the account's `grantedScopes` carries a scope that covers it. Turning a
toggle on asks for a reconnect, and the tool refuses naming it until then;
turning it off stops the tools even while the grant still holds the scope.
Folding write scopes into the stream toggles would have widened every
connected mirror's grant to sending on its next reconnect, with no switch
that meant "read, don't send". Leaning on the grant alone gives the owner no
way to stop sends short of revoking the account.

**The marker.** A `featureScopes` entry takes `access`, `read` (the default)
or `write`, and a bundle's status lists every toggle as `features` with its
scopes and its access. The facility requests both alike. The console reads
it to list write toggles under what the owner's agents may do, apart from
what the account brings in, and compares each account's `grantedScopes` with
the scopes of the toggles it has on: a toggle the grant does not cover needs
attention, with **Reconnect**. An account connected before version 51 reads
**Off** for the new toggles. A name pattern or a scope heuristic would key
behaviour on a spelling, which nothing here does. The marker is a new dialect
key ([0020](0020-dialect-keys-are-reserved-not-tolerated.md)), so core's
`bundle` kind declares it and moves to version 54.

**The helper.** `substrate.reamde.dev/assistant/secretary` is seeded in the
assistant package ([0241](0241-the-assistant-ships-with-the-binary-and-the-owner-shapes-it-through-instruction-records.md))
with `toolsFrom: providers.substrate.reamde.dev/google/*`. A pattern resolves
when a run builds its tools
([0198](0198-a-bundle-offers-its-functions-and-an-agent-takes-them-by-pattern.md)),
so it loads in a repository without the provider and gains its tools once it
is installed. A helper in the provider's package could not be named by the
seeded Moss, because a sub-agent must exist at load. 0198 rejected a
sub-agent per bundle because it costs a second LLM loop per question; that
cost is why Moss keeps the tools as well (its `*` already takes them) and
calls them itself for a single step, handing the secretary only work of
several steps.

The writes stay callables that write no record
([0125](0125-a-provider-write-is-a-callable-that-writes-no-record.md)), so no
policy or emit ceiling sees a send. The send and the calendar changes wait
for the owner's approval in the chat instead
([0254](0254-an-agents-call-of-a-function-that-declares-approval-waits-for-the-owner-in-chat.md)).

### Consequences

- Good, because a mirror's consent never carries a write the owner did not
  turn on, and one switch stops all of a product's writes.
- Good, because an account whose grant predates any toggle it has on, a
  stream's included, is flagged with the reconnect that fixes it.
- Good, because the seeded package stays loadable everywhere and the
  secretary follows an install or an uninstall with no edit.
- Good, because the write scopes chosen are the narrowest that cover the
  tools: `gmail.modify` cannot delete mail for good, and `calendar.events`
  touches no calendar's settings or sharing.
- Bad, because a binary before this one refuses a stored bundle carrying
  `access`, so rolling back past it quarantines the Google provider.
- Bad, because a provider-specific agent now lives in a package that is not
  its provider's, and another provider's tools reach it only by editing the
  seeded declaration.
- Bad, because a draft, a label and the reads run with nothing reviewing
  them: only the calls that declare an approval wait.
- Bad, because `grantedScopes` is what the consent asked for, not what the
  person approved on Google's screen, so an unticked permission reads as
  granted until a tool meets Google's refusal.
- Bad, because the operator's Google client must list both write scopes on
  its consent screen, and `gmail.modify` is a restricted scope like
  `gmail.readonly`.

### Confirmation

`TestGoogleToolsRefuseBeforeSending` holds each refusal (a toggle off, a
grant predating it, two accounts, a public `apiBase`, a redirect) and that
none sends Google anything; `TestGoogleToolsAdmit` holds the toggles' scopes
and `access: write`; `TestFeatureScopesAccessIsReadOrWrite` and
`TestBundleFeaturesListEachToggle` hold the key and the status.
`TestSecretaryTakesTheGoogleToolsAlone` holds the secretary's tool block
to the Google provider's offered tools. In the console, `account-form.test.ts`,
`providers.test.ts`, `agent-access-section.test.tsx` and
`account-dialog.test.tsx` hold the split, the reconnect problem and each
switch's state.

## More Information

[Google's tools](../bundles-catalog.md#googles-tools),
[the OAuth facility](../bundles.md#the-oauth-facility). Holding a send
before it goes is
[0254](0254-an-agents-call-of-a-function-that-declares-approval-waits-for-the-owner-in-chat.md)'s.
Reopen when another provider's tools should reach the secretary.
