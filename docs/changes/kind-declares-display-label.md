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

A client reads the kind list with
`GET /api/v1/records?filter={"kinds":["substrate.reamde.dev/core/kind"]}` and
finds the label on each declaration record at `properties.label`:

```json
{
  "id": "providers.substrate.reamde.dev/slack/conversation",
  "properties": {
    "label": {"singular": "Channel", "plural": "Channels"}
  }
}
```

`GET /api/v1/substrate.reamde.dev/core/trait/{id}/implementors` returns the
flat `KindInfo` shape, with the label at top-level `label`. A client shows
`label.plural` as the collection heading and `label.singular` for one record,
and falls back to humanizing the kind's name when `label` is absent.
