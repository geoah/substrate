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

In one transaction the package's declarations land under the published name,
every record of its kinds moves to the kind of the same name (same id, same
properties, every reference to it repointed), the triggers that call its
functions and the app tokens scoped to it follow, the package in `~` is
removed, and release 1 is written. History keeps the old lines under `~` and
the move after them. From then on your own records, references and functions
use the published name, and the package's code writes as its published
hands (`function:ada.example.com:recipes:<name>`): the actor records of its
hands under `~` are disabled, as an uninstall disables them, so the writes
before still link them.

## Write the next release

Change the package's declarations as you would any of your own, then write
its next release: the console's New release action,
`substratectl publish ada.example.com/recipes --release`, or
`POST /api/v1/packages/release` with `{"package": "ada.example.com/recipes"}`.
A release snapshots the package's declarations as they are now, at a version
above every release before it, and is refused when nothing changed. It checks
the record again: if the domain's record no longer names your document, the
release is refused and no release written before changes. (`substratectl
release` is another command: it hands a record's pinned values back to sync.) What a publisher
may change between releases is
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

A server that publishes nothing answers `404` there.

## Afterwards

- There is no unpublish, and a release is never changed or removed.
- Deleting your repository stops its document being served. A copy installed
  elsewhere keeps its declarations and records, and gets no more releases.
- If the domain's record later names something else, copies installed
  elsewhere keep what they hold and follow the record wherever it points
  next; your next release is refused until it names your document again.
- A publisher's document may say `movedTo: <authority>`. A server shows that
  beside the publisher's catalog entries and never follows it: nothing moves
  between authorities. A document your server serves carries no pointer in
  this build.
