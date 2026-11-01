# SubstrateKit

What every Apple app that speaks to a substrate shares, as a Swift package
(iOS and macOS 26): the record and its values, a kind's name and glyph as the
console draws them, and a linked record as a pill that opens a read-only view
of it. Substrate Chat uses it; an app adds it as a local package
(`project.yml`'s `packages:`) and imports `SubstrateKit`.

It has no API client of its own. An app hands the views a `RecordReader` over
the client it already has, so a read shares that app's session, timeouts and
sign-in, and says a refusal in the app's own words. `HTTPRecordReader` is the
plain bearer-token reader for an app with nothing of its own.

## What is in it

| | |
| --- | --- |
| `JSONValue`, `Record`, `Timestamp` | a record as the API serves it, read tolerantly |
| `RecordReference`, `KindReference` | a stored `{ref: <kind>/<id>}` and a kind's three segments; an id may hold slashes |
| `kindDisplayName`, `kindDisplayPlural`, `propertyLabel` | names in words, by the console's word lists (`web/console/src/lib/kind-names.ts`) |
| `KindGlyph`, `KindGlyphSpec` | a kind's symbol on its hue, by the console's table and hash (`web/console/src/lib/kind-glyph.ts`) |
| `ValueShape`, `RecordValueView`, `valueWords` | a property's value drawn by its declared type, else by its shape |
| `RecordPill` | a linked record: its kind's glyph and its name; a tap opens it |
| `RecordDirectory` | the records and declarations a scene has read, each read once and shared |
| `recordViewer(_:)`, `RecordSheet`, `RecordDetailView` | the read-only view a pill opens, whose own pills push within it |

## Using it

```swift
let records = RecordDirectory(reader: MyReader(api: api))

RootView()
    .recordViewer(records)              // every pill below opens its record
    .environment(\.recordTitles, known) // names the view already holds, by kind/id

RecordPill(RecordReference(kind: "ada.example.com/people/person", id: "ada"))
RecordValueView(record["assignee"] ?? .null)
```

A pill names its record from, in order: the title it was given, the
surrounding view's `recordTitles`, a read through the directory, then the id
when it is a slug, else the kind. A record the reader may not read throws
`RecordReadError.notAllowed`, which the view shows with the reader's
`deniedHint`.

The word lists and the glyph table mirror the console's; a change to either
there is a change here, so the same kind reads and looks the same in the
browser and the apps. `mise run apple:test:kit` runs the tests, which pin the
console's hues for a set of references.
