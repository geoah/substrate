---
type: breaking
release: v0.86.0
---

# A webhook fire carries only the headers its trigger lists in `source.webhook.headers`

Anyone with a `substrate.reamde.dev/core/trigger` record whose source is a
webhook is hit. Before v0.86.0 the engine forwarded a fixed provider list
(`x-github-event`, `stripe-signature`, `x-slack-signature`, the Pebble app's
`x-index-*` and others). From v0.86.0 a fire, and its parked copy, carries
`content-type`, `content-length`, `content-encoding`, `user-agent`, `date`
and the names the trigger declares, nothing else. An undeclared header reads
as absent in the callable's `request.headers`. The trigger still fires.

Before, a callable reading `x-github-event` worked with:

```yaml
source:
  webhook: {}
```

After, it needs:

```yaml
source:
  webhook:
    headers:
      - x-github-event
      - x-hub-signature-256
```

Names match case-insensitively, at most 32. Declaring `authorization`,
`proxy-authorization`, `cookie`, `set-cookie`, `host` or
`transfer-encoding` is refused at write time.

## What to do

1. List the triggers: `substratectl get substrate.reamde.dev/core/trigger -o yaml`.
2. For each record with `source.webhook`, find the headers its callable reads
   and add them under `source.webhook.headers`.
3. Apply each edited record with `substratectl apply -f <file>`.
4. For a trigger shipped by a sample (the Pebble sample declares its own
   eight names), re-importing the sample writes the list, but it discards a
   hand-set `key`, so set `key` again afterwards.
