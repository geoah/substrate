---
type: fix
---

# A settled schedule fire retires its trigger's older parked fires

When a fire of a schedule trigger settles, the engine deletes every parked
fire of that trigger at or before the settled occurrence, through the same
unpark a retry writes. `GET /api/v1/sync/status`, the trigger status and
`GET /api/v1/substrate.reamde.dev/core/trigger/{id}/parked` then report 0
parked for it, and the console stops showing "N runs failed and are waiting
to be tried again" for a sync that has recovered. Rows parked before the
upgrade clear at the trigger's next settled fire.

Example: `github-scheduled` holds 110 parked fires from 2026-09-22 to
2026-09-28 13:30 UTC. Its 14:30 UTC fire settles `ok`, and the list is empty:

```console
$ substratectl trigger parked github-scheduled
ID  SEQ  FIRE  RECORD  ATTEMPTS  PARKED  RUNNING  ERROR
```

Three kinds of park stay until a person retries or forgets them: a record
trigger's (one record's change), a webhook request's, and an agent run a
server stop interrupted. Decision record 0141 has the reasoning.
