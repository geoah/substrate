# Getting started

A fresh substrate is empty: no users, no records, nothing but the vocabulary
compiled into the binary. This page takes it from there to a task you can read
back, in four moves: register, log in, write a record, watch it land.

You need a running substrate and its invite code.
[Running a substrate](operations.md) covers standing one up; if somebody else
runs yours, they hand you the address and the code.

## Registration needs an invite code

One **invite code**, configured on the service, admits people. Registering
with it creates a **user** and, in the same transaction, that user's one
**repository**, seeded with the core vocabulary — `core.substrate.reamde.dev`
alone. Everything else, including the task kinds used below, is a
[vocabulary bundle you import](builtin-kinds.md). With no invite code
configured, registration is closed;
[users, tokens, and actors](auth.md#the-invite-code) has the detail.

Registration needs three things from you: a username, a password, and a TOTP
second factor. All three are required, and the username is yours permanently.

## Register

The [web console](console.md) serves a registration page at `/register`, and
that is the easiest way in. From a terminal, [substratectl](substratectl.md) does the same
thing:

```bash
substratectl register --server https://substrate.example --username ada
```

It asks the substrate for a TOTP enrollment, prints the `otpauth://` URI and
the seed for your authenticator, and takes back one code along with the
password you choose. Only that second call writes anything, so an abandoned
registration leaves nothing behind. Registration ends logged in: `substratectl` stores
the minted token as a context in `~/.config/substratectl/config.yaml`. The
repository it created owns an **authority**, a hostname every kind you
declare lives under: any name you control through `--authority`
(`ada.example.com`), else your username under the server's host.

Unattended, bring your own seed and skip the prompts:

```bash
substratectl register --username ada --invite-code CODE \
    --totp-secret BASE32SEED --totp-code 123456 --password-stdin < password
```

Underneath, registration is two HTTP calls, and only the second writes
anything. The response carries the token secret, shown exactly once, and a
recovery key exists either way: `substratectl register` generates the pair
itself and saves the key for you, while a raw HTTP request that names no
`recoveryPublicKey` gets a one-time server-minted `recoveryKey` back.
[Users, tokens, and actors](auth.md) documents the wire, recovery fields
included.

## Log in

A login is both factors presented directly, and it mints a token record:

```bash
substratectl login --server https://substrate.example --username ada
```

```http
POST /login
{"username": "ada", "password": "…", "totpCode": "123456", "label": "laptop"}

→ 201 {"token": {…}, "secret": "substrate_tok_…"}
```

There is no session object beside it: a session **is** a token record, and
[users, tokens, and actors](auth.md) is the full picture.

## Write your first record

Registration seeded the core vocabulary only, so the task kinds are not there
yet. Install the bundle that ships them from the catalog built into the binary,
and the collection exists. A catalog id is `{authority}/{name}`, so the slash in
it is percent-encoded to stay one path segment. Tasks name an assignee, so
`people` is admitted first — a bundle whose `requires:` is not met is refused:

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/catalog/people.substrate.reamde.dev%2Fpeople/install

curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/catalog/tasks.substrate.reamde.dev%2Ftasks/install

substratectl get task                     # empty, but the collection is there
```

The console does the same thing from the [Registry](console.md#registry) page,
and `substratectl apply -f bundle.yaml` applies a closure you hold as files.
All three run the same install path and validation, which
[vocabulary.md](vocabulary.md#admission) calls admission.

Add a task. The collection path names the kind, so the body is only the
properties:

```http
POST /api/v1/tasks.substrate.reamde.dev/task
Authorization: Bearer substrate_tok_…
{"properties": {"name": "Buy milk", "dueAt": "2026-08-13T09:00:00Z"}}

→ 201 {"id": "kq3v9x2m41pf", "kind": "tasks.substrate.reamde.dev/task",
       "properties": {"name": "Buy milk", "title": "Buy milk", "status": "open",
                      "dueAt": "2026-08-13T09:00:00Z"},
       "version": 1,
       "createdAt": "2026-08-12T10:00:00Z", "updatedAt": "2026-08-12T10:00:00Z"}
```

The server assigned the id, the `status` state started at its declared
`initial` value, and `title` came back without being written: the task kind
[derives it](vocabulary.md#admission) from `name`. The same write as a file
`substratectl apply` takes:

```yaml
kind: tasks.substrate.reamde.dev/task
data:
  properties:
    name: Buy milk
    dueAt: 2026-08-13T09:00:00Z
```

Read it back, then complete it. A state change is a `patch`, and the
declaration stamps `completedAt` for you:

```bash
substratectl get task kq3v9x2m41pf -o yaml
substratectl patch task kq3v9x2m41pf --state status=done
```

## Watch the change stream

Every committed write appended one entry to the repository's changelog. That
changelog is the truth (the records you just read are computed by replaying
it), and it streams:

```bash
substratectl watch
```

```http
GET /api/v1/changes?watch=1

{"bookmark": 412}
{"seq": 413, "ts": "2026-08-12T10:00:00.183742Z", "actor": "api", "op": "put",
 "kind": "tasks.substrate.reamde.dev/task", "recordId": "kq3v9x2m41pf",
 "payload": {"created": true, "properties": ["name", "dueAt"]}}
```

Without `from`, the stream opens at the current head and tails from there; the
bookmark is the seq it opened at, and passing it back as `from` later resumes
exactly where you stopped. Your repository's early sequence numbers are its
initial vocabulary: the shipped declarations were written into the changelog
as ordinary entries when the repository was created, which is why a first task
lands well above 1.

`actor` names the client that made the write. This request named none, so it
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
