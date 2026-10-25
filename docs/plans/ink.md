# Ink: handwritten notes and marked-up PDFs

Substrate stores typed records and files, and nothing in it could hold or
show handwriting. This page records how a proof of concept adds it: the file
format it reads, where a record keeps ink, the PDF approach, and what is
left. The sample package is `samples.substrate.reamde.dev/ink`
([samples/ink](../../samples/ink/bundle.yaml)); the console code is under
`web/console/src/components/ink/` and `web/console/src/lib/ink*.ts`.

Decided on 2026-10-23: ink is a file a record points at. The sample keeps
`note` and `markup`; the third option, ink inside the record, was built for
the comparison below and removed.

## The format: Inkidian `.inkd`

[Inkidian](https://github.com/toffalori/obsidian-inkidian) is an iPad app and
an Obsidian plugin. The app writes `.inkd` files, the plugin renders them, and
the format has a public spec (`format/SPEC.md`), a JSON Schema and sample
files, all MIT. Version 1 is final; new optional fields may still appear.

- A file is one JSON object: `format: "inkidian"`, `formatVersion: 1`, an id,
  two timestamps, one page size for every page (`1240 x 1754` is A4, in units
  of 1240 per 210 mm), and a list of pages.
- A page is a background pattern (`blank`, `lined`, `grid`, `dots`) and its
  strokes in drawing order.
- A stroke is a tool (`pen`, `pencil`, `marker`), a color, an opacity, a
  `baseWidth` and its points. Each point is
  `[x, y, pressure, t, azimuth, altitude]`.
- The spec fixes the rendering: the pen's width is
  `baseWidth * (0.25 + 1.5 * pressure)`, pencil and marker vary darkness
  instead, and the marker multiplies with the paper. That rule equals
  [perfect-freehand](https://github.com/steveruizok/perfect-freehand) with
  `thinning: 0.75`, which is what the plugin and this console use.
- A reader must refuse a newer `formatVersion`, must draw an unknown tool as a
  pen, and a writer must keep every field it does not understand on every
  object it did not change.

Compact `.inkd` costs about 41 bytes per point (measured on the plugin's four
samples). Three lines of handwriting are about 1,300 points, 55 KB.

Ink on a PDF is different: the app appends an incremental update to the PDF
with one `/Ink` annotation per stroke (a `/Highlight` for the marker), each
carrying an appearance stream and the stroke's own JSON in `/InkidianStroke`.
Every PDF viewer shows that ink, PDF.js included.

### Why this format

| Format | What it is | Why not the base |
| --- | --- | --- |
| `.inkd` | JSON pages of pressure strokes, public spec | chosen: plain JSON, has a writer (the iPad app) and an MIT renderer to port |
| W3C InkML | XML traces with declared channels | no writer or reader anyone here uses |
| Excalidraw | JSON scene of shapes, text and `freedraw` elements | a whiteboard, not pages; ink is one element type of many |
| tldraw | `draw` shapes in a document store | current versions delta-encode points as base64, an SDK-owned shape |
| reMarkable `.rm` v6 | binary, versioned per firmware | device-specific, reverse-engineered |
| PencilKit `PKDrawing` | Apple's binary drawing | opaque outside Apple's frameworks |
| SVG | rendered paths | loses pressure, time and tilt; output, not storage |

## Where the ink lives

The proof of concept built three options on one renderer and editor, one
kind each, and compared them:

| | `note`: file | `sketch`: inside the record (removed) | `markup`: PDF and a layer |
| --- | --- | --- | --- |
| Property | `file`, a `blobref` | `ink`, a `json` | `document` and `ink`, two `blobref`s |
| Largest note | 64 MiB per file | 1 MiB per write, about 25,000 points | 64 MiB per file |
| A save writes | a new blob, and the record's pointer moves | the whole document into the record | a new layer blob |
| History | each save is a pointer change; a replaced file's bytes are collected once nothing points at them | each save is the whole document in the changelog, so every version stays | as `note` |
| Inkidian round trip | an imported file is stored byte for byte | stored as parsed JSON; unknown fields are kept | the PDF is never changed |
| Search and agents | strokes are not indexed; the `transcript` property is the text | same | same |

One record per stroke or per page was weighed and not built: a page read
would be a list query and a note thousands of rows.

`note` and `markup` stay: handwriting outgrows 1 MiB within a few pages, and
a file round-trips with Inkidian exactly. `sketch` is gone, and with it the
console's support for ink inside a `json` property.

## A kind, or a property

Ink is already a property, not a kind. The console keys on a `blobref`
property that declares the `.inkd` media type, never on the kind holding it,
so any kind gains a drawing by declaring one:

```yaml
properties:
  sketch:
    type: blobref
    mediaTypes: [application/vnd.inkidian+json]
```

A task with that property gets the same section, editor and import as a
`note`. The `note` kind exists because a notebook page is a thing you list
and open on its own, with a name and a transcript; `markup` because the
console pairs a kind's one PDF with its one ink file.

What `mediaTypes` does not do is force the bytes to be an `.inkd` file.
There are two ways to force it, and only the first is cheap:

- Check the label. At write time the engine compares the blob's manifest
  `mediaType` with the declared list; the manifest is already loaded there.
  This is the follow-up record 0190 names. It checks what the uploader said,
  not the bytes.
- Check the bytes, or go further and make the property a composite of the
  digest and metadata the server derives (page and stroke counts). That is
  a new datatype whose server parses `.inkd`: a third party's format, and
  each of its versions, in the core. The dialect also forbids a `blobref`
  inside an object today, because a read resolves blob-refs per property.
  Nothing filters or sorts on a page count, and the console derives the
  counts from the file it already reads.

A `propertytype` refinement (`type: inkfile`) would name the shape once, but
refinements are package-local, so a task in another package could not use
the sample's.

## How a client knows a property holds ink

A `blobref` property may declare `mediaTypes:`, for example
`[application/vnd.inkidian+json]` or `[application/pdf]`. The loader checks
the spelling and stores it; the server acts on nothing yet. Decision record
[0190](../decisions/0190-a-blobref-declares-the-media-types-of-its-bytes.md)
(proposed) has the reasoning. Inkidian registers no media type, so the sample
uses a vendor-tree name.

Where a `blobref` declares no media types, the console still draws it as ink
or a PDF when the stored blob's own media type (or a `.inkd` or `.pdf` name)
says so, so an existing kind such as a lab report's `file` gets a PDF view
without a change.

## The PDF approach

A `markup` keeps the PDF as uploaded and stores the ink as a separate `.inkd`
blob, page n of the layer over page n of the PDF. PDF.js draws each page with
the annotations the PDF already has, so ink Inkidian or Preview wrote into
the file shows. The layer uses Inkidian's coordinates for ink on a PDF page
(top-left of the page as displayed, 1240 units per 210 mm), so a later export
into the PDF needs no conversion.

## The console

- The record page draws each ink property as pages, with Draw, Import .inkd
  and Download .inkd. Draw opens the editor: pen, pencil, highlighter and
  eraser, five ink colors, three widths, undo and redo (also Ctrl/Cmd+Z), the
  paper pattern and more pages. A stylus's pressure and tilt are recorded; a
  mouse writes the spec's defaults. After a pen touches the page, a finger
  no longer draws.
- A `markup` shows the PDF's pages with the layer over them, and Annotate
  draws on the layer.
- The new-record form uploads a file for a `blobref`, and offers "Start a
  blank note" where the property holds ink. The record write uploads the
  file again first, so a form left open past the blob store's one-minute
  grace still saves.
- Any other `blobref` reads as its file name, size and type with a Download
  button, where it used to be raw JSON.
- PDF.js loads only when a PDF is shown.

## Not done

- The server does not check a `blobref`'s bytes against its `mediaTypes`.
- A replaced `.inkd` file is collected once unreferenced, so a note's older
  versions are not restorable. Keeping them is a question for the blob store,
  not for ink.
- One property that declares several media types (`application/*`, or both
  PDF and `.inkd`) is shown and uploaded by its declaration, not by the
  stored file: the upload refuses a PDF there as not an Inkidian note.
- No export writes the layer into the PDF as `/Ink` annotations, and ink
  already inside a PDF is shown but not editable.
- No agent fills `transcript` yet. A vision model reading a rendered page is
  the obvious way.
- No list thumbnails, no dark paper, and a long PDF renders every page at
  once.
- No sync from an Obsidian vault; a `.inkd` arrives by import.
