# Publishing a package

A repository's own kinds live in its local namespace, `~`
([decision 0259](decisions/0259-a-repositorys-own-kinds-live-in-the-local-namespace.md)).
Nobody else can install a package there: `~/recipes` means "this
repository's recipes" wherever it is read. **Publishing** gives one of your
packages a name of its own under a domain you control, and serves it from
your server so another server can install it
([decision 0289](decisions/0289-a-repository-publishes-a-package-by-moving-it-out-of-the-local-namespace-to-an-authority-it-proves.md),
[decision 0291](decisions/0291-a-repositorys-server-serves-what-it-publishes-and-the-authoritys-record-naming-that-document-is-the-proof.md)).

Publishing is experimental: its routes are the `publishing` feature
([the features](api.md#the-features)).

## Three sorts of namespace

| Namespace | Example | Whose | Can it change |
| --- | --- | --- | --- |
| The project's | `substrate.reamde.dev` | the project | never ([decision 0292](decisions/0292-the-projects-name-is-permanent-and-its-packages-come-from-the-binary.md)) |
| A publisher's | `ada.example.com/recipes` | whoever proves the domain through DNS | never; a publisher that moves may leave a pointer ([decision 0293](decisions/0293-a-publisher-that-moves-leaves-a-pointer-the-server-shows-and-never-follows.md)) |
| Local | `~/recipes` | the repository | it has no global name, so there is nothing to rename |

Renaming your repository ([decision 0287](decisions/0287-a-repositorys-name-is-an-alias-and-a-former-name-holds-for-90-days.md))
never touches what you published, even when you published under the name the
repository had.

## Before you publish

- Your server has an HTTPS public URL (`SUBSTRATE_PUBLIC_URL`): another server
  fetches only HTTPS.
- You control a domain's DNS, such as `ada.example.com`. It may be your
  repository's name or any other. It may not be the project's name or a name
  under it.
- The package is in `~`, holds no settings or secrets of its own, and names
  no other package of yours. Publish that other package first, or stop naming
  it.

## Publish

Publishing is permanent. The package leaves `~`, keeps its published name for
good, and cannot be unpublished. Every client states that before you confirm.

1. **Ask what it will do.** In the console, the package's Publish action;
   with the CLI, `substratectl publish ~/recipes --as ada.example.com`; over
   the API, `POST /api/v1/packages/publish` with
   `{"package": "~/recipes", "authority": "ada.example.com"}`. The answer
   lists the package's new name, the kinds that move, how many records move,
   how many records elsewhere have a reference repointed, the other packages
   of yours whose declarations follow, and anything that refuses it now.
2. **Add the record it gives you.** It names a TXT record at
   `_substrate.ada.example.com`, for example:

   ```text
   _substrate.ada.example.com.  TXT  "v=substrate1 doc=https://substrate.example.net/published/rnd-0vrkvsgbs6bnz2sjy4196kccck/ada.example.com"
   ```

   The `doc=` address is your repository's document on your server. It is the
   proof: only whoever controls the domain's DNS can point it there. Do not
   pin `sha256=`: every release changes the document. If the name already
   carries a `v=substrate1` record (the one that verifies your repository's
   name, or an app's), the answer merges into it, so replace the record
   rather than add a second.
3. **Confirm.** The console's confirm button, `substratectl publish … --yes`,
   or the same request with `"confirm": true`. It is refused (`403`) until the
   record names your document.

The answer in step 1 is the publish itself, run to where it would commit,
with every check the publish makes on the way, and rolled back there: what
it lists as stopping the publish is what the confirm would be refused for,
and a preview with nothing listed is a publish that goes through once the
record is in place.
- Like the publish, the preview holds the repository's writes for as long as
  it runs, which for a package of thousands of records is a while. There is
  no budget on previews; only an admin can ask for one.
- A preview leaves two things behind, as a refused publish does:
  - The published functions' prepared bodies, which the runner retires at
    its next reconcile when nothing installs them.
  - The published kinds' indexes, which cost only their space until the
    publish lands the kinds that use them.

In one transaction the package's declarations land under the published name,
every record of its kinds moves to the kind of the same name, every name that
pointed at the package follows it, the package in `~` is removed, and release
1 is written. History keeps the old lines under `~` and the move after them.
From then on your own records, references and functions use the published
name, and the package's code writes as its published hands
(`function:ada.example.com:recipes:<name>`): the actor records of its hands
under `~` are disabled, as an uninstall disables them, so the writes before
still link them.

## What a publish moves, what stops it, and what it leaves

**It moves**, so that nothing live names the package in `~` afterwards:

- Every record of the package's kinds, to the kind of the same name. Each
  keeps its id, properties, marks, labels and annotations, the ids it was
  merged from, and its search vectors. A record merged into another moves
  with it, still merged, so a split brings it back. A record that pointed at
  one points at it under the published name.
- Every name of the package in the repository's other packages: a reference
  pinned at one of its kinds, a grant, a requirement, and a kind spelled in a
  function's source or a package's shared modules.
- Every name of it in your records:
  - a trigger's source and the function or agent it calls, and the kinds its
    guard and its arguments spell;
  - a change policy's kinds, agents and functions, so a gate on the
    package's agent keeps holding it;
  - an app token's scopes;
  - a role's rules;
  - a record view, a collection view and its filter, and the console's
    favorites;
  - a change request's target and proposal, and an approval's call.
- A pattern over every package of `~` (`~/*`, in a token's scopes, a
  trigger's source, a policy's kinds, a role's rules or another package's
  grant) keeps it, and gains the published package's glob beside it
  (`ada.example.com/recipes/*`). What reached the package before still
  reaches it, and nothing reaches more.

**It stops at**, with each record named in the preview:

- A setting or a secret of the package. Delete it, publish, and write it
  again.
- An approval still pending, approved or running whose call names the
  package: the call is handed the names it was asked with when it runs.
  Decide it, or let it run, first.
- A parked delivery of a record of the package, which a retry replays as it
  was written. Retry it or forget it first.
- A package it requires that no release serves at the version it requires.
  The publish compiles against what another server receives: a package of
  yours from its latest release, never from what you declared since, and
  another publisher's as the server's catalog serves it.
- A quantity in a unit only your repository holds. Another server holds the
  units every repository is given, and no more.
- A name in the package of another of your packages in `~`. Publish that one
  first, or stop naming it.
- Anything another server's install would refuse in the release. The
  publish runs that server's admission on the closure it will serve, against
  a fresh repository holding only what the package requires: a published
  agent names the provider input it takes rather than a provider, and states
  its daily cap.

  A release answers for the package it releases. A package it requires is
  taken as its publisher released it: if that package cannot be installed
  on a fresh repository (a unit only its author had, a shipped record that
  does not resolve), neither can yours, and the installer says which.
- A kind of the package that a mapping reads or writes, while it holds
  records.
- A publish of more than the work ceiling, 10,000 writes by default.
- A name the repository may not publish under (below).

**It leaves as written**:

- An agent's prompt. It is its author's prose: the preview lists every
  prompt that still names the package, so you can edit it.
- A trigger's arguments, which are data handed to its function: a string in
  them may be a name of yours or anybody's key. The preview lists every
  trigger whose arguments name the package.
- A transcript and a trigger's runs. These are history, and keep the names
  they were written under.
- The record of a hand that wrote under the package's name in `~`. Its
  writes link it. A run of that hand still going when the publish commits
  writes nothing more: its writes are refused, and its conversation ends in
  `error`.
- A record already deleted. A restored one keeps a reference at the old
  kind.
- An Idempotency-Key's stored answer. A retry under the key replays what the
  write answered then.
- Text of your own that names the package anywhere else, such as a
  setting's value of another package or a note. It is neither found nor
  changed: search for the old name after publishing.

The moved records' lines are no creation and no deletion: no trigger runs
for them, whatever its source
([decision 0298](decisions/0298-a-kind-move-sets-off-no-trigger-and-carries-a-records-merge-trail-and-vectors.md)).
Until the sweep collects them, within minutes, the old rows stay as
tombstones under a kind the repository no longer declares, and a read at the
old address
(`~/recipes/recipe/<id>`) answers as an unknown kind: use the published one.

## Which name you may publish under

- **Your repository's own name**, which then names the published package and
  no longer spells `~` for you.
- **Any other name** no repository on this server answers to.

It is refused (`409`):

- **Under a former name your repository still answers to.** The 90 days are
  for coming back to it, and a published name would stop it spelling `~` at
  once. Rename back to it first, or wait until it lapses.
- **Under a name another repository here answers to**, now or as a former
  name in force.

A rename later neither moves nor stops what you published:

- Rename away from a name you published under, and the packages stay under
  it.
- Rename back to it, and you may. The name then names your published
  packages, as when you published under your own name.

## Write the next release

Change the package's declarations as you would any of your own, then write
its next release: the console's New release action,
`substratectl publish ada.example.com/recipes --release`, or
`POST /api/v1/packages/release` with `{"package": "ada.example.com/recipes"}`.

Each of these first answers what the release will hold, and writes nothing:
its version and the one before it, the SHA-256 of the closure it will serve,
the declarations in it, and what stops it now. The console then asks you to
confirm. The CLI asks too, or writes without asking under `--yes`. Over the
API, the same request with `"confirm": true` writes it.

A release snapshots the package's declarations as they are now, at a version
above every release before it. It is refused:

- when nothing changed since the last one;
- when another server's install would refuse it, as at the publish;
- when the domain's record no longer names your document. A release written
  before stays as it was.

`substratectl release` is another command: it hands a record's pinned
values back to sync. What a publisher may change between releases is
[decision 0269](decisions/0269-a-publisher-may-not-rename-a-kind-and-code-from-outside-publishers-is-experimental.md)'s
rule: a kind is never renamed or removed. `GET /api/v1/packages/releases`
lists your releases.

## Installing it elsewhere

Another server installs it as it installs any publisher's package: through
the catalog's add route (`POST /api/v1/catalog/ada.example.com%2Frecipes/add`)
or the console's Add a collection. It reads the authority's record, your
document, and the closure the document lists by digest
([decision 0212](decisions/0212-a-package-is-published-by-its-authoritys-document-and-installed-from-it.md)),
and takes a newer release the same way.

Your server serves, without a token:

- `GET /published/{repository id}/{authority}`: the authority document.
- `GET /published/{repository id}/{authority}/{package}/{version}`: one
  release's closure, byte for byte.

A server that publishes nothing answers `404` there, and opens nothing to
find that out.

## Afterwards

- There is no unpublish, and a release is never changed or removed.
- Deleting your repository stops its document being served. A copy installed
  elsewhere keeps its declarations and records, and gets no more releases.
- If the domain's record later names something else, copies installed
  elsewhere keep what they hold and follow the record wherever it points
  next; your next release is refused until it names your document again.
- An authority publishes from the repository its record names. If the
  record is moved to another repository's document, on this server or
  another, that repository can publish under the authority too. Installers
  then read its document and its releases, and the first repository's next
  release is refused. The two hold separate release histories, so a version
  number the first one served may come back from the second with other
  bytes. Move the record only between repositories that carry the same
  package and agree on its versions.
- Restoring your repository from an archive taken before a publish takes
  the publish away:
  - the package is back in `~`;
  - its releases are gone, and your server stops serving its document;
  - the domain's record still names it.

  A release served once must never come back under the same version with
  other bytes, because servers that installed it hold those bytes. So if you
  publish again, start above every version you released before: raise the
  package's `version` in its header first. The publish takes that version for
  its first release.
- A publisher's document may say `movedTo: <authority>`. A server shows that
  beside the publisher's catalog entries and never follows it: nothing moves
  between authorities. A document your server serves carries no pointer in
  this build.
