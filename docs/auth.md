# Users, tokens, and actors

A **user** is a human principal: a username, a password, and a TOTP second
factor, all three required. A user owns exactly one **repository**, and a
**token** is how anything reaches it. Both the credential and every token are
ordinary [records](data-model.md) in the repository they belong to, so the
same reads, the same changelog, and the same console pages that show your tasks show
your account too.

## The invite code

One hard-coded **invite code**, configured on the service, is the only door
into a fresh substrate. Registering with it creates the user and, in the same
transaction, the repository, seeded with the shipped vocabulary.

A substrate with no invite code configured is closed to registration:
`/register` and `/register/enroll` answer `501 unsupported`. That is the right
resting state for a substrate that already has its user. The code is compared
in constant time.

Users cannot see each other, and there is no admin user. The operator acts on
the box, through
[substratectl's operator mode](operations.md#operator-recovery) (its "operator
hat", the DSN-speaking side of the CLI).

## Registering

Registration is two calls, and only the second writes anything:

```http
POST /register/enroll
{"inviteCode": "…", "username": "ada"}

→ 200 {"totpSecret": "JBSWY3DPEHPK3PXP",
       "otpauthUri": "otpauth://totp/Substrate:ada?secret=JBSWY3DPEHPK3PXP&issuer=Substrate&algorithm=SHA1&digits=6&period=30"}
```

The caller holds the seed and hands it back with one code, so an abandoned
registration leaves no row to expire and nothing to sweep:

```http
POST /register
{"inviteCode": "…", "username": "ada", "password": "…",
 "totpSecret": "JBSWY3DPEHPK3PXP", "totpCode": "123456", "label": "laptop",
 "authority": "ada.substrate.example"}

→ 201 {"token": {…}, "secret": "substrate_tok_…",
       "authority": "ada.substrate.example",
       "recoveryKey": "AGE-SECRET-KEY-1…", "recoveryPublicKey": "age1…",
       "signingPublicKey": "…"}
```

`authority` is the DNS-style name the repository owns: the home of every kind
its user declares, and what a sample import rewrites a copied closure onto.
Omitted, it defaults to the username under the host the request reached
(`ada.substrate.example` for a request to `substrate.example`), and the
response says what it got. It is lowercase DNS labels with at least one dot,
within the DNS length limits, never under `substrate.reamde.dev` (where the
shipped vocabulary publishes), unique across the substrate, and permanent
([decision record 0046](decisions/0046-a-repository-owns-one-authority-chosen-at-registration.md)).
The repository's own `repository` record carries it, so a client that only
speaks the record API can read it back.

A request that names no `recoveryPublicKey` asks the server to mint the
recovery pair, and the response carries the age identity exactly once,
beside the token secret: that `recoveryKey` is what opens the repository's
`recoverykey` record in a backup, and the substrate never stores it. A
client that generated its own pair sends `recoveryPublicKey` instead and no
key comes back.

`signingPublicKey` is the repository's Ed25519 changelog-signing public key,
in hex. No private key material rides this response: the signing seed stays
sealed server-side, and the public key is what
`repository verify --expect-public-key` checks a backup's signatures against
([the chain](changelog.md#the-chain)).

That second call is **one creation act**: the seed of the shipped vocabulary,
the sealed material, the credential record and the first token all commit as
one transaction in the new repository's changelog, and the control-plane row that
_is_ the user is written last, so the user exists exactly when the repository
is already complete. Anything that fails before that erases what it wrote: a
failed registration creates nothing, and no order of failures leaves a
half-created user.

Registration therefore ends logged in — the response carries the first token's
secret. Usernames match `[a-z][a-z0-9]{1,29}`, are unique across the
substrate, and are permanent.

## Logging in

A login presents both factors directly and mints a token record:

```http
POST /login
{"username": "ada", "password": "…", "totpCode": "123456", "label": "laptop"}

→ 201 {"token": {"id": "…", "label": "laptop", "createdAt": "…"},
       "secret": "substrate_tok_…"}
```

**There is no session concept beside the token.** A session _is_ a token
record: the console holds one exactly like a script does, logging out revokes
it, and there is no sessions table to reap. One consequence worth stating: a
password change does not sign anything out. Live tokens survive it, because a
token is data access and the credential is the account.

Every failure — an unknown username, a wrong password, a wrong code — answers
one identical `401`, and the engine does the same password-hashing and HMAC
work on all three so timing is not an oracle either.

## Tokens

A token is a record of kind `core.substrate.reamde.dev/token` carrying a `label`, an
optional `expiresAt`, and the SHA-256 of its secret. Nothing on it records
use: authentication is a read, so a busy token never appends to the changelog.
The secret itself is `substrate_tok_` followed by 40 hex characters, shown
**exactly once** at mint. The prefix is there so leak scanners match it with no
false positives.

```http
POST /tokens          # authenticated mint
{"label": "backup", "expiresAt": "2027-01-01T00:00:00Z"}
→ 201 {"token": {…}, "secret": "substrate_tok_…"}

GET    /tokens        # → 200 {"items": [ … ]} — metadata only, never a hash
DELETE /tokens/{id}   # revoke
```

Four things follow from a token being a record:

- **A token has full access to its repository.** There are no scopes, no
  roles, no ACLs, and no actor set on the token. Authentication is a hash
  lookup that finds the token record, and the repository holding that record
  _is_ the request's scope.
- **Revoking is deleting the record.** No row means no access. The same write
  reaches from `DELETE /tokens/{id}`, from the generic record delete at
  `DELETE /api/v1/core.substrate.reamde.dev/token/{id}`, from the console, or from
  `substratectl token revoke`.
- **Expiry is optional and server-enforced.** A token past its `expiresAt`
  fails authentication with an `auth` error, no revoke step needed. A token
  without one lives until it is deleted.
- **They list and read like anything else.** `GET …/core.substrate.reamde.dev/token` is
  an ordinary collection read, and every mint and revocation is a row in the
  [changelog](changelog.md).

Present one on every request under `/api`:

```http
Authorization: Bearer substrate_tok_…
```

## The credential, and the password-factor rule

The user's own auth material is one record,
`core.substrate.reamde.dev/credential` at id `self`. It carries the username and two
secret-typed references into the repository's sealed store — one for the
password hash (argon2id), one for the TOTP seed and its replay counter. **The
material itself never enters the changelog or a record's data**, so the changelog shows
"the credential changed at T" and nothing crackable, and a rotation deletes the
old sealed rows in the same transaction rather than piling old hashes into an
append-only sequence.

Three endpoints change auth material, and all three obey one rule:

```http
POST /password        # {"username","password","totpCode","newPassword"} → 200
POST /totp/enroll     # {"username","password","totpCode"} → 200 {totpSecret, otpauthUri}
POST /totp            # + {"newTotpSecret","newTotpCode"} → 200
```

**The password-factor rule: changing auth material requires the current
password and TOTP code, presented directly in the request body. A bearer token
alone is refused — `403 forbidden`, because the endpoint does not accept tokens
at all.**

This is the one rule that bounds what a leaked token can do. Without it, any
leaked token could rotate the password and outlive its own revocation; with it,
a token's blast radius is the data, never the account. It is also why
`substratectl user password` and `substratectl user totp` send no bearer token at all.

The generic record API cannot touch either auth kind: the credential cannot be
put, patched, or deleted through REST, GraphQL, or the CLI's record surface,
and a token can only be **deleted** there, which is exactly revocation. Both
stay readable and listable. Auth-path writes are attributed to the `substrate`
actor.

TOTP is RFC 6238: SHA-1, six digits, a 30-second step, one step of skew either
way. Codes are one-time — the consumed step is compare-and-swapped under a row
lock, so two requests racing on one code cannot both win.

## Rate limits

Registration and the credential endpoints are the substrate's unauthenticated
write paths beside the [webhook door](api.md#webhooks), so they share one
posture. Attempts are paced to
one per five seconds, keyed by (client IP, username) and by username alone,
under one global bucket 32 attempts wide, so a flood is bounded without an
honest login waiting out somebody else's. There is no failure lockout: a
lockout keyed off the caller is a denial-of-service lever rather than a
defense, so pacing is all there is. One user action counts as one attempt
even when it takes two requests: `/register/enroll` and `/register` share an
attempt, so the pair goes through back to back while the next registration
still waits out the interval. The peer address is the transport's,
never a header's. A refusal is `429 rate_limited` with a `Retry-After`.

Everything authenticated is unmetered.

## Actors are attribution, not authorization

Writes carry an [actor](api.md#actors), and it is worth restating here what it
is not: an actor is attribution, not authorization. A token holds its whole
repository whatever name a request puts in `X-Substrate-Actor`, so the header
unlocks nothing. What it decides is provenance — which property manager a write
records, which rows a function's trigger excludes as its own, and what the changelog
says about who did this.

## The second factor can be switched off locally

`SUBSTRATE_INSECURE_DISABLE_TOTP` stops the substrate verifying codes at all:
login, registration and both credential changes take a username and a password,
and a code sent anyway is ignored. It exists for a substrate on your own
machine that gets wiped daily — every `mise run dev*` task except `dev:totp`
sets it, and nothing else in the tree does. On a reachable deployment it would make a leaked
password the account, which is the whole reason the second factor is there.

What does **not** change: the password is still required and still argon2id, the
[password-factor rule](#the-credential-and-the-password-factor-rule) still
refuses a bearer token as evidence for a credential change, the rate limits are
untouched, and a seed is still minted and sealed with every credential — the
factor is off, not absent.

A deployment states, unauthenticated at
`GET /.well-known/substrate/server.json`, whether it verifies the second
factor:

```json
{ "registration": { "open": true, "totpRequired": false } }
```

The console and `substratectl` read it before they ask a person for anything,
so neither prompts for a code nothing will check; a client that cannot reach
discovery, or one talking to a substrate too old to answer, asks for a code as
before.

Putting the factor back is one restart — but a user who **registered** while it was
off holds no authenticator, because the seed sealed with their credential was
minted server-side and shown to nobody. Reset them (below), or wipe the
database.

## Losing both factors

There is no self-serve recovery. A user who has lost both factors is reset by
the operator, on the box, with `substratectl user reset` — which writes fresh sealed
material and a new credential record and prints a fresh enrollment. The data is
untouched; the account gets new keys. [Running a substrate](operations.md)
covers it.

Next: [GraphQL and search](graphql-and-search.md), the same records at one
endpoint.
