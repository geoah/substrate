---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0291. A repository's server serves what it publishes, and the authority's record naming that document is the proof

## Context and Problem Statement

Publishing moves a package out of `~` to an authority its owner proves
([0289](0289-a-repository-publishes-a-package-by-moving-it-out-of-the-local-namespace-to-an-authority-it-proves.md)).
Another server installs a published package by reading the authority's
`_substrate` record, the document it names, and the closure that document
lists by digest
([0212](0212-a-package-is-published-by-its-authoritys-document-and-installed-from-it.md)).
Until now a publisher wrote and hosted that document and its closures by
hand. A repository needs a proof of the domain, a place its versions live,
and an address another server reads them at.

## Considered Options

- A one-time code in the `_substrate` record, as a repository proves its own
  name ([0208](0208-a-repository-proves-its-name-with-a-txt-record-after-registration.md)),
  and a hand-hosted document as before.
- The record's `doc=` naming the document this server serves for that
  repository and authority, which is both the proof and the address.

## Decision Outcome

Chosen: the record's `doc=` is the proof. A publish, and every release after
it, reads the authority's `_substrate` record and admits only when its
`doc=` is exactly `<public URL>/published/<repository id>/<authority>`, with
no `sha256=` pin, because every release changes the document. Only whoever
controls the authority's DNS can write that record, and it names this server
and this repository, so it carries both the control and the intent. The
publishing server needs an HTTPS public URL (`SUBSTRATE_PUBLIC_URL`), because
another server fetches only HTTPS. A server installing the package reads the
same record and the same document, as it reads any publisher's, so nothing
about installing changes.

- **Releases are records.** Each version is a record of core's `release` kind
  in the publishing repository: the closure's bytes (the package's
  declarations as it holds them at that version, with no data record), their
  SHA-256 and the version, written by the publish and by each release, and
  never changed or removed. The history holds them, so an export carries
  them.
- **The server serves them**, bearer-less and outside `/api`, addressed by
  the repository's id so a rename moves nothing:
  `GET /published/{repository}/{authority}` is the authority document, and
  `/published/{repository}/{authority}/{package}/{version}` is a closure. The
  `publishing` feature (experimental) holds these routes with
  `POST /api/v1/packages/publish` (a plan without `confirm`, the publish with
  it), `POST /api/v1/packages/release` and `GET /api/v1/packages/releases`.
  A server that publishes nothing answers `404` there and pays one lookup.
- **In the publishing repository** the package stays its own: `installed`,
  writable by its owner under the published name, and released by the owner
  when the owner chooses. A release is the package header's version raised
  past every release before it, and it is refused when nothing changed since
  the last one. What may change between releases is step 30's rule for every
  publisher
  ([0269](0269-a-publisher-may-not-rename-a-kind-and-code-from-outside-publishers-is-experimental.md)).
- **Permanence.** There is no unpublish and no deletion of a release. Deleting
  the publishing repository stops its document being served. A copy
  installed elsewhere keeps its declarations and records and stops receiving
  releases, as with any publisher whose document goes (0212).
- **A proof that stops holding** refuses the next release, not anything
  already done: installed copies keep what they hold and follow the record
  wherever it points next.

This amends 0212 (a publisher's document may be served by a repository's
server) and [0209](0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md)
(the same record serves both).

### Consequences

- Good, because publishing needs no hosting beyond the server the repository
  already runs on, and the record a person adds is the one line the
  console and `substratectl` show them.
- Good, because an installing server needs nothing new.
- Bad, because a publisher's packages are served only while its server runs
  and has a public HTTPS URL.
- Bad, because the authority's DNS decides who publishes it: whoever points
  the record at another repository's document publishes there next, and an
  installed copy refuses that one's lower versions as a downgrade.
- Bad, because a closure is stored whole in a record, so a very large package
  is bounded by what a record holds.

### Confirmation

`TestARepositoryPublishesAPackageAndAnotherServerInstallsIt` (two servers, a
stand-in for DNS, both releases) and the refusals in it.
