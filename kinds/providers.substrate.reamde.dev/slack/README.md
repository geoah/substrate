# The Slack bundle

Package `providers.substrate.reamde.dev/slack`: a provider that mirrors one
Slack workspace as Slack's own object model, its people, channels, group DMs,
1:1 DMs, the messages in them and the files they share. It takes a pasted
Slack user token (`xoxp-…`) on the `config` record rather than OAuth. It
reads; it never posts, reacts or joins.

`bundle.yaml` is the closure (the config and account kinds, the six mirrors,
the connector's own cursor kind and the sync function) and `triggers.yaml` is
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

What it writes, where the token comes from, the account's toggles, and how
the walk bounds itself:
[docs/bundles-catalog.md#slack](../../../docs/bundles-catalog.md#slack).
