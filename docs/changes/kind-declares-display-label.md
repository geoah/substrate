---
type: feature
---

# A kind declares a display label, and the vocabulary read carries it

A kind declaration may carry `label:` with `singular` and `plural`, the words
a client shows a person for one record and for the collection. The kind read
(`KindInfo`) returns it as `label`, absent when the kind declares none. The
shipped Slack `conversation` and `user`, Google `contactgroup` and
`calendarseries`, and the four `*sync` state kinds declare one (decision
record 0106).

```json
{
  "identity": "providers.substrate.reamde.dev/slack/conversation",
  "name": "conversation",
  "label": {"singular": "Channel", "plural": "Channels"}
}
```

A client shows `label.plural` as the collection heading and `label.singular`
for one record, and falls back to humanizing `name` when `label` is absent.
