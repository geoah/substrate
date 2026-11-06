---
status: proposed
date: 2026-11-06
decision-makers: George Antoniadis
---

# 0230. An assistant reaches a substrate through MCP, with a token that works there alone

## Context and Problem Statement

Owners want Claude Code, Codex, Cursor and similar assistants to use their
substrate as they do in the console. Today the only way is a full-access
token pasted into the assistant, which reaches the tokens, the export and
the credential changes, writes as the owner, and outlives a rollback as
full access. The assistants speak MCP and sign in through the flow of
[0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md),
but they ask for the scope the server's challenge names and send the MCP
address as `resource`, neither of which that flow serves.
[The plan](../plans/mcp.md) has the design, what the clients were seen
doing, and the review.

## Considered Options

- An `mcp` scope, asked for alone and approved whole, minting a token whose
  stored scopes are `["mcp"]` and which authenticates only at the MCP door,
  on the route patterns its tools use, where it holds the owner's tier on a
  dataset that never writes the auth material.
- `full` (0204) at the MCP door, with the auth material held back by the
  tool layer alone.
- The scoped verbs at the MCP door (`write:*`, `agents:*`, `decide:*`,
  `call:*`).
- A separate MCP server beside the substrate, calling its REST API.

## Decision Outcome

Chosen: the `mcp` scope, because it is the only option that gives an
assistant what the owner asked for (everything the console does but
writing the auth material) without handing the assistant's client a token
that does more anywhere else. `full` was rejected because its token is the
owner's everywhere: a copy leaked from an assistant's config reaches
`/tokens` and the export. The scoped verbs were rejected because no scope
writes `core` or `llm` (0194), so an assistant could not declare a kind,
install a package or change an agent. A separate server was rejected
because it would need its own issuer and token handling, and a `resource`
naming it is a second protected resource.

The rules this binds:

- **`mcp` is asked for alone and approved whole**, through a human door,
  like `full`, and every special case of `full` in the flow is one of
  `mcp` too. A request whose `resource` is the MCP address and whose
  `scope` is absent reads as `mcp`.
- **The token stores `["mcp"]`**, never nil, so a binary that does not know
  the word refuses it rather than reading it as full access.
- **It authenticates at the MCP door alone**: the `/mcp` handler and the
  requests it makes in process, marked on the context and held to the
  route patterns its tools use. `/mcp` admits no other token.
- **At the door it holds the owner's tier without a scope mark**, on a
  dataset that refuses every write of `core/token`, `core/credential`,
  `core/secret` and `core/recoverykey`, an accept that would write one, a
  trigger's webhook key, minting a token, the export, a provider's connect
  flow, domain verification, a package takeover and a package's uninstall
  and purge. It reads what the owner reads.
- **It writes as its app's derived actor** (0209), never as the owner, and
  History names it by the token's claimed name.
- **`/oauth/authorize` and `/oauth/token` accept `resource` equal to the
  issuer or to `<issuer>/mcp`**. One server is still one issuer.

This amends 0194, whose flow took only the issuer as `resource` and
required a `scope`. The rest of 0194 and 0204 stands.

### Consequences

- Good, because an assistant connects with the address alone and a sign-in
  in the browser, and the owner revokes it in Settings like any app.
- Good, because a token copied out of an assistant's config reaches nothing
  outside `/mcp`.
- Good, because every tool is a REST request in process, so validation,
  errors and attribution are the API's and for-agents.md holds unchanged.
- Bad, because code an assistant declares runs as the owner's code does: a
  function that spells out `core/secret`, a package input that injects
  secrets, or an accepted proposal reaches what the assistant itself
  cannot. The owner chose this over guards on assistant-declared code, and
  the approval page says it.
- Bad, because a loopback approval cannot be verified (0209), so any
  program on the owner's device can present Claude Code's client id, and a
  hosted client's shared callback means a link started from another
  account delivers the token there; the approval page warns of both.
- Bad, because a binary from before this change fails the owner's whole
  token list on the stored word, so `mcp` tokens are revoked before a
  rollback. From this change on an unknown verb reads as retired.
- Bad, because there is no refresh token: when a token lapses the owner
  approves again, or picks "until you revoke it" and a leaked token lives
  until revoked.
- Bad, because Cursor, which offers no client ID metadata document yet,
  connects only with a token the owner pastes.

### Confirmation

`internal/mcp`'s tests hold the protocol. `internal/api`'s MCP tests hold
the challenge, the metadata, the flow with the real Claude Code and Codex
client documents, the token's refusal off the door and on an unlisted
route, the auth kinds' writes and the accept refused, the webhook key, the
package lifecycle refusal and the actor. `internal/scope`'s tests hold the
word.

## More Information

Reopen when an assistant needs a surface the MCP door does not reach, when
the 2026-07-28 stateless revision of MCP is final, when Cursor reads client
ID metadata documents, or when the owner wants code an assistant declares
held back from secrets.
