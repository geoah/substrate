---
status: accepted
date: 2026-11-08
decision-makers: George Antoniadis
amended-by: 0244
---

# 0243. An applet is A2UI screens the server runs, seeded in every repository, and the console gives applets a place

## Context and Problem Statement

The owner asks Moss for "an app for my books" and wants something to use on
a phone, not a collection table. Two proposals came before this one:
declarative views as records ([#532](https://github.com/geoah/substrate/pull/532))
and code-first TSX modules behind a sandboxed frame
([#533](https://github.com/geoah/substrate/pull/533)). Neither renders on a
server, so neither can later open standalone on a scoped token or be shared
without handing a client a query API. [0130](0130-the-console-serves-four-things-and-its-navigation-follows-them.md)
said apps built through an agent are packages like any other and need no
place of their own. The design and the live runs are in
[the applets plan](../plans/applets.md).

## Considered Options

- Proposal A: declarative view and app records drawn by the console.
- Proposal B: an app is TSX run in an opaque-origin frame with an SDK.
- An applet: a record whose screens are [A2UI](https://a2ui.org) v1.0
  components and whose sources and actions the server runs.

## Decision Outcome

Chosen: an applet, because a closed component catalog is what a model
writes and the server checks best, and a server that runs the applet leaves
a client nothing but drawing what it is sent and reporting named actions.
A's catalog was the console's own, and B's code cannot be checked or
shared.

- **A seeded package, outside core.** `substrate.reamde.dev/applets`
  declares `applet` (`internal`) and the `preview` host function. It is
  seeded into every repository, as the assistant is
  ([0241](0241-the-assistant-ships-with-the-binary-and-the-owner-shapes-it-through-instruction-records.md)),
  and advertised as `applets`, alpha, in discovery.
- **The server is the A2UI agent, and a deterministic one.** A tap is a
  read or a write under the token of whoever opens the applet, never a
  model turn. Every write of an applet is checked against the vendored
  A2UI v1.0 catalog and the repository's kinds and functions.
- **No scope writes an applet.** The applets package joins core and llm
  among the packages no app scope writes, because an applet runs under its
  viewer's token and an image URL it builds from a record's values carries
  them to that URL's host. The owner, the assistant's `designer` (which
  names the kind in its writes) and the MCP door
  ([0230](0230-an-assistant-reaches-a-substrate-through-mcp-with-a-token-that-works-there-alone.md))
  write applets.
- **An action is not replayed.** Its steps run without the request's
  idempotency key, and a step that wrote stays written when a later one is
  refused. Replay protection is
  [#1265](https://github.com/geoah/substrate/issues/1265).
- **Images come through the server.** `GET /api/v1/images` fetches an https
  image through egress's confined client for the owner's token, so the
  console's `img-src` stays its own origin.
- **This amends 0130.** The console gives applets a place, **Applets**, after
  All data, and draws one full screen at `/a/{id}`. An applet is not a
  package's collection: it is a thing the owner opens, as an agent is.

### Consequences

- Good, because one chat message builds a working applet, and the check
  and the designer's preview catch its mistakes before the owner sees one.
- Good, because the same server-run applet can later open standalone on a
  token that reaches its runtime alone, and be shared, without a client
  query API.
- Good, because an assistant at the MCP door builds applets with `write`
  and the `applets` tool's guide and preview.
- Bad, because A2UI v1.0 has no conditional visibility, no join and no
  label lookup; the runtime's `display` values cover the common cases, and
  the rest needs a catalog extension.
- Bad, because rolling the binary back keeps every seeded declaration (the
  boot upgrade never downgrades): the designer stays declared and its
  `preview` tool is refused at dispatch, and the applet records stay with
  no route to render them. An operator who rolls back tells the owner
  applets are paused; the records are untouched and render again on the
  next upgrade.
- Bad, because an image host still learns the images an applet names; a
  host policy is owed before anyone but the owner, the designer and the MCP
  door writes one.

### Confirmation

`internal/applet`'s tests (the check, the runtime, display values, the
guide naming every op), `internal/scope`'s refusal of `write:` on the
applets package, `internal/api`'s applet, preview, MCP and image proxy
tests, `TestDiscoveryStampsEachFeatureStability`, and
`internal/engine/applets_db_test.go` (the check at the write, the designer
writing and previewing, preview reads held to the agent's).

## More Information

[The applets plan](../plans/applets.md) holds the record's format, the runs
and what comes next. Revisit when applets open standalone or are shared:
both need a token whose audience is one applet's runtime, which this record
does not decide.
