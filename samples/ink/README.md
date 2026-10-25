# The ink sample

Package `samples.substrate.reamde.dev/ink`: handwritten notes, drawings and
marked-up PDFs in the Inkidian `.inkd` format. An `.inkd` file is JSON: pages
of pen, pencil and marker strokes, each point with its position, pressure,
time and stylus angle. The Inkidian iPad app writes it, its Obsidian plugin
reads it, and the format is specified in
[format/SPEC.md](https://github.com/toffalori/obsidian-inkidian/blob/main/format/SPEC.md).

The two kinds point at files:

- `note` is a handwritten note: the record points at its `.inkd` file.
- `markup` points at a PDF and at a separate `.inkd` layer drawn over it.

Ink is never JSON inside a record: a write carries at most 1 MiB, a few dense
handwritten pages, and a file round-trips with Inkidian byte for byte.
[docs/plans/ink.md](../../docs/plans/ink.md) compares the options. The console
reads `mediaTypes` on each property to know which one holds ink and which one
holds a PDF, and draws, edits, imports and exports both. `bundle.yaml` is the
contract; this file is not.
