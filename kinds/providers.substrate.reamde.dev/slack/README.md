# The Slack bundle

Package `providers.substrate.reamde.dev/slack`: a provider that mirrors one
Slack workspace as Slack's own object model, its people, channels, group DMs,
1:1 DMs, the messages in them and the files they share. It takes a pasted
Slack user token (`xoxp-…`) on the `config` record rather than OAuth. The
sync reads; it never posts, reacts or joins. `postmessage` is the one write:
a function a caller invokes, which posts one message with the same token.

`bundle.yaml` is the closure (the config and account kinds, the six mirrors,
the connector's own cursor kind, the sync function and `postmessage`) and `triggers.yaml` is
the delivery wiring. They are the contract; this file is not.

## Kinds

| Kind | What it holds |
| --- | --- |
| `config` | the pasted user token and the pinned API origin |
| `account` | one connected workspace: the toggles, `team`, `user` and the stream state |
| `team` | the workspace, from `team.info` |
| `user` | one workspace member, profile whole, from `users.list` and `users.info` |
| `conversation` | one channel, private channel, group DM or 1:1 DM |
| `message` | one message on the timeline; every subtype is this kind |
| `file` | one file shared in Slack, its metadata only (the bytes are never fetched) |
| `bot` | one bot identity, behind a `bot_id` a message carried |
| `conversationsync` | not a mirror: this connector's per-conversation cursors |

## Live events

`triggers.yaml` ships a webhook trigger, `slack-events` (label "Slack live
events"), at `POST /webhooks/<authority>/slack-events`. A Slack Socket Mode
sidecar, which this repository does not ship, holds the websocket,
acknowledges each `events_api` envelope and POSTs batches of their payloads
with `Content-Type: application/json`:

```json
{"version": 1, "events": [{"type": "event_callback", "team_id": "T…", "event_id": "Ev…", "event_time": 1760000000, "event": {"type": "message", "…": "…"}}]}
```

`slacksync` applies the batch to the mirror rows: messages and replies,
`message_changed` and `message_deleted`, `reaction_added` and
`reaction_removed` (the message's whole set read from `reactions.get`),
pins, conversation creates, renames and archives, member joins and leaves,
`user_change`, `team_join` and the file events. It moves no history cursor
and writes no account property, so the scheduled walk still reads what the
socket missed. A callback whose `team_id` is not the account's workspace is
ignored, and so is an event type the body does not know.

The trigger ships without a key, so the endpoint takes any POST that reaches
the server. A deployment reachable from outside sets `source.webhook.key` on
the trigger record and gives the sidecar
`POST /webhooks/<authority>/slack-events/<key>`. Applying `triggers.yaml`
again replaces `source` and drops the key, so set it again after an apply.
The Slack app the sidecar connects with subscribes to the events above.
`reactions.get` runs with the pasted user token, which needs the
`reactions:read` scope; without it each reaction event is counted as
refused, and the walk still reads reactions when it re-reads a message.

What it writes, where the token comes from, the account's toggles, and how
the walk bounds itself:
[docs/bundles-catalog.md#slack](../../../docs/bundles-catalog.md#slack).
