# Applets

An applet is a small app over your records, built for a phone: your books
with a search and a filter by status, a page to log runs, a list of plants
by when they need water. Each is a record of
`substrate.reamde.dev/applets/applet` that names its runtime, what draws its
screens. Today that is `a2ui`: screens of [A2UI](https://a2ui.org) v1.0
components, which the substrate runs, so what you tap reads and writes your
records under your own access, never a model's turn. Applets are
experimental: the `applets` package and its routes may change or go in any
release, and they are outside what a release promises to keep
([stability](stability.md#outside-the-promise-by-name)). The design is
[decision 0243](decisions/0243-an-applet-is-a2ui-screens-the-server-runs-and-the-console-gives-applets-a-place.md),
the runtime
[decision 0244](decisions/0244-an-applet-names-its-runtime-and-a2ui-is-the-one-the-server-runs.md),
and [the applets plan](plans/applets.md).

## Getting one

Ask Moss: "make me an app for my books, where I can filter by status, search,
and add books by searching online". Moss has its architect make the
collection when you lack one, and hands the applet to its designer, which
writes it, checks every screen and tells Moss the link. It takes a few
minutes, in the background. When your request leaves open something that
changes what gets built (what you will mostly do with it, how often a plant
needs water), the architect or the designer asks first, Moss puts the
questions to you, and the same builder carries on with your answers. To
change an applet, say what to change ("show the author's name in the list",
"add a notes box"), and the designer revises it.

An assistant connected over [MCP](mcp.md) builds applets too: its `applets`
tool serves the guide to the format and previews a screen, and it writes the
applet with `write`.

## Using one

**Applets** in the console's sidebar lists them. An applet opens in the
console at `/applets/{id}`, or full screen at `/a/{id}` (**Open full
screen**), the address to add to a phone's home screen. Back and bookmarks
work: the address holds the screen and the record it is about.

## What an applet can and cannot do

It can list, search and filter records, show one, create, change and delete
them, call a function you have (a book search), and move between screens. On
a collection with a schedule it shows what is due and what is overdue: a
plant watered a few days after the last time has its next date moved on when
you mark it done, and a bill due on the 1st has its dates worked out by the
substrate from a repeat rule. It reaches nothing your own access
does not.

It cannot run code of its own, reach a site except through a function you
installed, or show a part of a screen only when a value is set (A2UI v1.0 has
no such condition; the designer works around it). An action that fails
halfway keeps what its earlier steps wrote, and a double tap may run an
action twice ([#1265](https://github.com/geoah/substrate/issues/1265)).

Images an applet names on another site (a book's cover) come through the
substrate, so the site never learns who is looking. It does learn which images
are shown, so an applet written by anyone but you, your designer or your MCP
assistant is refused: no app scope writes one.

## Changing or removing one

Ask Moss, or edit the record: the console's applet page links its
**Definition**, the record page, where it can be edited or deleted like any
record. Every write of an applet is checked against the A2UI catalog and your
collections, and a broken one is refused with a problem per field.
