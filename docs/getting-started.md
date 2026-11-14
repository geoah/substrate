# Getting started

A fresh substrate is empty: no users, no records, nothing but the vocabulary
compiled into the binary. This page takes it from there to a task you can read
back, in four moves: register, log in, write a record, watch it land.

You need a running substrate and its invite code, if it has one.
[Running one locally](running-locally.md) stands one up on your own machine and
[running a substrate](operations.md) is the deployment; if somebody else runs
yours, they hand you the address and the code.

## Registration

Registering creates a **user** and, in the same transaction, that user's one
**repository**, seeded with the substrate's own vocabulary —
`substrate.reamde.dev/core` and `substrate.reamde.dev/llm` — three keyless
provider rows (`openai`, `anthropic`, `gemini`), and the assistant, Moss
(`substrate.reamde.dev/assistant`). Everything else,
including the task kinds used below, is a
[sample you add](builtin-kinds.md). A deployment with an **invite code**
configured admits only a request that presents it; the local substrate from
`docker compose up` or `mise run dev` has none and reads none.
[Users, tokens, and actors](auth.md#the-invite-code) has the detail.

Registration needs a repository name and a password from you, and offers a
TOTP second factor, which you may skip and turn on later in Settings
([decision record 0237](decisions/0237-a-second-factor-is-the-owners-choice-and-sign-in-asks-for-the-code-only-of-an-account-that-has-one.md)). The name is
yours permanently. The local substrate
[verifies no second factor](auth.md#the-second-factor-can-be-switched-off-locally),
so there a repository name and a password are the whole of it.

## Register

The [web console](console.md) serves a registration page at `/register`, and
that is the easiest way in. From a terminal, [substratectl](substratectl.md) does the same
thing:

```bash
substratectl register --server https://substrate.example --identity ada.example.com
```

It asks the substrate for a TOTP enrollment, prints the `otpauth://` URI and
the seed for your authenticator, and takes back one code along with the
password you choose. Only that second call writes anything, so an abandoned
registration leaves nothing behind. Registration ends logged in: `substratectl` stores
the minted token as a context in `~/.config/substratectl/config.yaml`. The
name is the one you sign in with, and the repository registration creates
takes it too
([0274](decisions/0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md)).
The repository's name IS its **authority**: any name you control
(`ada.example.com`), or a bare label the substrate completes under its public
host, and you can rename it later
([renaming a repository](auth.md#renaming-a-repository)). The kinds you
declare and the samples you add live in the repository's **local namespace**,
written `~` (`~/tasks/task`), so the commands below never spell the name
([0259](decisions/0259-a-repositorys-own-kinds-live-in-the-local-namespace.md)). Quote `~` in a
shell, which expands an unquoted `~/` at the start of a word to your home
directory.

Unattended, bring your own seed and skip the prompts:

```bash
substratectl register --identity ada.example.com --invite-code CODE \
    --totp-secret BASE32SEED --totp-code 123456 --password-stdin < password
```

`--skip-totp` registers without a second factor instead: no enrollment and no
code, and signing in asks for the password alone.

Underneath, registration is two HTTP calls, and only the second writes
anything. The response carries the token secret, shown exactly once.
[Users, tokens, and actors](auth.md) documents the wire.

## Log in

A login presents the password, and the code where the account has a second
factor, and it mints a token record. `substratectl login` asks for the code
only when the substrate answers that the account has one:

```bash
substratectl login --server https://substrate.example --identity ada.example.com
```

```http
POST /login
{"identity": "ada.example.com", "password": "…", "totpCode": "123456", "label": "laptop"}

→ 201 {"token": {…}, "secret": "substrate_tok_…", "repository": "ada.example.com",
       "identity": "ada.example.com"}
```

There is no session object beside it: a session **is** a token record, and
[users, tokens, and actors](auth.md) is the full picture.

## Write your first record

Registration seeded `core`, `llm`, `console`, `observations` and `assistant`, so the task kinds are
not there yet. ADD the sample that ships them from the catalog built into
the binary, and the kind exists. A catalog id is a package identity,
`{authority}/{package}`, so the slash in it is percent-encoded to stay one path
segment. A task names an assignee (a `person` from `people`) and its `tasklog`
binds the `occurrencelog` trait from `scheduling`, so the tasks bundle
`requires:` both and they are added first: a bundle whose `requires:` is
not met is refused:

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/catalog/samples.substrate.reamde.dev%2Fpeople/add

curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/catalog/samples.substrate.reamde.dev%2Fscheduling/add

curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/catalog/samples.substrate.reamde.dev%2Ftasks/add

substratectl get '~/tasks/task'    # empty, but the kind is there
```

Adding a sample copies the closure into THIS repository's local namespace, so
what lands is `~/tasks/task` and it is yours to change
([0048](decisions/0048-providers-are-published-samples-are-copied.md)).
`ada.example.com/tasks/task` is accepted as another spelling of it, and every
answer says `~`. The
console does the same thing from **All data → Add a collection → Start from
a sample** ([web console](console.md#add-a-collection)),
`substratectl add samples.substrate.reamde.dev/tasks` does it from a
terminal, and `substratectl apply -f bundle.yaml --as-mine` applies
a closure you hold as files. All four run the same admission, which
[vocabulary.md](vocabulary.md#admission) describes.

Add a task. The body names the kind, and the server assigns the id:

```http
POST /api/v1/records
Authorization: Bearer substrate_tok_…
{"kind": "~/tasks/task",
 "properties": {"name": "Buy milk", "dueAt": "2026-08-13T09:00:00Z"}}

→ 201 {"id": "01jbx9m2qz7k4c5v8n3w6t1hrd", "kind": "~/tasks/task",
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
kind: ~/tasks/task
data:
  properties:
    name: Buy milk
    dueAt: 2026-08-13T09:00:00Z
```

Read it back, then complete it. A state change is a `patch`, and the
declaration stamps `completedAt` for you:

```bash
substratectl get '~/tasks/task' 01jbx9m2qz7k4c5v8n3w6t1hrd -o yaml
substratectl patch '~/tasks/task' 01jbx9m2qz7k4c5v8n3w6t1hrd --state status=done
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

{"bookmark": 412, "generation": "7f3a0c2e9b1d4e6f"}
{"seq": 413, "ts": "2026-08-12T10:00:00.183742Z", "actor": "6nk6ekkczdgh399nt2pqpccjz0",
 "token": "01jbx9k0token000000000000a", "op": "put",
 "kind": "~/tasks/task", "recordId": "01jbx9m2qz7k4c5v8n3w6t1hrd",
 "payload": {"created": true, "properties": ["name", "dueAt"]}}
```

Without `from`, the stream opens at the current head and tails from there; the
bookmark is the seq it opened at, and passing it back as `from` later resumes
exactly where you stopped. Your repository's early sequence numbers are its
initial vocabulary: the shipped declarations were written into the changelog
as ordinary entries when the repository was created, which is why a first task
lands well above 1.

`actor` is the id of the [actor record](api.md#actors) that made the write:
here you, the repository's person, whichever client you used. `token` is the
id of the token the write came through, which tells your laptop's writes
from your phone's. An app's or a function's writes name their own actor. `substratectl get substrate.reamde.dev/core/actor <id>` reads
one. [The changelog and watch](changelog.md) is the whole contract, including
how to resume without a gap.

## Where to go next

- Declare a kind of your own, or read what the shipped ones say:
  [the data model](data-model.md) and [vocabulary as records](vocabulary.md).
- Query across kinds, or search: [the API](api.md#search).
- Connect a provider, or install an automation:
  [bundles](bundles.md) and the [catalog](bundles-catalog.md).
- Mint a token for a script: `substratectl token create --label backup`.

Next: [the data model](data-model.md), the one shape everything reads and
writes as.
