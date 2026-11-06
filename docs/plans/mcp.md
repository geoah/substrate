# An MCP endpoint for assistants

Status: proposed, 2026-11-06. The binding choices are recorded in
[0230](../decisions/0230-an-assistant-reaches-a-substrate-through-mcp-with-a-token-that-works-there-alone.md).

## The problem

An owner who works in Claude Code, Codex, Cursor or another assistant has
no way to let it use their substrate short of minting a full-access token
and teaching it the REST API by hand.
[Using a substrate from an agent](../for-agents.md) is the contract such an
assistant should keep, and nothing serves it in the protocol assistants
speak. [App authorization](app-authorization.md) listed "an MCP endpoint"
as out of scope.

## The outcome

The owner gives their assistant one address, `https://<server>/mcp`,
approves it once on the console's approval page, and the assistant can do
what the owner does in the console, through tools, with three exceptions:
it never reads or writes the auth material (tokens, credentials, secrets,
recovery keys), never uninstalls or purges a provider, and never answers
another app's approval. Its changes are its own in History. **Settings →
AI assistants** says how to connect each assistant and lists the connected
ones, each one **Revoke…** away.

The owner decided the reach (everything but the auth material), the
attribution (the assistant, not the owner) and the provider exception on
2026-11-06.

## What the clients do

Checked on 2026-11-06 against a mock of this design, through each
client's own `mcp login --no-browser`:

| | Claude Code 2.1.308 | Codex 0.166 |
| --- | --- | --- |
| `client_id` | `https://claude.ai/oauth/claude-code-client-metadata` | `https://chatgpt.com/oauth/codex/client.json` |
| redirect | `http://localhost:<port>/callback` | `http://127.0.0.1:<port>/callback` |
| scope asked for | the challenge's `scope`; without one, every `scopes_supported` entry | the same |
| `resource` sent | the protected-resource metadata's `resource` | the same |
| refresh token needed | no | no |
| first call | `server/discover` (2026-07-28), then `initialize` (2025-11-25) after `-32601` | `initialize` |

Without a `scope` in the challenge both ask for `read:* write:* agents:*
decide:* call:* full` at once, which the flow refuses since `full` is asked
for alone; the challenge has to name the scope. Both send the
path-suffixed `resource` the metadata names, which `sameResource` refuses
today.

From their documentation and published client documents, not run:
claude.ai connectors and Claude Desktop use
`https://claude.ai/oauth/mcp-oauth-client-metadata` with the callback
`https://claude.ai/api/mcp/auth_callback`, on the client id's own origin,
and connect from Anthropic's network, so the server must be public. Cursor
registers by dynamic registration alone (its staff said on 2026-11-02 that
client ID metadata documents are not supported yet), which this server
does not offer; its cloud agents send no `scope`. Cursor connects with a
pasted token, below.

## The protocol

Streamable HTTP, stateless, written here in `internal/mcp` rather than
taken from an SDK: a stateless server is one JSON-RPC message in and one
out, the SDKs bring the sessions and server streams this design refuses,
and it adds no dependency.

- `POST /mcp` takes one JSON-RPC message. A request is answered
  `application/json`; a notification or a response is `202` with no body.
  A batch is refused. The body is at most 1 MiB.
- `GET /mcp` and `DELETE /mcp` are `405`: there is no server stream and no
  session. No `Mcp-Session-Id` is issued or read.
- `initialize` answers the client's `protocolVersion` when it is
  `2025-11-25`, `2025-06-18` or `2025-03-26`, else `2025-11-25`. A
  request's `MCP-Protocol-Version` header, when present, must be one of
  them, else `400`.
- `server/discover` (the stateless 2026-07-28 revision) answers `-32601`
  until that revision is final; Claude Code falls back on it, as shown
  above.
- A request carrying an `Origin` header other than the issuer's origin is
  `403` (the transport's DNS-rebinding rule). Assistants send none.
- `initialize` carries `instructions`: the rules of
  [for-agents](../for-agents.md) in a dozen lines (read the kinds before
  the first write, write the heading property, hold writes with
  `ifVersion`, never retry a `422` unchanged, ids come from results).
- Methods: `initialize`, `ping`, `tools/list`, `tools/call` and the
  `notifications/*` it ignores. Resources, prompts and elicitation are not
  offered.
- A tool result is `content` with one text block holding the JSON answer,
  and `structuredContent` with the same object. A result past 64 KiB is
  cut, and the text says so and how to page. A REST refusal is a tool
  result with `isError: true` carrying the error object, so the model reads
  `problemDetails` and corrects itself, as for-agents.md asks.

## Access: the `mcp` scope

`mcp` is a new scope word, asked for alone like `full`
([0204](../decisions/0204-an-app-may-ask-for-full-access-and-its-owner-approves-it-whole.md)),
approved whole through a human door, and bound to the MCP door.

**Discovery.** A `401` from `/mcp` carries
`WWW-Authenticate: Bearer resource_metadata="<issuer>/.well-known/oauth-protected-resource/mcp", scope="mcp"`.
That metadata answers `resource: <issuer>/mcp`, the issuer as its
authorization server and `scopes_supported: ["mcp"]`. The root metadata
and the server metadata list `mcp` beside the other scopes.

**The flow.** `/oauth/authorize` and `/oauth/token` accept a `resource`
naming the issuer, `<issuer>/mcp`, or none. A request whose `resource` is
`<issuer>/mcp` and whose `scope` is absent reads as `mcp`. `mcp` beside any
other scope, or with `authorization_details`, is refused. The approval page
offers no checkbox; the owner picks a lifetime, `until you revoke it` first
for `mcp`, since there is no refresh token and an assistant that loses its
token sends the owner through the page again.

**The token.** Approving `mcp` mints a token whose stored `scopes` is
`["mcp"]`, with the app's client id and claimed name, so a binary from
before this change refuses it instead of reading it as full access: a
stored scope it cannot parse fails the token. `POST /tokens` also mints one
when the body names `scopes: ["mcp"]`, for a client that takes a pasted
token; its `label` is its name.

**Where it works.** An `mcp` token authenticates at the MCP door alone:
the `/mcp` handler and the requests that handler makes in process, marked
on the context, never by a header. Anywhere else it is `403`, naming
`/mcp`. At the door it holds the owner's tier with no scope mark, so a
vocabulary apply, an install, a function call or an agent chat goes through
as the owner's would, and every request runs on a dataset wrapper that:

- refuses `substrate.reamde.dev/core/token`, `/credential`, `/secret` and
  `/recoverykey` on every method, and leaves them out of every list,
  search, expansion and change page, as a glob grant does
  ([0080](../decisions/0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md));
- refuses a bundle lifecycle move to `uninstalled` or `purged`;
- refuses the token mint and revoke, the export, the credential changes,
  the approval calls, domain verification and `oauth/start` (connecting a
  provider account needs the owner's browser, so the `providers` tool
  answers the console link instead).

Secret-typed properties read back redacted on every surface already.

**The actor.** An `mcp` token writes as its app's derived actor
([0209](../decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md)):
`app:local` for a loopback approval (Claude Code, Codex), `app:<host>` or
`app:<authority>` otherwise (claude.ai connectors), and `api` for a pasted
token. The changelog's principal is the token, which names the assistant,
so History says which one (below).

## The tools

Each tool sends one request, in process, to the REST route it names, with
the caller's own bearer and the door mark. The route's validation, errors,
idempotency and attribution are the API's exactly, so for-agents.md holds
word for word and no rule is restated in Go. Annotations tell the client
which tools only read and which destroy, so an assistant asks before those.

| Tool | Does | Route | Hint |
| --- | --- | --- | --- |
| `kinds` | lists the kinds with purpose and first sentence | `GET /records`, filter kind `core/kind` | read only |
| `describe_kind` | one declaration: properties, states, transitions, traits | `GET` the `core/kind` record | read only |
| `query` | one record, a page, a search, a window, an aggregate | the record path or `GET /records` | read only |
| `history` | the changes to a record or a kind, newest first | `GET /changes` | read only |
| `write` | `put`, `patch` or `delete` one record, with `ifVersion` and an idempotency key | record path, `POST /records` | destructive |
| `propose` | a `recordpatchrequest` for the owner to review | `POST /records` | |
| `decide` | accept or reject a change request | `PATCH` the request | |
| `apply` | declare or change vocabulary; `plan: true` previews | `POST /vocabulary/apply`, `/plan` | destructive |
| `catalog` | list, read, install a provider, import a sample | `/catalog…` | |
| `call` | run a function | `POST …/function/{name}/call` | |
| `chat` | talk to an agent; answers the reply and the thread | `POST …/agent/{name}/chat` | |
| `providers` | sync status, run a sync now, the link to connect an account | `/sync/status`, `…/trigger/{id}/run` | |
| `merge` | merge or split two records | `POST /merge`, `/split` | destructive |

`chat` reads the turn's event stream to its end, or to 50 seconds, and
answers the reply so far with the thread id; a longer turn keeps running
detached ([0169](../decisions/0169-a-chat-turn-runs-detached-from-its-client-and-its-thread-is-attached-by-id.md)) and `query` reads its thread.

## History names the assistant

The change wire gains `principal`, the token id behind the write, on the
owner's reads (an app's scoped read never carries it, since it would name
other apps' tokens). The console's History names an `app:` actor by its
token's claimed name where it has one: "Claude Code, an app on this
device". A deleted token leaves the actor's own words.

## Console

- **Settings → AI assistants** (`/settings/assistants`): the address with
  Copy; one tab per client (Claude Code, Codex, Claude, Cursor, Other) with
  its exact steps; what an assistant can and cannot do; the connected
  assistants (tokens whose scopes are `["mcp"]`), each with its name, its
  app, when it was approved, when it stops working, and **Revoke…**. The
  Cursor and Other tabs mint a token for a pasted header and show it once.
- **The approval page** words an `mcp` request: where it works (this
  server's MCP address and nowhere else), what it can do, what it never
  sees, and that its changes appear as the assistant.
- **Settings → Apps** leaves `mcp` tokens to the new page.
- A browser opening `/mcp` (`Accept: text/html`) is sent to the new page.

## Discovery

`server.json` gains `surfaces.mcp` (`endpoint: /mcp`) and the feature
`mcp`, `alpha`, surface `mcp`.

## Tests

- `internal/mcp`: the JSON-RPC shapes, versions, notifications, the
  `405`s, the origin rule, a tool's error result, truncation.
- `internal/api`: the challenge and the `/mcp` metadata; `resource`
  `<issuer>/mcp` and an absent scope through authorize and token; `mcp`
  refused beside another scope; the stored `["mcp"]`; the token refused off
  the door; each auth kind refused and absent from lists, searches and
  changes; uninstall and purge refused; the actor; `principal` on the
  owner's change page and not an app's; each tool against the fake.
- The whole flow with the real Claude Code and Codex client documents as
  fixtures of the fake fetcher.
- The console: the page, the scope words, the approval page's `mcp` copy,
  History's name.

## Out of scope

The 2026-07-28 stateless revision (`server/discover`) until it is final;
resources, prompts and elicitation; dynamic client registration (Cursor
uses a pasted token until it reads client ID metadata documents); refresh
tokens; a local `substratectl mcp` over stdio.
