# Connecting an assistant

An assistant such as Claude Code, Codex, Cursor or Claude reaches a
substrate through its MCP address, `https://<server>/mcp`: the Model
Context Protocol's Streamable HTTP transport, with tools for what the
console does. The owner approves it once, on the console's approval page,
and revokes it in **Settings → AI assistants**. The console's page carries
the same steps with this server's address filled in. The design and its
reasons are in [the plan](plans/mcp.md) and
[0230](decisions/0230-an-assistant-reaches-a-substrate-through-mcp-with-a-token-that-works-there-alone.md).
The MCP door is experimental: [discovery](api.md#discovery) lists it as the
`mcp` feature, and its tools may change in any release
([stability](stability.md)).

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

Most of what the owner does in the console: read every record, the token
records among them (their secrets are digests and redacted values); write any record but those and the protected
`substrate.reamde.dev/core/secret` records (a secret-typed property of
another kind, such as a provider's `apiKey`, is written like any other);
declare and change kinds, triggers,
functions and agents; install providers and import samples; run functions,
chat with agents, run syncs; merge and split records; read and store files.

What it never does: write, mint or revoke tokens, credentials or secrets,
or accept a change request that would; read a webhook's key; export the
repository; connect a provider account (the owner does that in the console,
since it needs their browser); verify a domain;
answer another app's approval; uninstall or purge a package.

Code an assistant declares runs as the owner's code does, and reaches what
the owner's code may, secrets included.

Its writes are its own in History, made by its app's
[actor record](api.md#actors) on behalf of the owner: the app whose
publisher is `local` for an assistant on the owner's computer (Claude Code,
Codex), the client's host or verified authority for a hosted one
(claude.ai), and the person for a token the owner made by hand. Each write
also links its token, so two assistants on one computer, one local app,
read apart by their names. Its token works at `/mcp` alone.

## The tools

| Tool | What it does |
| --- | --- |
| `overview` | start here: the owner's authority (their own packages are under `~`), each package with its source and kinds, the agents, applets and LLM providers, what waits on the owner, and the guide's topics |
| `guide` | the documentation, one topic at a time (below) |
| `kinds` | the kinds the repository holds, grouped by package, each with its purpose and first sentence; narrowed by package, purpose or words |
| `describe_kind` | one kind's declaration |
| `query` | one record or one of its values, a page, a search, a window or an aggregate |
| `history` | the changes to a record, a kind or by an actor, paged by cursor and generation |
| `write` | put, patch or delete one record |
| `decide` | accept or reject a change request |
| `apply` | declare or change vocabulary; `plan` previews |
| `packages` | the catalog, add, status, enable, disable, bind, upgrades |
| `call` | run a function |
| `chat` | talk to an agent, or stop its turn |
| `triggers` | status, sync status, wake, run, replay, parked deliveries |
| `files` | read a blob in chunks, or store one |
| `units` | the unit table a page at a time, and conversions |
| `merge` | merge two records, or split a merge by its id |
| `applets` | the guide to building an applet, and a preview that checks one and draws a screen ([applets](applets.md)) |

## The guide

An assistant reads what it needs when it needs it, not up front: the
instructions `initialize` carries are a dozen lines (the rules every write
keeps, and which tool to call first), and the rest is the guide, one topic
at a time, a few thousand characters each. `guide` without a topic lists
them, each with what it covers:

| Topic | Covers |
| --- | --- |
| `records` | writing records, change requests, the questions agents ask |
| `query` | filters, search, windows, aggregates, the changelog |
| `vocabulary` | declaring packages and kinds, every property type, changing a kind safely |
| `traits` | what each core trait gives a kind and when to bind it |
| `packages` | the catalog, samples, providers, inputs |
| `agents` | Moss and its helpers, the owner's own agents, LLM providers and models |
| `automation` | functions, triggers and policies |
| `observations` | metrics, measured values and units |
| `console` | the console's record pages and saved views |
| `applets` | building an applet ([applets](applets.md)) |

The topics are files under `internal/mcp/guide/` (the applets topic is
`internal/applet/guide.md`), and the same text is each topic's resource,
`substrate://guide/<topic>`, which Claude Code attaches with
`@substrate:substrate://guide/<topic>`. Tests hold the topics to the door's
tool names, the vocabulary topic to every property type the parser takes,
and the traits topic to every core trait.

## Prompts

A client that offers prompts shows each as a command the owner starts
(Claude Code: `/mcp__substrate__<name>`). Each is a request in the owner's
voice that names the tools and topics the work needs, with at most one
optional argument, since a client may split arguments on whitespace:

| Prompt | Does |
| --- | --- |
| `tour` | what the substrate holds and what waits on the owner |
| `new_collection` | designs a collection, plans it and applies it on the owner's word |
| `new_applet` | builds an applet and previews every screen |
| `inbox` | the change requests and questions waiting on the owner, and what is due |
| `tidy` | finds duplicates, gaps and inconsistent values in one collection |

## Results

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
clients fall back to `initialize`. The methods are `initialize`, `ping`,
`tools/list`, `tools/call`, `resources/list`, `resources/templates/list`
(none), `resources/read`, `prompts/list` and `prompts/get`; the guide's
resources and the prompts are fixed, so neither list changes. A request without a token is `401`,
naming `scope="mcp"` and the door's own metadata at
`/.well-known/oauth-protected-resource/mcp`; a token that is not an
assistant's is `403` with `error="insufficient_scope"`. The sign-in is
[app authorization](apps.md) with the `mcp` scope, asked for alone and
approved whole, and `resource` naming the MCP address. There is no refresh
token: the approval page offers "until you revoke it" first.

A binary from before the MCP door fails its whole token list on a stored
`["mcp"]`, so revoke assistants' tokens before rolling back past it.
