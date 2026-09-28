---
type: feature
---

# `SUBSTRATE_TRIGGER_INTERVAL` sets the trigger dispatcher's tick

The dispatcher that checks every repository for a trigger due to run ticked
every 5 seconds, fixed in the binary. `SUBSTRATE_TRIGGER_INTERVAL` now sets
that tick; unset, it is still `5s`. The tick bounds how long a record write
waits for the delivery it fires, so a test suite that writes a record and
polls for the run wants a short one, and a host with many repositories may
want a longer one. Zero or a negative duration refuses the boot, naming the
variable.

```sh
SUBSTRATE_TRIGGER_INTERVAL=1s bin/substrate
```

`mise run test:e2e` and the provider suite (`internal/providere2e`) start
their servers with `1s`. A deployment that sets nothing sees no change.
