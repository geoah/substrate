# Users, tokens, and actors

A **user** is a person, and three things make one
([decision record 0274](decisions/0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md)):

- an **identity**, held by the server beside the repositories: the name the
  person signs in with, a password, and, unless they skipped it at
  registration, a TOTP second factor
  ([decision record 0237](decisions/0237-a-second-factor-is-the-owners-choice-and-sign-in-asks-for-the-code-only-of-an-account-that-has-one.md)).
  It holds no data;
- a **repository**, whose name is its authority, which holds everything the
  person has;
- the person's **actor** in the repository, a `substrate.reamde.dev/core/actor`
  record of type `person` that names their identity: a person is in a
  repository while a person actor there names them, and their writes link
  that actor. The server keeps an index of those actors for the identities
  it holds (`identity_repositories`), which a sign-in and every token are
  checked against. Two rules hold the index to the actors:
  - It is never ahead of them. While the server admits anyone, the index
    names nobody the repository's live person actors do not name. A person
    whose actor a restored history disabled or deleted has no access once
    the boot that reads that history has run, nothing writes that actor
    again, new work that acts for them is refused, what they left running
    is settled at the next open for nobody, and only `user reset` gives the
    repository a person, with an actor of their own. A repository from
    before identities is the one exception, told apart by what it holds (an
    actor kind that declares no identity, a sign-in record, or a line of
    one): it keeps the person its old sign-in named until its first open
    writes that person onto its actor.
  - It is behind them only while the database refuses the write. After a
    person's actor commits, its row of the index is written on a context
    of its own, not the request's, and tried again until it lands or the
    process stops; the next boot derives it.

  Wherever actors arrive another way (an import, a restore, a rebuild) the
  index is derived again from them before the repository is offered to
  anyone, at every boot and at the end of a rebuild. A sign-in names the
  person, who picks the repository it opens.

A person has one repository today, which registration creates and names
after them. A **token** is how anything reaches a repository; every token is
an ordinary [record](data-model.md) in the repository it opens and names the
identity it acts for, so the same reads, the same changelog and the same
console pages that show your tasks show your tokens too. The identity is no
record of any repository, so no repository's history holds the person's
factors.

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
{"inviteCode": "…", "identity": "ada.example.com"}

→ 200 {"totpSecret": "JBSWY3DPEHPK3PXP",
       "otpauthUri": "otpauth://totp/Substrate:ada?secret=JBSWY3DPEHPK3PXP&issuer=Substrate&algorithm=SHA1&digits=6&period=30"}
```

The caller holds the seed and hands it back with one code, so an abandoned
registration leaves no row to expire and nothing to sweep:

```http
POST /register
{"inviteCode": "…", "identity": "ada.example.com", "password": "…",
 "totpSecret": "JBSWY3DPEHPK3PXP", "totpCode": "123456", "label": "laptop"}

→ 201 {"token": {…}, "secret": "substrate_tok_…",
       "repository": "ada.example.com", "identity": "ada.example.com"}
```

A registration may skip the second factor instead: `"skipTotp": true` in
place of `totpSecret` and `totpCode`, and no `/register/enroll` call before
it. The account then signs in with its password alone until its owner turns a
factor on ([below](#the-credential-and-the-password-factor-rule)). The skip
is explicit: a registration that sends neither an enrollment nor `skipTotp` is
refused `422`, so a client that lost its enrollment on the way does not create
an account without one by accident.

`identity` is the name the person signs in with, and the repository
registration creates takes the same name unless the request names another in
`repository`. The repository's name BECOMES its authority, any DNS name its
user controls (`ada.example.com`, `geoah.me`). The packages its user declares
kinds in live in the repository's local namespace, `~`, not under the name
([0259](decisions/0259-a-repositorys-own-kinds-live-in-the-local-namespace.md)). Both names keep that DNS-style form, so
another server could one day vouch for a person's. A name carrying a dot is
taken as it is; a bare label is completed under the host the request reached
(`ada` reaching `substrate.example` is `ada.substrate.example`), and the
response says what was created. A name is lowercase DNS labels with at least
one dot, within the DNS length limits, and unique across the substrate; a
repository's is never under `substrate.reamde.dev` (where the shipped
vocabulary publishes) and is permanent. A request that names no `identity`
is refused `400` with the field to send. The repository's own `repository`
record carries its name, so a client that only speaks the record API can
read it back.

That one name is everywhere a person or a client names the repository: a
delivery envelope's `repository.authority` carries it and a webhook URL takes
it. The repository's storage is keyed by an id minted at registration
instead (`rnd-…`): the directory under the data root, every table, and the
webhook address the server gives out
([decision record 0261](decisions/0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md)).

That second call is **one creation act**: the seed (the `core`, `llm` and
`console` packages and the provider rows), the recovery key and the first
token all commit as one transaction in the new repository's changelog, and
the control-plane rows that make the repository and the person exist are
written last, in one transaction of their own, so the person exists exactly
when the repository is already complete. Anything that fails before that erases what it wrote: a
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

A login names the person, presents the factors directly and mints a token
record in the repository it opens:

```http
POST /login
{"identity": "ada.example.com", "password": "…", "totpCode": "123456", "label": "laptop"}

→ 201 {"token": {"id": "…", "label": "laptop", "createdAt": "…"},
       "secret": "substrate_tok_…", "repository": "ada.example.com",
       "identity": "ada.example.com"}
```

A login that names no `repository` opens the person's one repository. A
person who may open several and names none is answered `409 conflict` with
the reason `repository_required` and the repositories to pick from, and the
code they sent is not spent, so the same request naming one, by its name or
its id, signs in:

```http
→ 409 {"error": {"code": "conflict", "reason": "repository_required",
       "message": "…", "repositories": [{"id": "rnd-…", "name": "ada.example.com"},
                                        {"id": "rnd-…", "name": "notes.example.com"}]}}
```

A repository the person may not open is `403 forbidden`, whether it exists or
not. `GET /identity`, with a full token, reads the person it acts for: the
name, whether their sign-in asks for a code (`secondFactor`) and the
repositories they may open.

**There is no session concept beside the token.** A session _is_ a token
record: the console holds one exactly like a script does, logging out revokes
it, and there is no sessions table to reap. One consequence worth stating: a
password change signs nothing out unless it asks to. Live tokens survive it,
because a token is data access and the credential is the account; a change
that sends `"signOutEverywhere": true` also
[signs out everywhere](#signing-out-everywhere).

Every failure (an unknown name, a wrong password, a wrong code) answers
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
  It authenticates while the person it names may open that repository: one
  a restore brought back naming a person this server does not hold is
  refused until `user reset` gives the repository its person back
  ([below](#losing-both-factors)). A change the server makes to who may open
  a repository (a repository deleted, a person no actor names any more)
  holds every token of that repository to the table from before it commits
  until it is proved to have ended, so it takes effect at once; a change
  made to the tables by hand, outside the server, takes effect within the
  token cache's minute.
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
  `substratectl token revoke`. Every stream the token opened ends too: a
  watch on the records route or the change feed, a chat's stream and a
  thread's events read their token again whenever the repository's changelog
  moves, which a revoke does, and end with an `auth` error (a terminal frame
  on a watch, an `error` event with `"code": "auth"` on an agent stream, and
  a plain `401` for an agent stream whose first event had not gone out). A
  long transfer reads its token between its writes, at most once a second:
  an export or a blob's bytes still going out are cut, which a client reads
  as a body that ended early. [What ends with a token](#what-ends-with-a-token)
  says which responses do which.
- **A person revokes their own tokens; another's is an admin's.** A token is
  sign-in material, which no rule reaches but by naming it and which the
  `admin` role reaches by being `admin` ([roles](#roles)). The engine refuses
  a token delete from installed code with `403 forbidden`, whatever its
  declaration names: a function's `delete` effect, an agent's `write`
  built-in, and a policy judge's accept of a proposed delete. A judged
  proposal escalates to a person's review instead, and an admin's accept
  revokes. The loader refuses a function or agent whose `permissions.writes`
  names `substrate.reamde.dev/core/token` or `/credential`, and a stored one
  quarantines its package at the next open.
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

## Roles

What a person may read and write in a repository is the roles they hold
([0275](decisions/0275-what-a-person-may-do-is-the-roles-they-hold-and-nothing-is-called-owner.md)).
A role is a record of `substrate.reamde.dev/core/role`: a name and a list of
rules, each a list of verbs (`read`, `write`, which includes read) over a
list of kinds, spelled as an app's scope spells them (`*`,
`<authority>/*`, `<authority>/<package>/*`, one kind,
`implements(<trait>)`, `~` for the repository's own authority). Rules only
add. A person's actor names the roles they hold (`roles`), and they are read
on every request, so a role changed or taken away holds from the next one
([0295](decisions/0295-a-persons-token-is-held-to-that-persons-roles.md)).

| Role | May |
| --- | --- |
| `admin` | everything: every kind, the control kinds and sign-in material included, and changing who may do what |
| `editor` | read and write every kind that is not a control kind |
| `viewer` | read every kind that is not a control kind |

The **control kinds** decide what code runs, what it may do, what the
vocabulary is and who may do what
([0276](decisions/0276-one-check-holds-every-hand-that-writes-and-one-list-names-the-control-kinds.md)):
triggers; policies; AI providers and the
models they offer; a package's settings, the kinds its bundle takes as
inputs, and the accounts that connect it; applets; declarations; actors;
and roles. A kind's description says its class (`control`, with `sign-in`
for sign-in material). A rule reaches one only by naming that kind; `*`
never does. `admin` reaches every one by being `admin`. A change request is
not one: deciding it writes its target as the person who decides, so an
editor accepts or adjusts a request only where they could write its
target.

Who may do what is fixed, not a rule: no rule writes an actor or a role, a
person's roles among them. Only an admin changes them, and the last admin
keeps the role. Nothing is called owner: the creator is the first admin,
and every person a repository had before roles became one. At 1.0 nothing
on the network adds a person or changes a role; the operator's interface
does (`AddPerson` with roles, `SetPersonRoles`).

**One check holds every hand.** A write is allowed when the rules of the
hand that writes allow it and, where it acts for someone, that person's
rules allow it too: never more than both. A person's rules are their roles;
an app's token is held to its scopes and to its person's roles; an
assistant's reaches what its person's roles reach but sign-in material; a
function or an agent is held to the writes its declaration lists, naming
each control kind it writes (but a policy, which installed code never
writes), and, read by read and write by write, to the
roles of the person its turn acts for and the grant of the app that drives
the turn, each as it stands at that read or write. So an agent a viewer
starts writes nothing but its own conversation, a role reduced or an app's
token revoked holds from the run's next read or write, and a function a
trigger runs, acting for nobody, keeps its own rules. Answering a run's
question, approving its call, writing its next message or stopping its turn
is admitted only to a hand that could have started that run itself, and the
work it continues is held to that hand too. Whose rules hold a run's work
once its request is over (an approved call, a background job, a resume) is
stored on the run's own records, `heldTo`, which the engine alone writes and
the person's view does not show; work reads its own record's, never the
conversation's latest turn, so a person's message between an app's call and
its approval does not lend the call the person's rules. Work asked before
the release that records it is refused once, and the person asks again. A
refusal by a rule is `403 forbidden` with the reason `not_permitted`.

**What a person who is not an admin reaches.** Every route is public, for
an admin, or held to its rules
([0294](decisions/0294-every-route-is-public-for-an-admin-or-held-to-its-rules.md)).
Such a person reaches the routes an app's token reaches, held by the same
view to their roles instead of a grant: they read and write what their
rules reach, talk to every agent whose reads their rules cover and call
every function whose reach they cover, and a kind their rules do not read is
answered as one the repository never declared; the unit graph is read where
their rules read units and conversions. Their own sign-ins
(`/tokens`) and identity (`GET /identity`, which lists the roles they hold
in each repository) are theirs whatever their roles. The list of kind
declarations holds the kinds their rules read, and they search the history
of the kinds they read whole. Every other route answers them `403
forbidden`, `not_permitted`: merging and splitting records, which rewrite
references in kinds they may not write, and `/sync/status`, whose syncs sit
on account kinds, among them. An app's token that delivers to a webhook
(`webhook:`) acts for its person: it delivers only while they hold a role,
and the delivery's run is held to their roles.

## Signing out everywhere

A full-access token can mint another with no password (`POST /tokens`),
mint an assistant's token, and approve an app, so revoking a token that
leaked does not end the leak: whatever it made meanwhile still works.
Signing out everywhere does. It ends every token the person holds, in every
repository they may open, with each repository's tokens that name no person:

| Token | Ended | Why |
| --- | --- | --- |
| From `/login` or `/register` | yes | It is a sign-in. |
| Minted from another through `POST /tokens` | yes | A leaked token can mint one, so a sign-out that left it would leave the leak a way back. |
| An assistant's, for the MCP door (`scopes: ["mcp"]`) | yes | It reaches everything but the auth material, and a full token can mint one. |
| An app's, from the authorization flow | yes | A full token can approve an app, `full` access included, so the same reason holds. The app asks to be approved again. |

This browser or script is one of them: after a sign out everywhere every
client signs in again. Sign-in tokens still have no expiry; this is how they
end at once.

```http
POST /signout/everywhere
{"identity": "ada.example.com", "password": "…", "totpCode": "123456"}
→ 200 {"identity": "ada.example.com", "signedOut": 4}
```

It takes both current factors in the body and refuses a bearer token, like
the credential changes below: it is how a person takes the account back from
a leaked token, so a token is no evidence for it. A password change does the
same in the same request with `"signOutEverywhere": true`, because the code
it carries is spent once; its answer carries `signedOut` too. Every open
stream of every ended token ends with an `auth` error as soon as the sign-out
commits, and an export or a blob's bytes going out stop between two writes.
Nothing a token set going before it mints afterwards: a token is minted
standing on the token that asked for it (`POST /tokens`) or approved it (an
app's code, exchanged after the approval), checked in the mint's own
transaction to still authenticate (not revoked, not expired, and its person
still one who may open the repository), so a mint whose request was
authenticated before the sign-out, or a code approved before it, is refused.

A sign out everywhere ends every token that exists when it commits. With a
password change it also ends every sign-in still in flight that proved the
old password: every door that proves the factors and then acts (a sign-in,
a credential change, a sign out everywhere, a repository's delete) acts under
the identity's row, the lock every change of the factors takes, and checks
there that they are still the ones it proved. A sign-in that proves the
factors as they stand is a new sign-in, whenever it completes: a sign out
everywhere that changes no factor does not stop it, because whoever can
prove the factors can sign in again a moment later. The console offers it under Settings, Signed in, and as an option of
the password change, and `substratectl logout --everywhere` runs it
([0272](decisions/0272-signing-out-everywhere-ends-every-token-the-person-holds-and-every-stream-ends-with-its-token.md)).

## What ends with a token

A token is read when a request starts. What happens when it ends while the
response is still going out depends on whether the server goes on reading
for that token once the answer has begun:

- **A stream** is a response the server keeps open to send what happens
  later: the watch, and an agent's event streams. It reads its token before
  its first byte, then at every batch and every heartbeat, and whenever the
  repository's changelog moves, and ends when the token is dead: an ordinary
  `401` before its first byte, a terminal `auth` frame or event after it. It
  reads the roles of the token's person with it. A change of them ends a
  watch with no terminal frame, so its client reconnects from its cursor
  onto the roles held now, and an agent stream `unavailable` (an ordinary
  `503` before its first event, an `error` event after it), which a client
  opens again rather than signing in. A read of the token or the roles that
  cannot be made (a database that did not answer) ends either `unavailable`,
  a watch with that terminal frame: no stream goes on under what it read
  before.
- **A long transfer** is a response with no bound on its size: the export
  and a blob's bytes. It reads its token before its headers and then between
  writes, at most once a second, and a dead token, a change of its person's
  roles, or a read of either that cannot be made cuts it, so that no client
  can take it for complete: the connection closes mid-body, which a client
  reads as an unexpected end, and an export's archive lacks its last entry,
  `snapshot.json`.
- **Every other response is answered from what was read when it started.**
  It is authorized then, and it completes: ending a token does not reach
  back into a request already being answered. A stream and a long transfer
  go on reading after their first byte (what happens later; a file, piece
  by piece), which is why they read their token again; nothing here does.

| Route | Class | What it answers from |
| --- | --- | --- |
| `GET /api/v1/records?watch=1`, `GET /api/v1/changes?watch=1` | stream | the changelog as it moves, until its client leaves, the server drains, or its token ends |
| `POST …/core/agent/{name}/chat`, `GET …/llm/thread/{id}/events` | stream | the turn's events as they come, as above; the thread's events with no turn running are one line |
| `GET /api/v1/export` | long transfer | the repository's directory, read file by file as it goes out |
| `GET /api/v1/blobs/{digest}` | long transfer | the blob's bytes, written piece by piece |
| `GET /api/v1/records` | answered from its start | one page, at most 500 records |
| `GET /api/v1/changes` (`from`, `before`, `first`, `runs`) | answered from its start | one page, at most 500 changes |
| `GET`, `PUT`, `PATCH`, `DELETE` on a record, `POST /api/v1/records` | answered from its start | one record |
| `PUT /api/v1/blobs`, `PUT /api/v1/blobs/{digest}` | answered from its start | the stored blob's description |
| `GET /api/v1/images` | answered from its start | one image, at most 8 MiB |
| `POST …/core/function/{name}/call` | answered from its start | the function's reply, one frame of at most 8 MiB |
| `POST …/core/agent/{name}/call` | answered from its start | one turn's result |
| `* /mcp` | answered from its start | one buffered answer; a tool's result at most 32 KiB |
| `POST …/applets/applet/{id}/render`, `/preview`, `/act` | answered from its start | one render or one act |
| `GET /tokens`, `GET …/core/trigger/status`, `GET …/core/trigger/{id}/parked`, `GET /api/v1/sync/status`, `GET …/core/bundle/status`, `GET …/core/trait/{id}/implementors`, `GET /api/v1/units` | answered from its start | one list, unpaged, read whole when the request starts |
| `GET /api/v1/catalog`, `GET /api/v1/catalog/{id}`, `GET /api/v1/vocabulary/upgrade` | answered from its start | the catalog this server ships |
| Every other route that takes a token | answered from its start | one answer to one request: a write, a mint, an approval, an apply |

The routes that take no token (discovery, `/healthz`, `/metrics`, the
console's files, the webhooks, the sign-in and registration doors, the
authorization server's metadata and token exchange) are no token's to end.

## The credential, and the password-factor rule

The person's own auth material is their identity's: the password hash
(argon2id), and the TOTP seed with its replay counter while they have a
second factor, each sealed under a data key of the identity's own, which the
host credential key wraps as it wraps a repository's. **The material never
enters any repository**: no changelog, record or sealed row of a repository
holds it, and a change of it writes the identity row alone, so no
repository's history records one. `GET /identity` says whether the person has
a second factor. A repository registered before identities kept the sign-in
as its `core/credential` record; the boot gives that repository its identity
from it, with the same password and the same seed, and the repository's
first open purges the record. A backup of such a repository taken before
still holds the record's sealed rows
([what older backups keep](operations.md#a-rotated-or-deleted-secret-stays-in-older-backups)).

Three endpoints change auth material, and all three obey one rule:

```http
POST /password        # {"identity","password","totpCode","newPassword"} → 200
POST /totp/enroll     # {"identity","password","totpCode"} → 200 {totpSecret, otpauthUri}
POST /totp            # + {"newTotpSecret","newTotpCode"} → 200
```

Deleting a repository takes the same factors and the repository's name:

```http
POST /repository/delete  # {"identity","repository","password","totpCode"} → 204
```

It erases every record, token and file the repository holds, its archive
included, and frees its name for the next registration
([0236](decisions/0236-an-owner-may-delete-their-repository-and-the-name-is-freed.md)),
so it is an admin's: another person of the repository who proves their own
factors is answered `403 forbidden`, `not_permitted` ([roles](#roles)).
A person whose last repository it was goes with it, and their name is free
too. The console offers it under Settings, Danger zone.

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

This rule keeps a leaked token from taking the account: without it, a leaked
token could rotate the password and lock its owner out. It does not stop the
token from minting more tokens, an assistant's included, or approving an app,
so revoking the leaked token alone does not end the leak;
[signing out everywhere](#signing-out-everywhere) does, and it takes the
factors for the same reason. It is also why the console's Settings page sends
no bearer token at all on any of these changes.

The generic record API cannot touch either auth kind: the credential cannot be
put, patched, or deleted through REST or the CLI's record surface,
and a token can only be **deleted** there, which is exactly revocation. Both
stay readable and listable. Auth-path writes are made by the repository's
`system` [actor](api.md#actors).

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

Every write links an [actor record](api.md#actors), and it is worth restating
here what it is not: an actor is attribution, not authorization. The server
derives it from the token, never from the request: a full-access token
writes as the person, whichever client holds it, and an app's token as its
app, so there is nothing a request could name and nothing a name could
unlock. The write also links the token itself, which is how two sign-ins of
one person read apart. What the actor decides is provenance — which property
manager a write records, which rows a function's trigger excludes as its own,
and what the changelog says about who did this.

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

There is no self-serve recovery. A person who has lost their factors is reset
by the operator, on the box, with `substratectl user reset <name>`, which
seals a fresh password hash and seed on their identity and prints a fresh
enrollment. It writes no repository, so the server keeps running. The data
is untouched; the person gets new keys. Named by a repository nobody can sign
in to (one restored without its sign-in), it gives that repository a person:
the one its person's actor names, under the same id, so the tokens the
directory carried authenticate again. A repository whose actor names a
person this server already holds has that person, whom the boot let in:
the reset is refused, names them, and makes no other.
[Running a substrate](operations.md#operator-recovery) covers it.

Next: [the changelog and watch](changelog.md), the one changelog every write
lands in.
