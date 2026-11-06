# An MCP endpoint for assistants

Status: proposed, 2026-11-06, reviewed once (below). The binding choices
are recorded in
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
what the owner does in the console, through tools, with these exceptions:
it never writes, mints or revokes the auth material (tokens, credentials,
secrets, recovery keys), never sees a webhook's key, never uninstalls or
purges a package, never exports the repository, never connects a provider
account and never answers another app's approval. It reads what the owner
reads, the token and credential records among them, whose secrets are
digests and redacted values. Code it declares runs as the owner's code does
and may reach what the owner's code may, secrets included, and the approval
page says so. Its changes are its own in History. **Settings → AI
assistants** says how to connect each assistant and lists the connected
ones, each one **Revoke…** away.

The owner decided all of this on 2026-11-06: the reach, the attribution,
the package exception, reads of the token records, and code without a
promise about secrets (over "no new code" and over engine guards on code
an assistant declares).

## What the clients do

Checked on 2026-11-06 against a mock of this design, through each
client's own `mcp login --no-browser` and, for Codex, `codex app-server`:

| | Claude Code 2.1.308 | Codex 0.166 |
| --- | --- | --- |
| `client_id` | `https://claude.ai/oauth/claude-code-client-metadata` | `https://chatgpt.com/oauth/codex/client.json` |
| redirect | `http://localhost:<port>/callback` | `http://127.0.0.1:<port>/callback` |
| scope asked for | the challenge's `scope`; without one, every `scopes_supported` entry | the same |
| `resource` sent | the protected-resource metadata's `resource` | the same |
| refresh token needed | no | no; a token past `expires_in` is never sent, and the owner runs `codex mcp login` again |
| first call | `server/discover` (2026-07-28), then `initialize` (2025-11-25) after `-32601` | an unauthenticated `GET /mcp`, then `initialize` (2025-06-18) |
| tool result | text | `structuredContent` alone when present; cut at 10,000 tokens |
| a `401` mid-session | not checked | an "Authentication required" tool error, no new sign-in |

Without a `scope` in the challenge both ask for `read:* write:* agents:*
decide:* call:* full` at once, which the flow refuses since `full` is asked
for alone; the challenge has to name the scope. Both send the
path-suffixed `resource` the metadata names, which `sameResource` refuses
today. Codex asks before a tool unless it is marked read-only, or marked
neither destructive nor open-world, and shows the model the first line of
the server's instructions alone.

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
and it adds no dependency. `internal/mcp` holds the JSON-RPC and the tools
and talks to the REST API through an interface; `internal/api` mounts it,
authenticates, and answers that interface in process.

- `POST /mcp` takes one JSON-RPC message, at most the vocabulary apply's
  32 MiB. A request is answered `application/json`; a notification or a
  response is `202` with no body. A batch is refused.
- Every request without a valid bearer, any method, is `401` with the
  challenge below; Codex opens with a bearer-less `GET`. With one, `GET`
  and `DELETE` are `405`: there is no server stream and no session, and no
  `Mcp-Session-Id` is issued or read. A browser asking for HTML is sent to
  the console's page. Every method is registered and none is stored.
- `server/discover` (the stateless 2026-07-28 revision) answers `-32601`
  before any header is read, until that revision is final; both clients
  fall back to `initialize`.
- `initialize` answers the client's `protocolVersion` when it is
  `2025-11-25`, `2025-06-18` or `2025-03-26`, else `2025-11-25`. Another
  request's `MCP-Protocol-Version` header, when present, must be one of
  them, else `400`.
- A request carrying an `Origin` header other than the issuer's origin is
  `403` (the transport's DNS-rebinding rule). Assistants send none.
- `initialize` carries `instructions`. Its first line, which is all Codex
  shows, says to read the kinds before the first write; the rest are the
  rules of [for-agents](../for-agents.md) in a dozen lines (write the
  heading property, hold writes with `ifVersion`, never retry a `422`
  unchanged, ids come from results, stop at a `lossy` refusal and hand the
  plan to the owner).
- Methods: `initialize`, `ping`, `tools/list`, `tools/call` and the
  `notifications/*` it ignores. Resources, prompts and elicitation are not
  offered.
- A tool result is one text block of compact JSON, with no
  `structuredContent`. A result past 32 KiB is not cut: the tool answers an
  error saying how large it was and to ask again with a smaller `first`,
  since the list tools page with `first` and `after`. A REST refusal is a
  result with `isError: true` carrying the error object, so the model reads
  `problemDetails` and corrects itself.
- Every tool carries `readOnlyHint`, `destructiveHint`, `idempotentHint`
  and `openWorldHint: false` explicitly, since a missing `destructiveHint`
  reads as `true`.

## Access: the `mcp` scope

`mcp` is a new scope word, asked for alone like `full`
([0204](../decisions/0204-an-app-may-ask-for-full-access-and-its-owner-approves-it-whole.md)),
approved whole through a human door, and bound to the MCP door.

**Discovery.** A `401` from `/mcp` carries
`WWW-Authenticate: Bearer resource_metadata="<issuer>/.well-known/oauth-protected-resource/mcp", scope="mcp"`.
That metadata answers `resource: <issuer>/mcp`, the issuer as its
authorization server and `scopes_supported: ["mcp"]`. The server metadata
lists `mcp` beside the other scopes.

**The flow.** `/oauth/authorize` and `/oauth/token` accept a `resource`
naming the issuer, `<issuer>/mcp`, or none. A request whose `resource` is
`<issuer>/mcp` and whose `scope` is absent reads as `mcp`. `mcp` beside any
other scope, or with `authorization_details`, is refused. Every place the
flow treats `full` specially treats `mcp` the same way, the human-door
check above all, so an app holding a `full` token cannot approve an `mcp`
request. The approval page offers no checkbox and selects
`until you revoke it` first, since there is no refresh token. For an app
whose redirect is not a loopback address (claude.ai's connectors), whose
client id and callback every account of that service shares, the page
says to approve only a sign-in the owner just started.

**The token.** Approving `mcp` mints a token whose stored `scopes` is
`["mcp"]`, with the app's client id and claimed name. `POST /tokens` also
mints one when the body names `scopes: ["mcp"]`, for a client that takes a
pasted token; its `label` is its name. A binary from before this change
refuses the token at authentication, but also fails its whole token list
on the word it cannot parse, so `mcp` tokens are revoked before a
rollback; from this change on, a stored scope with a verb the binary does
not know reads as retired, not as damage.

**Where it works.** `/mcp` admits an `mcp` token alone; any other token is
`403` with `Bearer error="insufficient_scope", scope="mcp"` and the
metadata, so the client signs in for the right one. An `mcp` token
authenticates at the MCP door alone: the `/mcp` handler and the requests
it makes in process, marked on the context, never by a header. Those
requests are held to a fixed list of method and route patterns, the ones
the tools use, checked on the pattern the router matched; anything else is
`403`, and so is an `mcp` token anywhere outside the door. A tool takes a
kind only as a full three-segment reference and percent-encodes every id it
puts in a path.

At the door the token holds the owner's tier with no scope mark, so a
vocabulary apply, an install, a function call or an agent chat goes
through as the owner's would, and every request runs on a dataset wrapper
that refuses:

- a put, patch, delete or merge of `substrate.reamde.dev/core/token`,
  `/credential`, `/secret` or `/recoverykey`, and a decision on a change
  request whose target is one, since the accept writes it under the
  owner's hand;
- a trigger's webhook key: cut from every trigger read back and from the
  change feed's trigger entries, and refused as a list's filter or order;
- minting a token, the export, a provider account's connect flow (the
  tools name the console's Providers page instead), domain verification,
  an app's set up bookkeeping, a package takeover and the engine's
  maintenance passes;
- a package's uninstall and purge.

**The actor.** An `mcp` token writes as its app's derived actor
([0209](../decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md)):
`app:local` for a loopback approval (Claude Code, Codex), `app:<host>` or
`app:<authority>` otherwise (claude.ai connectors), and `api` for a pasted
token. `X-Substrate-Actor` is ignored, as it is for an app.

## The tools

Each tool sends requests, in process, to the REST routes it names, with
the caller's own bearer and the door mark. The routes' validation, errors,
idempotency and attribution are the API's exactly, so for-agents.md holds
word for word and no rule is restated in Go.

| Tool | Does | Routes | Hint |
| --- | --- | --- | --- |
| `kinds` | lists the kinds with purpose and first sentence | `GET /records`, kind `core/kind` | read only |
| `describe_kind` | one declaration: properties, states, transitions, traits | `GET` the `core/kind` record | read only |
| `query` | one record, a page, a search, a window, an aggregate | the record path, `GET /records` | read only |
| `history` | the changes to a record, a kind or by an actor, newest first | `GET /changes` | read only |
| `write` | `put`, `patch` or `delete` one record, with `ifVersion` and an idempotency key | the record path, `POST /records` | destructive |
| `decide` | accept or reject a change request | `PATCH` the request | destructive |
| `apply` | declare or change vocabulary; `plan: true` previews; a lossy change is the owner's | `POST /vocabulary/apply`, `/plan`, `GET /vocabulary/upgrade` | destructive |
| `packages` | the catalog, installing a provider, importing a sample, each package's status, enable, disable, bind an input | `/catalog…`, `…/bundle/…` | not destructive |
| `call` | run a function, for at most four minutes | `POST …/function/{name}/call` | not destructive |
| `chat` | talk to an agent, stop its turn; answers the reply and the thread | `POST …/agent/{name}/chat`, `…/thread/{id}/stop` | not destructive |
| `triggers` | status, wake (a sync now), replay, run on a record, parked deliveries, retry, forget | `…/trigger/…`, `/sync/status` | not destructive |
| `files` | read a blob by digest, store one | `GET`, `PUT /blobs` | not destructive |
| `units` | the unit table, a conversion | `/units`, `/units/convert` | read only |
| `merge` | merge or split two records | `POST /merge`, `/split` | destructive |

`chat` reads the turn's event stream to its end, or to 50 seconds, and
answers the reply so far with the thread id; a longer turn keeps running
detached
([0169](../decisions/0169-a-chat-turn-runs-detached-from-its-client-and-its-thread-is-attached-by-id.md))
and `query` reads its thread. The in-process writer flushes, so the stream
runs. Connecting a provider account needs the owner's browser, so the
`triggers` and `packages` answers name the console's Providers page.

## History names the assistant

History reads change runs, grouped by actor, and Claude Code and Codex are
both `app:local`. A run therefore also splits on the token behind its
changes where the actor is an app's or `api`, and carries
`principalName`: the token's claimed name or label, resolved by the server
from the token record, a deleted one included, so a revoked assistant is
still named. It is on the owner's reads alone; the `mcp` wrapper and an
app's scoped read never carry it. The console names such a run "Claude
Code, an app on this device".

## Console

- **Settings → AI assistants** (`/settings/assistants`): the address with
  Copy; one tab per client (Claude Code, Codex, Claude, Cursor, Other) with
  its exact steps, Codex's `codex mcp login substrate` after a revoke or a
  lapse among them; what an assistant can and cannot do, code reaching
  secrets included; the connected assistants (tokens whose scopes are
  `["mcp"]`), each with its name, its app, when it was approved, when it
  stops working, and **Revoke…**. The Cursor and Other tabs mint a token
  for a pasted header and show it once.
- **The approval page** words an `mcp` request: where it works (this
  server's MCP address and nowhere else), what it can and cannot do, that
  code it writes runs as the owner's, that its changes appear as the
  assistant, and the shared-callback warning above.
- **Settings → Apps** leaves `mcp` tokens to the new page.

## Discovery

`server.json` gains `surfaces.mcp` (`endpoint: /mcp`) and the feature
`mcp`, `alpha`, surface `mcp`.

## Tests

- `internal/mcp`: the JSON-RPC shapes, versions, `server/discover` before
  the header rule, notifications, a tool's error result, the size refusal,
  each tool's requests against a fake API.
- `internal/api`: the challenge on every method and the `/mcp` metadata;
  `resource` `<issuer>/mcp` and an absent scope through authorize and
  token; `mcp` refused beside another scope; an app's `full` token refused
  at an `mcp` approval; the stored `["mcp"]`; a non-`mcp` token refused at
  `/mcp`; the token refused off the door and on an unlisted route pattern
  (an `authorizations` path among them); the auth kinds' writes and the
  accept refused; the webhook key absent; uninstall and purge refused; the
  actor; `principalName` on the owner's runs and not an app's.
- The whole flow with the real Claude Code and Codex client documents as
  fixtures of the fake fetcher.
- The console: the page, the scope words, the approval page's `mcp` copy,
  History's name.

## Out of scope

The 2026-07-28 stateless revision (`server/discover`) until it is final;
resources, prompts and elicitation; dynamic client registration (Cursor
uses a pasted token until it reads client ID metadata documents); refresh
tokens; a local `substratectl mcp` over stdio; holding code an assistant
declares back from secrets.

## Review record

One review (Claude Opus 5.5) of the first draft, on 2026-11-06. Folded in:
a tool's path could reach any route the door mark admitted (the
`authorizations` routes among them), so the mark holds route patterns and a
kind is held to three segments; the scope word's storage, and every `full`
special case given an `mcp` twin; `/mcp` admits `mcp` tokens alone; the
rollback note; History's runs split on the token with a server-resolved
name; `server/discover` answered before the header rule; the table's sync
is `wake`, not `run`; the missing tools (files, triggers, packages, units,
stop); the body limit; explicit hints; paging instead of cutting; the
shared-callback warning. The review also showed that code an assistant
declares can reach secrets through a spelled-out `permissions` entry, an
injecting package input or an accepted change request; the owner kept the
assistant's reach and took the honest page over guards (above).
