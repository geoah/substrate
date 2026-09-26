---
type: fix
---

# Slack, Beeper and Notion sync while `syncRequestedAt` is unacknowledged

The on-demand triggers of the Slack (package version 10), Beeper (17) and
Notion (15) bundles fire while the account's `syncRequestedAt` differs from its
`syncRequestedAck`, the rule the Google, GitHub and Linear bundles already
follow. They used to fire on `syncRequestedAt > lastSyncedAt`, which CEL
compares as strings. A Slack request stamped in the same second as the
whole-second `lastSyncedAt` sorted below it (`"…:30.84Z" < "…:30Z"`) and never
ran, so a Sync now pressed within a second of a finished run did nothing.

A request that a bounded or failed run did not acknowledge stays open: the next
write to the account by anyone other than the sync fires the trigger again.

```bash
substratectl patch providers.substrate.reamde.dev/slack/account/owner \
  --prop syncRequestedAt=2026-09-26T19:28:30.844008Z
# fires even when lastSyncedAt is 2026-09-26T19:28:30Z
```
