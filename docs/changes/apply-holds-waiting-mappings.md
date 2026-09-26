---
type: feature
---

# `vocabulary/apply` holds back a mapping whose provider is absent, on request

A batch whose `recordmapping` names a source kind this repository does not
have is refused whole. With `"holdWaitingMappings": true` in the body of
`POST /api/v1/vocabulary/apply`, the door holds back each suggested mapping
(onto the declaring package's own kind from another package's kind) whose
source kind is absent, applies the rest, and lists what it held in
`heldMappings` with state `waiting` (decision record 0106). `POST
/api/v1/vocabulary/plan` takes the same key and previews the batch without the
held mappings; its response does not list them. A mapping whose source is
present but does not fit it still refuses the batch.

```bash
substratectl apply -f people.yaml --hold-waiting-mappings
# recordmapping/ada.example.com/people/slackuserperson held: waits on providers.substrate.reamde.dev/slack/user from providers.substrate.reamde.dev/slack (install it, then apply again)
```

Apply the same files again after installing the provider to land the mapping.
