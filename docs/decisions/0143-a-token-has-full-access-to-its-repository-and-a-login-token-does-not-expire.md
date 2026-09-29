---
status: accepted
date: 2026-09-29
decision-makers: George Antoniadis
---

# 0143. A token has full access to its repository, and a login token does not expire

## Context and Problem Statement

This record is written after the fact, from `internal/engine/auth.go`,
`kinds/substrate.reamde.dev/core/token.yaml` and [auth.md](../auth.md) as
they stand on 2026-09-29
([#138](https://github.com/geoah/substrate/issues/138)). A token is a record
of `substrate.reamde.dev/core/token` with a `label`, the SHA-256 of its secret
and an optional `expiresAt`; the kind declares no `scopes`. `Authenticate`
checks the secret's shape, finds the live token record by that hash, refuses
it past its `expiresAt`, and hands back the dataset of the repository holding
it. No scope, role or ACL check follows. `Login` mints with
`ds.MintToken(ctx, label, nil)` and registration with
`t.mintToken(label, nil)`, so every session token has no `expiresAt`.

Both facts were stated in `docs/auth.md` and in comments (the token kind,
`TokenInfo`, `internal/api/core.go`, `internal/engine/write.go`), and in no
decision record. A scoped-token or session-expiry design had no record to
supersede, and could change a default that the console, `substratectl` and
every script already depend on.

## Considered Options

On access:

- A token without `scopes` has full access; a later `scopes` property is
  optional, and its absence keeps full access
- Add `scopes` now
- A later `scopes` property whose absence means a narrow default

On login expiry:

- Login and registration tokens stay open-ended; an expiry is opt-in, at
  `POST /tokens` or `substratectl token create --expires`
- Login mints with a default `expiresAt`

## Decision Outcome

Chosen: a token has full access to its repository, the absence of `scopes` is
what means full access, and a login token stays open-ended.

Full access holds because a repository is single-user: one name, one password
and TOTP, no sharing and no roles. A narrower token would protect the owner
from their own scripts and devices, and nothing in the tree needs that yet.
When something does, the retrofit is an optional `scopes` property on the
token kind, and a token record without it keeps full access. Every token
minted before the retrofit has no `scopes`, and the console, `substratectl`
and every script that installs a bundle through `vocabulary/apply` or a
catalog import run on such tokens. A narrow default would cut all of them off
at the upgrade. An optional property is an additive change the boot upgrade
admits; adding `required` without a `default` is a narrowing it refuses while
live records lack the value.

What bounds a token today is the password-factor rule: `/password`,
`/totp/enroll` and `/totp` refuse a bearer token and demand both factors, so
a leaked token cannot change the password or the second factor. It can still
mint more tokens through `POST /tokens` and delete any token record. The
generic record API may only delete a token record, and the seeded `core`
package is not writable by a token.

Login tokens stay open-ended because a default expiry would sign the console
out on a schedule (it drops its session on a `401`) and break every context
`substratectl login` stored, with nothing gained while every token has full
access: a stolen token that expires in 30 days can mint an open-ended one
through `POST /tokens` before it lapses. Revocation is the control that works
now. `DELETE /tokens/{id}`, the generic
`DELETE /api/v1/substrate.reamde.dev/core/token/{id}`, the console's sign-out,
`substratectl logout` and `substratectl token revoke` all delete the record,
and no row means no access. An owner who wants a token to
lapse sets `expiresAt` at mint, and `Authenticate` refuses it after that
instant.

### Consequences

- Good, because the meaning of a token without `scopes` never changes, so a
  scoped-token design ships without breaking one client, script or stored
  CLI context.
- Good, because the console and the CLI stay signed in until the owner signs
  out, and authentication stays a read: no refresh flow, no renewal write.
- Bad, because a bearer token is arbitrary code execution:
  `POST /api/v1/vocabulary/apply` can declare a function and
  `POST /api/v1/substrate.reamde.dev/core/function/{name}/call` runs it, inside
  the function sandbox, with whatever grants that same holder declared.
- Bad, because a leaked login token never stops working on its own, and a
  password change does not end it. The holder can also mint new tokens and
  revoke the owner's, so recovering from a leak means deleting every token
  minted since, not only the leaked one, and the token record keeps no
  last-used stamp to help.

### Confirmation

In internal/engine, `TestTokenLookupScopesTheRequest` holds that the hash
lookup alone picks the repository, `TestLoginMintsATokenAndSpendsTheCode` that
a login mints an ordinary token record, and `TestTokenExpiryIsServerEnforced`
that an opt-in expiry is enforced. In internal/api,
`TestExpiredTokenRejected` holds the same expiry at the HTTP door and
`TestCredentialChangesRefuseABearerTokenAlone` the password-factor rule. In
cmd/substratectl, `TestLogoutRevokesTheStoredTokenAndForgetsIt` holds that
logout deletes the record. No test asserts that a login token has no
`expiresAt` or that the token kind declares no `scopes`; those two halves are
held by review only.

## More Information

This is the record a scoped-token or session-expiry design supersedes. That
design meets three facts. Authentication writes nothing
(`TestAuthenticationWritesNothing`), so an expiry renewed on use would append
to the changelog on every request. The token kind is in the seeded `core`
package, so `scopes` ships as a core version bump. `POST /tokens` decodes its
body strictly, so a `scopes` field is refused with `400` today and no stored
token holds one.

A function's or agent's kind grant is a separate thing, and
[0080](0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md)
keeps a glob grant off the token kind; neither is a token scope.

Reopen trigger: a token the owner wants to hand to something they do not fully
trust, or a second person in one repository.
