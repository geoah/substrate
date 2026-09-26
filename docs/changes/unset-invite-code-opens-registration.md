---
type: breaking
release: v0.76.0
---

# Unset `SUBSTRATE_INVITE_CODE` opens registration instead of closing it

Before v0.76.0, a server with `SUBSTRATE_INVITE_CODE` unset or empty refused
`/register` and `/register/enroll` with `501 unsupported`. From v0.76.0 the
same server registers anyone who can reach the port and logs a `WARN` at
boot. There is no closed state any more. This hits every operator who
closed registration by unsetting the code after creating their user.

Discovery changes with it. `GET /.well-known/substrate/server.json` drops
`registration.open` and reports `registration.inviteRequired`:

```json
before: {"registration": {"open": false, "totpRequired": true}}
after:  {"registration": {"inviteRequired": false, "totpRequired": true}}
```

`compose.yaml` also changed its defaults: `SUBSTRATE_INVITE_CODE` now
defaults to empty (it was `let-me-in`), and
`SUBSTRATE_INSECURE_DISABLE_TOTP` defaults to `true`, so login takes a
password alone.

## What to do

1. On any server reachable by someone other than you, set
   `SUBSTRATE_INVITE_CODE` to a long random value nobody is given, then
   restart. An empty value is the same as unset.
2. On a compose deployment that is not a laptop, also set
   `SUBSTRATE_INSECURE_DISABLE_TOTP=false` so the second factor is verified
   again. A user who enrolled an authenticator before the upgrade keeps
   using it: the server still holds the sealed seed.
3. In a client, read `registration.inviteRequired` instead of
   `registration.open`, and stop treating `501 unsupported` from the
   register door as "closed".
