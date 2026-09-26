---
type: breaking
release: v0.93.0
---

# `oauth/start` refuses a bare account id

Clients starting an OAuth connect are hit, including scripts calling
`substratectl bundle connect`. A `POST /api/v1/oauth/start` whose `record`
is an account's bare id is refused with `422` `validation`, and the message
lists the account paths the repository holds under that id. Before v0.93.0
the bare id resolved when one account kind held it. The full record path
has been accepted since v0.85.0 (decision record 0090).

```json
{"record": "owner"}
```

answers:

```
"owner" is a bare record id, and an account is named in full as <authority>/<package>/<kind>/<id>; this repository holds providers.substrate.reamde.dev/google/account/owner
```

The consent callback now carries the path too: the return page's
`postMessage` `record` and the console fallback redirect
`/registry?connected=<path>` hold `<authority>/<package>/<kind>/<id>`, not
the bare id. A consent started under the previous binary fails at the
callback and has to be started again.

## What to do

1. Send `record` as the full path:
   `{"record": "providers.substrate.reamde.dev/google/account/owner"}`.
2. Pass the full path to `substratectl bundle connect`:
   `substratectl bundle connect providers.substrate.reamde.dev/google/account/owner`.
3. If you listen for the `substrate-oauth` message or read `?connected=`,
   expect the full path there.
4. Restart any consent that was in progress during the upgrade.
