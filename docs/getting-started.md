# Getting started

A fresh substrate is empty: no users, no records, nothing but the vocabulary
compiled into the binary. This page takes it from there to a task you can read
back, in four moves: register, log in, write a record, watch it land.

You need a running substrate and its invite code.
[Running a substrate](operations.md) covers standing one up; if somebody else
runs yours, they hand you the address and the code.

## The invite code is the only door

One **invite code**, configured on the service, admits people. Registering
with it creates a **user** and, in the same transaction, that user's one
**repository**, seeded with the core vocabulary — `core.substrate.reamde.dev`
alone. Everything else, including the task kinds used below, is a
[vocabulary bundle you import](builtin-kinds.md). A substrate with no invite code
configured is closed: `/register` answers `unsupported`, which is exactly the
right state for a substrate that already has its user.

Registration needs three things from you: a username, a password, and a TOTP
second factor. All three are required, and the username is yours permanently.

## Register

The [web console](console.md) serves a registration page at `/register`, and
that is the easiest door. From a terminal, [substratectl](substratectl.md) does the same
thing:

```
substratectl register --server https://substrate.example --username ada
```

It asks the substrate for a TOTP enrollment, prints the `otpauth://` URI and
the seed for your authenticator, and takes back one code along with the
password you choose. Only that second call writes anything, so an abandoned
registration leaves nothing behind. Registration ends logged in: `substratectl` stores
the minted token as a context in `~/.config/substratectl/config.yaml`.

Unattended, bring your own seed and skip the prompts:

```
substratectl register --username ada --invite-code CODE \
    --totp-secret BASE32SEED --totp-code 123456 --password-stdin < password
```

The two HTTP calls underneath, if you would rather drive them yourself. The
first writes nothing:

```http
POST /register/enroll
{"inviteCode": "…", "username": "ada"}

→ 200 {"totpSecret": "JBSWY3DPEHPK3PXP",
       "otpauthUri": "otpauth://totp/Substrate:ada?secret=…&issuer=Substrate"}
```

The second is the whole creation act, one transaction:

```http
POST /register
{"inviteCode": "…", "username": "ada", "password": "…",
 "totpSecret": "JBSWY3DPEHPK3PXP", "totpCode": "123456", "label": "laptop"}

→ 201 {"token": {"id": "…", "label": "laptop", "createdAt": "…"},
       "secret": "substrate_tok_…"}
```

That `secret` is shown exactly once. The substrate keeps only its SHA-256, so
a lost token is revoked and re-minted, never recovered.

## Log in

A login is both factors presented directly, and it mints a token record:

```
substratectl login --server https://substrate.example --username ada
```

```http
POST /login
{"username": "ada", "password": "…", "totpCode": "123456", "label": "laptop"}

→ 201 {"token": {…}, "secret": "substrate_tok_…"}
```

There is no session object beside it. A session **is** a token record: the
console holds one exactly like a script does, and logging out revokes it.
Every request after this carries `Authorization: Bearer substrate_tok_…`, and the
token is what says which repository the request is in — no address anywhere
names it. [Users and tokens](auth.md) is the full picture.

## Write your first record

Registration seeded the core vocabulary only, so the task kinds are not there
yet. Install the bundle that ships them from the catalog built into the binary,
and the collection exists. A catalog id is `{authority}/{name}`, so the slash in
it is percent-encoded to stay one path segment. Tasks name an assignee, so
`people` is admitted first — a bundle whose `requires:` is not met is refused:

```
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/core.substrate.reamde.dev/catalog/people.substrate.reamde.dev%2Fpeople/install

curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/core.substrate.reamde.dev/catalog/tasks.substrate.reamde.dev%2Ftasks/install

substratectl get tasks                     # empty, but the collection is there
```

The console does the same thing from the [Registry](console.md) page, and
`substratectl apply -f bundle.yaml` applies a closure you hold as files —
all three are the same admission.

Add a task. The collection path names the kind, so the body is only the
properties:

```http
POST /api/v1/tasks.substrate.reamde.dev/tasks
Authorization: Bearer substrate_tok_…
{"properties": {"title": "Buy milk", "dueAt": "2026-08-13T09:00:00Z"}}

→ 201 {"id": "kq3v9x2m41pf", "kind": "tasks.substrate.reamde.dev/task",
       "properties": {"title": "Buy milk", "status": "open",
                      "dueAt": "2026-08-13T09:00:00Z"},
       "version": 1,
       "createdAt": "2026-08-12T10:00:00Z", "updatedAt": "2026-08-12T10:00:00Z"}
```

The server assigned the id and the `status` state started at its declared
`initial` value. The same write as a file `substratectl apply` takes:

```yaml
kind: tasks.substrate.reamde.dev/task
data:
  properties:
    title: Buy milk
    dueAt: 2026-08-13T09:00:00Z
```

Read it back, then complete it. A state change is a `patch`, and the
declaration stamps `completedAt` for you:

```
substratectl get tasks kq3v9x2m41pf -o yaml
substratectl patch tasks kq3v9x2m41pf --state status=done
```

## Watch it land

Every committed write appended one entry to the repository's changelog. That changelog is
the truth — the records you just read are its fold — and it streams:

```
substratectl watch
```

```http
GET /api/v1/core.substrate.reamde.dev/changes?watch=1

{"bookmark": 412}
{"seq": 413, "ts": "2026-08-12T10:00:00.183742Z", "actor": "api", "op": "put",
 "kind": "tasks.substrate.reamde.dev/task", "recordId": "kq3v9x2m41pf",
 "payload": {"created": true, "properties": ["title", "dueAt"]}}
```

Without `from`, the stream opens at the current head and tails from there; the
bookmark is the seq it opened at, and passing it back as `from` later resumes
exactly where you stopped. Your repository's early seqs are its seed: the
shipped vocabulary was written into the changelog as ordinary entries when the
repository was created, which is why a first task lands well above 1.

`actor` says which door the write came through. This request named none, so it
is `api`; the console sends `console` and `substratectl` sends `substratectl`.
[The changelog and watch](changelog.md) is the whole contract, including how to
resume without a gap.

## Where to go next

- Declare a kind of your own, or read what the shipped ones say:
  [the data model](data-model.md) and [vocabulary as records](vocabulary.md).
- Query across kinds, or search: [GraphQL and search](graphql-and-search.md).
- Connect a provider, or install an automation:
  [bundles](bundles.md) and the [catalog](bundles-catalog.md).
- Mint a token for a script: `substratectl token create --label backup`.

Next: [the data model](data-model.md), the one shape everything reads and
writes as.
