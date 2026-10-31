---
status: accepted
date: 2026-10-31
decision-makers: George Antoniadis
---

# 0195. An app talks to agents, decides their requests and tails the feed it reads

## Context and Problem Statement

[0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)
left agent calls, the change feed and every `core` or `llm` write outside an
app's token "until each gets a design of its own". A native chat app is the
first app that needs all three: it signs in through the 0194 flow instead of
asking for the password, chats with the owner's agents, answers their
questions, applies or dismisses what they suggest, and follows the threads
live. A `write:` scope cannot carry this: an agent is a declaration, a
thread is the engine's record, and accepting a change request applies a
diff to a record no scope bounds.

## Considered Options

- Lift 0194's "no scope writes core or llm" rule and let `write:` scopes
  name the `llm` kinds and the change request kind.
- Two scope verbs over agent ids, `agents:<pattern>` and
  `decide:<pattern>`, each with a fixed reach, and the change feed's live
  tail held to what the token reads.
- Keep the chat app on a full-access token.

## Decision Outcome

Chosen: the two verbs and the held tail, because each says in the owner's
words what the app may do ("talk to these agents", "apply or dismiss what
they suggest") and reaches exactly that. Lifting the rule was rejected
because a `write:` on `llm/thread` would also write the status, the usage
and the agent of every thread, and a `write:` on the change request kind
would decide what any proposer wrote. A full-access token was rejected
because it is the password with extra steps, which is what 0194 exists to
end.

The rules this binds:

- **The pattern is the kind grant grammar over agent record ids**
  (`<authority>/<package>/<name>`): `*`, `<authority>/*`,
  `<authority>/<package>/*` or one id, with `~` resolved before storing, as
  in 0194. A glob covers an agent declared after the approval.
- **`agents:` reaches the matching agents' rows**: reading the agent rows,
  their threads (a thread whose `agent` matches), and those threads'
  messages and interactions; chatting, attaching to and stopping a turn;
  renaming a thread (`subject` alone) and answering or dismissing an
  interaction (`state` and `answers` alone). Nothing else: no call API, no
  agent, provider or function write.
- **`decide:` reaches the change requests the matching agents' threads
  proposed**: reading them and moving `decision` to `accepted` or
  `rejected`, with `adjustedDiff` and a note in the app's own namespace as
  the owner may
  ([0115](0115-an-owner-adjusts-a-change-request-on-the-accepting-write.md)). The API names the one request it held to the grant
  on the context, and the engine's accept gates open for that record alone.
  The accept's own write of the target still meets the owner-only gates: an
  app never revokes a token or writes an account by accepting a request.
- **A chat an app opens is the app's message and the agent's turn**: the
  user message carries `app:<host>`, the agent runs under its own
  permissions, and the scoped mark stops before the engine so the agent's
  writes meet the gates an owner's chat meets.
- **Neither verb reads a kind whole.** What they make readable is decided
  record by record, so a list of one of those kinds names it alone and is
  narrowed by the agent (threads) or by the rows checked on the way out; a
  condition naming a thread or an agent the token cannot read is answered
  404, as the record is.
- **The change feed opens to an app as its live tail alone**
  (`watch=1`): rows of records the token reads, by its read and write
  scopes and by these verbs, each carrying names only (`seq`, `ts`, `op`,
  `kind`, `recordId`, `actor` and `affected` cut to readable records); no
  payload, no values, no text search, no history page.

### Consequences

- Good, because the chat app reaches the owner's agents without a password
  and the approval page says which agents, what each may read and change,
  and that deciding is the owner's authority.
- Good, because the reach of each verb is fixed in code: an app cannot widen
  it by naming more properties or kinds.
- Bad, because chatting reaches whatever the agents may do: an agent with
  write grants writes on the app's message, which the page discloses.
- Bad, because `decide:` accepts with the owner's authority, a policy-gated
  request included; only the owner-only actions stay shut.
- Bad, because the feed's seqs and its bookmark count the whole
  repository's writes, which 0194 kept off an app's lists, and every row
  names the actor that wrote it.
- Bad, because a list of messages, interactions or requests must name its
  threads (unless a read scope reaches the whole kind): filtering such a
  list after the engine paged it would let its cursor, which carries the
  last scanned row's sort values, read past the scope.
- Bad, because a stream outlives the token check at its start, so an app's
  watch and agent streams read the token again as they run (before each
  batch and at each heartbeat; on an agent stream at most every two
  seconds), and end on a revoked or expired token.

### Confirmation

`internal/scope/scope_test.go` holds the grammar.
`internal/api/appagents_test.go` holds the reach of each verb, the chat,
the approval page and the held tail against the fake.
`TestAnAppsDecisionAcceptsTheOneRequestItNames` and
`TestAChatAnAppOpensIsItsMessageAndItsThreadToRename` (internal/engine)
hold the opened accept gate and the chat's actor.

## More Information

Amends 0194's scoped surface and its "out of scope" list, which named
agents and the change feed. Reopen when an app needs the call API, a
history page of the feed, or a write to an agent.
