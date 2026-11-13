# Users, tokens, and actors

A **user** is a human principal: a repository name, a password, and, unless
they skipped it at registration, a TOTP second factor
([decision record 0237](decisions/0237-a-second-factor-is-the-owners-choice-and-sign-in-asks-for-the-code-only-of-an-account-that-has-one.md)).
A user owns exactly one **repository**,
whose name is its authority and their login name, and a **token** is how
anything reaches it. Both the credential and every token are
ordinary [records](data-model.md) in the repository they belong to, so the
same reads, the same changelog, and the same console pages that show your tasks show
your account too.

## The invite code

Registering creates the user and, in the same transaction, the repository,
seeded with the `core`, `llm`, `console`, `observations` and `assistant` packages and three keyless `llm/provider` rows
(`openai`, `anthropic`, `gemini`). One **invite code**, configured on the
service as `SUBSTRATE_INVITE_CODE`, gates that door: with one set,
`/register` and `/register/enroll` admit only a request that presents it, and
a wrong one is `401 auth`. The code is compared in constant time.

With no code configured the door reads none: anyone who can reach the
substrate may register, a code sent anyway is ignored, and the boot log says
so. That is the shape of the local substrate — `docker compose up` and every
`mise run dev*` task run it — and the wrong one for anything else. There is
no separate closed state: a substrate that has its user keeps strangers out
with a code nobody is given. A deployment states which door it runs,
unauthenticated, at `GET /.well-known/substrate/server.json`
(`registration.inviteRequired`), and the console and `substratectl` read it
before they ask anybody for a code.

Users cannot see each other, and there is no admin user. The operator acts on
the box, through
[substratectl's operator mode](operations.md#operator-recovery) (its "operator
hat", the DSN-speaking side of the CLI).

## Registering

Registration is two calls, and only the second writes anything:

```http
POST /register/enroll
{"inviteCode": "…", "repository": "ada.example.com"}

→ 200 {"totpSecret": "JBSWY3DPEHPK3PXP",
       "otpauthUri": "otpauth://totp/Substrate:ada?secret=JBSWY3DPEHPK3PXP&issuer=Substrate&algorithm=SHA1&digits=6&period=30"}
```

The caller holds the seed and hands it back with one code, so an abandoned
registration leaves no row to expire and nothing to sweep:

```http
POST /register
{"inviteCode": "…", "repository": "ada.example.com", "password": "…",
 "totpSecret": "JBSWY3DPEHPK3PXP", "totpCode": "123456", "label": "laptop"}

→ 201 {"token": {…}, "secret": "substrate_tok_…",
       "repository": "ada.example.com",
       "recoveryKey": "AGE-SECRET-KEY-1…", "recoveryPublicKey": "age1…"}
```

A registration may skip the second factor instead: `"skipTotp": true` in
place of `totpSecret` and `totpCode`, and no `/register/enroll` call before
it. The account then signs in with its password alone until its owner turns a
factor on ([below](#the-credential-and-the-password-factor-rule)). The skip
is explicit: a registration that sends neither an enrollment nor `skipTotp` is
refused `422`, so a client that lost its enrollment on the way does not create
an account without one by accident.

`repository` is the whole of the name: it BECOMES the repository's authority,
any DNS name its user controls (`ada.example.com`, `geoah.me`), which is the
home of every package its user declares kinds in and the name they log in
with. A name carrying a dot is taken as it is; a bare label is completed under
the host the request reached (`ada` reaching `substrate.example` is
`ada.substrate.example`), and the response says what was created. It is
lowercase DNS labels with at least one dot, within the DNS length limits,
never under `substrate.reamde.dev` (where the shipped vocabulary publishes),
unique across the substrate, and permanent
([decision record 0074](decisions/0074-the-repository-name-is-the-login-name.md)).
The repository's own `repository` record carries it, so a client that only
speaks the record API can read it back.

That one name is everywhere a person or a client names the repository: a
delivery envelope's `repository.authority` carries it and a webhook URL takes
it. The repository's storage is keyed by an id minted at registration
instead (`rnd-…`): the directory under the data root, every table, and the
webhook address the server gives out
([decision record 0261](decisions/0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md)).

A request that names no `recoveryPublicKey` asks the server to mint the
recovery pair, and the response carries the age identity exactly once,
beside the token secret: that `recoveryKey` is what opens the repository's
`recoverykey` record in a backup, and the substrate never stores it. A
client that generated its own pair sends `recoveryPublicKey` instead and no
key comes back.

That second call is **one creation act**: the seed (the `core`, `llm` and
`console` packages and the provider rows), the sealed material, the credential record and
the first token all commit as one transaction in the new repository's changelog, and the control-plane row that
_is_ the user is written last, so the user exists exactly when the repository
is already complete. Anything that fails before that erases what it wrote: a
failed registration creates nothing, and no order of failures leaves a
half-created user.

Registration therefore ends logged in: the response carries the first token's
secret.

## Verify domain

Registration takes the name on its owner's word, so on a shared server
anyone can register a domain they do not hold. **Verify domain** proves it
afterwards
([decision record 0208](decisions/0208-a-repository-proves-its-name-with-a-txt-record-after-registration.md)).
Until it passes, the console marks the name unverified beside it and on an
app's approval page. The console's register page completes a label under the
server's host and takes a domain of the reader's own only through its
advanced option, which says to use a domain they own and previews the record
below; a registration under such a domain lands on Settings, Verify domain.

```http
POST /api/v1/repository/domain

→ 200 {"name": "_substrate.ada.example.com",
       "record": "v=substrate1 server=https://substrate.example.net proof=k3j9x2abcd",
       "proof": "k3j9x2abcd"}
```

Add `record` as a TXT record at `name` in the domain's DNS. The proof is
one-time and stays pending on the repository record (`domainProof`) until a
check passes, so asking again answers the same record. Then check:

```http
POST /api/v1/repository/domain/verify

→ 200 {"verified": true, "verifiedAt": "2026-11-02T12:00:00Z"}
```

The check reads `_substrate.<repository>` and holds its `proof=` to the
pending one; the proof is the evidence, since only the name's controller can
publish it. Then it confirms that `server=` reaches this server: with
`SUBSTRATE_PUBLIC_URL` set, `server=` must equal that origin exactly, and in
every case the check fetches `server=`'s `/.well-known/substrate/server.json`
and compares its `server.instance` with this server's, an id each server
mints once into a file under its data root. This is a consistency check, not
authentication of the server: a proxy in front of this server relays its
document. A pass stamps `domainVerifiedAt` on the repository record and
spends the proof; the record may then keep `server=` alone. A failure
answers `verified: false` and a `reason` naming what to change: no record, a
record that does not parse, the proof differs, `server=` names another
substrate, or the lookup failed. An earlier stamp stands through a failed
check, and a server that lost its data root (and so minted a new instance
id) keeps the stamps it wrote.

A name of exactly one label under the host of `SUBSTRATE_PUBLIC_URL`
(`ada.substrate.example.net` on `https://substrate.example.net`) passes with
no record. The request's `Host` header never counts, so with no public URL
configured every name needs the record. The check never dials a loopback or
private address, so a development server verifies only names under its
public URL's host. Both routes refuse a scoped app token; a full-access
token is a sign-in and acts as the owner
([decision record 0204](decisions/0204-an-app-may-ask-for-full-access-and-its-owner-approves-it-whole.md)).
Settings shows the record to copy and a **Check** button, and
[`substratectl domain`](substratectl.md#verifying-the-repositorys-name) runs
the same two calls.

## Logging in

A login presents the factors directly and mints a token record:

```http
POST /login
{"repository": "ada.example.com", "password": "…", "totpCode": "123456", "label": "laptop"}

→ 201 {"token": {"id": "…", "label": "laptop", "createdAt": "…"},
       "secret": "substrate_tok_…", "repository": "ada.example.com"}
```

**There is no session concept beside the token.** A session _is_ a token
record: the console holds one exactly like a script does, logging out revokes
it, and there is no sessions table to reap. One consequence worth stating: a
password change does not sign anything out. Live tokens survive it, because a
token is data access and the credential is the account.

Every failure (an unknown repository, a wrong password, a wrong code) answers
one identical `401 auth`, and the engine does the same password-hashing and
HMAC work on all three so timing is not an oracle either.

Sign-in is two steps for an account with a second factor. A login with the
right password and no code answers `401` with the code `totp_required`
instead, and the client asks for the code and sends the login again with it.
An account without a factor signs in at the first step, and a code it is sent
is ignored. The `totp_required` answer comes only after the password
verified, so a caller without the password learns nothing from it; a caller
with the password learns that it is right and that the account has a factor.
The credential changes below answer a missing code the same way.

## Tokens

A token is a record of kind `substrate.reamde.dev/core/token` carrying a `label`, an
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

Five things follow from a token being a record:

- **A token without `scopes` has full access to its repository.** There are
  no roles, no ACLs, and no actor set on the token. Authentication is a hash
  lookup that finds the token record, and the repository holding that record
  _is_ the request's repository
  ([0143](decisions/0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md)).
  Every session, script and device token is one of these. A token an app got
  through the authorization flow carries the `scopes` the owner approved and
  reaches only those kinds, on the records route (lists, window and
  aggregate reads, every search mode), the record path, the change feed and
  the blobs its records name, plus the functions a `call:` scope names
  where its scopes cover their reach, and the unit reads (`/api/v1/units`,
  `/api/v1/units/convert`), which no scope limits
  ([apps](apps.md#use-the-token),
  [0194](decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md),
  [0219](decisions/0219-an-apps-token-reads-windows-aggregates-semantic-search-blobs-and-the-feeds-history-over-the-kinds-it-reads-and-calls-the-functions-its-call-scope-names.md)).
- **Revoking is deleting the record.** No row means no access. The same write
  reaches from `DELETE /tokens/{id}`, from the generic record delete at
  `DELETE /api/v1/substrate.reamde.dev/core/token/{id}`, from the console, or from
  `substratectl token revoke`.
- **Only the owner revokes a token.** The engine refuses a token delete from
  installed code with `403 forbidden`, whatever its grant names: a
  function's `delete` effect, an agent's `write` built-in, and a policy
  judge's accept of a proposed delete all run at the bundle tier. A judged
  proposal escalates to the owner's review instead, and the owner's own
  accept revokes. The loader refuses a function or agent whose
  `permissions.writes` names `substrate.reamde.dev/core/token`, `/credential`
  or `/recoverykey`, and a stored one quarantines its package at the next
  open.
- **Expiry is optional and server-enforced.** A token past its `expiresAt`
  fails authentication with an `auth` error, no revoke step needed. A token
  without one lives until it is deleted. Login and registration mint theirs
  without one, so a session lasts until it is signed out
  ([0143](decisions/0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md)).
- **They list and read like anything else.**
  `GET /api/v1/records?filter={"kinds":["substrate.reamde.dev/core/token"]}`
  is an ordinary list, and every mint and revocation is a row in the
  [changelog](changelog.md).

Present one on every request under `/api`:

```http
Authorization: Bearer substrate_tok_…
```

## The credential, and the password-factor rule

The user's own auth material is one record,
`substrate.reamde.dev/core/credential` at id `self`. It carries the repository it admits to and
secret-typed references into the repository's sealed store — one for the
password hash (argon2id), and one for the TOTP seed and its replay counter
while the account has a second factor. `totpRef` is absent from an account
without one, which is how a client reads whether it has one. **The
material itself never enters the changelog or a record's data**, so the changelog shows
"the credential changed at T" and nothing crackable, and a rotation deletes the
old sealed rows in the same transaction rather than piling old hashes into an
append-only sequence. A backup taken while the old password or TOTP seed was
current still holds it
([what older backups keep](operations.md#a-rotated-or-deleted-secret-stays-in-older-backups)).

Three endpoints change auth material, and all three obey one rule:

```http
POST /password        # {"repository","password","totpCode","newPassword"} → 200
POST /totp/enroll     # {"repository","password","totpCode"} → 200 {totpSecret, otpauthUri}
POST /totp            # + {"newTotpSecret","newTotpCode"} → 200
```

Deleting the repository takes the same factors, and nothing else:

```http
POST /repository/delete  # {"repository","password","totpCode"} → 204
```

It erases every record, token and file the repository holds and frees the
name for the next registration
([0236](decisions/0236-an-owner-may-delete-their-repository-and-the-name-is-freed.md)).
The console offers it under Settings, Danger zone.

**The password-factor rule: changing auth material requires the current
password, and the current TOTP code where the account has a second factor,
presented directly in the request body. A bearer token alone is refused —
`403 forbidden`, because the endpoint does not accept tokens at all.** A
request with the right password and no code, on an account that has a factor,
answers `401 totp_required`, as a login does.

Turning a factor on, for an account registered without one, is the same two
calls that replace one: `/totp/enroll` with the password and no code, then
`/totp` with the password, the candidate seed and one code from it. The
console's Settings offers it as **Turn on**. There is no call that turns a
factor off again; the operator's [reset](#losing-both-factors) issues a new
one.

This is the one rule that bounds what a leaked token can do. Without it, any
leaked token could rotate the password and outlive its own revocation; with it,
a token's blast radius is the data, never the account. It is also why the
console's Settings page sends no bearer token at all on either change.

The generic record API cannot touch either auth kind: the credential cannot be
put, patched, or deleted through REST or the CLI's record surface,
and a token can only be **deleted** there, which is exactly revocation. Both
stay readable and listable. Auth-path writes are attributed to the `substrate`
actor.

TOTP is RFC 6238: SHA-1, six digits, a 30-second step, one step of skew either
way. Codes are one-time — the consumed step is compare-and-swapped under a row
lock, so two requests racing on one code cannot both win.

## Rate limits

Registration, login and the credential endpoints are the substrate's
unauthenticated write paths beside the [webhook door](api.md#webhooks), so they
share one posture. Attempts are paced to
one per five seconds, keyed by (client IP, repository) and by repository alone,
under one global bucket of 32 attempts per five seconds for the whole
substrate. One user action counts as one attempt
even when it takes two requests: `/register/enroll` and `/register` share an
attempt, so the pair goes through back to back while the next registration
still waits out the interval. The peer address is the transport's,
never a header's. A refusal is `429 rate_limited` with a `Retry-After`.

There is no failure lockout: a
lockout keyed off the caller is a denial-of-service lever rather than a
defense, so pacing is all there is. The global bucket is the cost of that
choice. A client that names a different repository on each attempt is held
back by the global bucket alone, so a sustained flood from anywhere drains it,
and every login, registration and credential change answers `429` until the
flood stops. Behind the reverse proxy every caller has the proxy's address
([TLS and the reverse proxy](operations.md#tls-and-the-reverse-proxy)), so
no per-address limit could tell the flood from an honest login.

Each attempt runs argon2id at 64 MiB, an attempt naming a repository that
does not exist included. The server runs at most one per two CPUs at once
(between one and four) and queues the rest, so a burst of attempts costs
latency, not memory.

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
login, registration and both credential changes take a repository and a password,
and a code sent anyway is ignored. It exists for a substrate on your own
machine that gets wiped daily — `compose.yaml` and every `mise run dev*` task
except `dev:totp` set it, and nothing else in the tree does. On a reachable
deployment it would make a leaked password the account, which is the whole
reason the second factor is there.

What does **not** change: the password is still required and still argon2id, the
[password-factor rule](#the-credential-and-the-password-factor-rule) still
refuses a bearer token as evidence for a credential change, and the rate
limits are untouched. A seed a registration brings is still sealed, so an
account that enrolled one keeps it for the day the flag comes off.

A deployment states, unauthenticated at
`GET /.well-known/substrate/server.json`, whether it verifies the second
factor:

```json
{ "registration": { "inviteRequired": false, "totpRequired": false } }
```

The console and `substratectl` read it before they ask a person for anything,
so neither prompts for a code nothing will check; a client that cannot reach
discovery, or one talking to a substrate too old to answer, asks for a code as
before.

Putting the factor back is one restart. A user who **registered** while it
was off without bringing a seed has no second factor, exactly as if they had
skipped it, and signs in with the password alone until they turn one on in
Settings.

## Losing both factors

There is no self-serve recovery. A user who has lost their factors is reset by
the operator, on the box, with `substratectl user reset` — which writes fresh sealed
material and a new credential record and prints a fresh enrollment. The data is
untouched; the account gets new keys. [Running a substrate](operations.md)
covers it.

Next: [the changelog and watch](changelog.md), the one changelog every write
lands in.
