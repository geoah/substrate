# Connecting an assistant

An assistant such as Claude Code, Codex, Cursor or Claude reaches a
substrate through its MCP address, `https://<server>/mcp`: the Model
Context Protocol's Streamable HTTP transport, with tools for what the
console does. The owner approves it once, on the console's approval page,
and revokes it in **Settings → AI assistants**. The console's page carries
the same steps with this server's address filled in. The design and its
reasons are in [the plan](plans/mcp.md) and
[0230](decisions/0230-an-assistant-reaches-a-substrate-through-mcp-with-a-token-that-works-there-alone.md).

## Connect

**Claude Code.**

```bash
claude mcp add --transport http --scope user substrate https://substrate.example.com/mcp
```

Then `/mcp` in Claude Code, pick `substrate` and choose Authenticate, or
run `claude mcp login substrate`. The browser opens this server's approval
page: sign in, read what the assistant asks for, approve.

**Codex.**

```bash
codex mcp add substrate --url https://substrate.example.com/mcp
codex mcp login substrate
```

Codex does not sign in again on its own: after the owner revokes it, or
when the access lapses, run `codex mcp login substrate` again.

**Claude (claude.ai and Claude Desktop).** Settings → Connectors → Add
custom connector, with the address, keeping "Use Claude's published
identity". The connector reaches the server from Anthropic's network, so
the server must be reachable from the internet.

**Cursor, and any client that cannot sign in.** Make a token for it in
**Settings → AI assistants** (or `POST /tokens` with
`{"label": "Cursor", "scopes": ["mcp"]}`), keep it in an environment
variable, and send it as a header. In `~/.cursor/mcp.json`:

```json
{"mcpServers": {"substrate": {
  "url": "https://substrate.example.com/mcp",
  "headers": {"Authorization": "Bearer ${env:SUBSTRATE_TOKEN}"}
}}}
```

## What an assistant can do

What the owner does in the console: read every record, the token and
credential records among them (their secrets are digests and redacted
values); write any record but those; declare and change kinds, triggers,
functions and agents; install providers and import samples; run functions,
chat with agents, run syncs; merge and split records; read and store files.

What it never does: write, mint or revoke tokens, credentials, secrets or
recovery keys, or accept a change request that would; read a webhook's
key; export the repository; connect a provider account (the owner does
that in the console, since it needs their browser); verify a domain;
answer another app's approval; uninstall or purge a package.

Code an assistant declares runs as the owner's code does, and reaches what
the owner's code may, secrets included.

Its writes are its own in History, under its app's actor: `app:local` for
an assistant on the owner's computer (Claude Code, Codex), the client's
host or verified authority for a hosted one (claude.ai), and `api` for a
token the owner made by hand. Its token works at `/mcp` alone.

## The tools

| Tool | What it does |
| --- | --- |
| `kinds` | the kinds the repository holds, each with its purpose and first sentence |
| `describe_kind` | one kind's declaration |
| `query` | one record or one of its values, a page, a search, a window or an aggregate |
| `history` | the changes to a record, a kind or by an actor, paged by cursor and generation |
| `write` | put, patch or delete one record |
| `decide` | accept or reject a change request |
| `apply` | declare or change vocabulary; `plan` previews |
| `packages` | the catalog, install, import, status, enable, disable, bind, upgrades |
| `call` | run a function |
| `chat` | talk to an agent, or stop its turn |
| `triggers` | status, sync status, wake, run, replay, parked deliveries |
| `files` | read a blob in chunks, or store one |
| `units` | the unit table a page at a time, and conversions |
| `merge` | merge or split two records |
| `applets` | the guide to building an applet, and a preview that checks one and draws a screen ([applets](applets.md)) |

Each tool is a REST request made in process with the assistant's own token,
so every rule of [using a substrate from an agent](for-agents.md) holds,
and a refusal reaches the model as the API's error object. One result
carries at most 32 KiB: a larger answer's long values are cut and named, a
write's answer becomes a receipt of what it wrote, and each read that can
grow pages.

## The protocol

Stateless: one JSON-RPC message per `POST /mcp`, answered as JSON, with no
session and no server stream, so `GET` and `DELETE` with a token are `405`.
`initialize` speaks `2025-11-25`, `2025-06-18` and `2025-03-26`.
`server/discover` (the 2026-07-28 revision) answers method-not-found, and
clients fall back to `initialize`. A request without a token is `401`,
naming `scope="mcp"` and the door's own metadata at
`/.well-known/oauth-protected-resource/mcp`; a token that is not an
assistant's is `403` with `error="insufficient_scope"`. The sign-in is
[app authorization](apps.md) with the `mcp` scope, asked for alone and
approved whole, and `resource` naming the MCP address. There is no refresh
token: the approval page offers "until you revoke it" first.

A binary from before the MCP door fails its whole token list on a stored
`["mcp"]`, so revoke assistants' tokens before rolling back past it.
