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
The console's reader and renderer (`lib/ink.ts`, `lib/ink-render.ts`) are
adapted from the plugin and carry its copyright and MIT permission notice,
which the console build keeps in the bundle with every dependency's own.

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
has the reasoning. Inkidian registers no media type, so the sample uses a
vendor-tree name.

The console decides per property:

- A declaration that lists only the `.inkd` type holds ink, and one that
  lists only `application/pdf` holds a PDF, before anything is stored.
- A declaration that lists several types, or a family such as
  `application/*`, leaves it to the stored file: its media type, or a
  `.inkd` or `.pdf` name. Empty, such a property is neither.
- A property that declares nothing is read the same way, so an existing kind
  such as a lab report's `file` gets a PDF view without a change.

A kind with exactly one PDF property is drawn as a marked-up PDF, and its
first ink property (required ones first, then by name) is the layer over it.
Every other ink property is a note of its own. Nothing declares the pairing;
a kind that needs more than one PDF or more than one layer would need a
declared link, which is a new key and a decision of its own.

## The PDF approach

A `markup` keeps the PDF as uploaded and stores the ink as a separate `.inkd`
blob, page n of the layer over page n of the PDF. PDF.js draws each page with
the annotations the PDF already has, so ink Inkidian or Preview wrote into
the file shows. The layer uses Inkidian's coordinates for ink on a PDF page
(top-left of the page as displayed, 1240 units per 210 mm), the ones
Inkidian stores in a PDF's `/InkidianStroke`. An export into the PDF would
still map them through the page's crop box and rotation (`pageToPDF`, §9.3),
which the layer does not store.

## The console

- New, on a kind whose one ink property and heading are all a create needs
  (the sample's `note`), opens drawing mode on a fresh page. The name is
  filled in with when the note was started and stays editable; Save uploads
  the file and creates the record in one step. Any other kind keeps the
  general form, where a `blobref` is uploaded from a file picker, and so
  does a kind whose heading is a list of values.
- Drawing mode fills the window: the name, the save state and Save at the
  top; pen, pencil, highlighter and eraser, five colors, three widths and
  undo and redo (also Ctrl/Cmd+Z); the page setup; "Draw with finger"; zoom
  (Fit, in, out); and the pages with Add page at the bottom. A stylus's
  pressure and tilt are recorded; a mouse writes the spec's defaults. A
  finger scrolls the page unless "Draw with finger" is on, where a second
  finger scrolls instead. While a pen is down a finger does nothing, and a
  scroll the palm began stops.
- Leaving unsaved ink asks first, from every editor (a new note, a saved
  one, a PDF's layer): Cancel, Escape, a link and the browser's back through
  one dialog, a reload or a closed tab through the browser's own prompt
  where it has one (Safari on an iPad has none).
- Page setup: A4, A5, US Letter, this screen's size, or a custom size,
  portrait or landscape, for every page, and the background of the page being
  drawn on. Resizing keeps every stroke where it is (ink past the new edge is
  hidden, not removed) and undoes like any change. A new note starts on the
  size and paper this device last saved a note with.
- The record page shows a note's pages, with Draw first and Import .inkd and
  Download .inkd in a File menu. A note with no file, or an empty one, shows
  blank paper and "No handwriting yet".
- A save takes no change while it runs, and asserts the version the drawing
  began from: another save of the same ink meanwhile is refused and the
  drawing stays open, while a change to anything else on the record is
  saved over.
- An upload sends its bytes as a buffer and is refused when the server
  stored another size. The Apple shell received a `Blob` body empty, which
  stored a note as a 0-byte file.
- A `markup` shows the PDF's pages with the layer over them, and Annotate
  draws on the layer.
- Any other `blobref` reads as its file name, size and type with a Download
  button, where it used to be raw JSON.
- PDF.js loads only when a PDF is shown.

## Not done

- The server does not check a `blobref`'s bytes against its `mediaTypes`.
- A replaced `.inkd` file is collected once unreferenced, so a note's older
  versions are not restorable. Keeping them is a question for the blob store,
  not for ink.
- PDF.js gets no character maps or standard font data, so a PDF that
  relies on non-embedded CJK or standard fonts may draw its text wrong.
- No export writes the layer into the PDF as `/Ink` annotations, and ink
  already inside a PDF is shown but not editable.
- No agent fills `transcript` yet. A vision model reading a rendered page is
  the obvious way.
- No list thumbnails, no dark paper, and a long PDF renders every page at
  once.
- No sync from an Obsidian vault; a `.inkd` arrives by import.
