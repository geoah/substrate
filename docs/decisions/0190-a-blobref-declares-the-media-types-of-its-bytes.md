---
status: accepted
date: 2026-10-25
decision-makers: George Antoniadis
---

# 0190. A blobref declares the media types of its bytes

## Context and Problem Statement

A client that renders a file needs to know its format before the property
holds one. A `blobref` resolves to a manifest whose `mediaType` the uploader
chose, and only once a blob exists; an empty property says nothing, so the
console cannot offer the right upload filter or editor for it. The ink proof
of concept hit this at once: a new handwritten note's `file` and a new
marked-up PDF's `ink` layer had nothing a client could read to know they hold
an Inkidian `.inkd` file.

## Considered Options

- Sniff the value: read the manifest's `mediaType` once a blob exists
- Key on the property's name (`ink`, `file`, `document`)
- A `propertytype` refinement per format, based on `blobref`
- A new datatype per format (an `ink` file), its value the digest plus
  metadata the server derives from the bytes (page and stroke counts)
- A closed property key, `mediaTypes:`, on `blobref` alone

## Decision Outcome

Chosen: the closed key. A `blobref` property may declare
`mediaTypes:`, a non-empty list of lowercase `type/subtype` media types
without parameters, where `type/*` is allowed and `*/*` is not. It says what
the referenced bytes are. The loader validates it and stores it in the
definition, and the server acts on nothing: no write is refused for bytes of
another type. A client reads it to pick a renderer and to filter an upload.

Sniffing works only once a value exists. A name convention is a guess every
client makes separately, which is what
[0020](0020-dialect-keys-are-reserved-not-tolerated.md) refused. A refinement
cannot state a media type: its keys constrain the stored string, which for a
`blobref` is a digest, and a refinement is package-local, so every package
would declare its own `ink` type. A datatype per format puts a third party's
file format, and each of its versions, into the core: the engine would parse
`.inkd` to validate it and to derive the metadata, a `blobref` cannot be an
object field to carry that metadata today, and nothing filters or sorts on a
page count. Media types are the established name for a byte format, so the
key reuses the manifest's own vocabulary.

### Consequences

- Good, because a kind states once what its file properties hold, and every
  client reads the same answer, empty property included.
- Good, because the key is reserved before enforcement exists, so checking
  the manifest's `mediaType` at write time later changes no declaration
  ([0020](0020-dialect-keys-are-reserved-not-tolerated.md)).
- Bad, because until enforcement lands a declared `application/pdf` property
  can hold a PNG, and a client still checks the manifest before it renders.
- Bad, because even enforced, the key checks the uploader's label, not the
  bytes: a file labeled `.inkd` that is not one is refused only by the
  client that reads it.
- Bad, because the key set grows by one, which is an upgrade of every binary
  that reads a closure using it.

### Confirmation

`TestMediaTypesReserved` in `internal/vocabulary/mediatypes_test.go`: the key
is admitted on `blobref`, stored in the definition, and refused on every
other datatype, `json` included, and for each malformed entry.

## More Information

Enforcement is the trigger for revisiting: refusing a `blobref` write whose
manifest `mediaType` is outside the list, and deciding what an absent
manifest `mediaType` means then. Checking the bytes themselves would be a
per-format validator in the blob store, a separate decision.
