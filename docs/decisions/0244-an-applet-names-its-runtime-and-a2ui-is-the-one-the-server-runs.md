---
status: accepted
date: 2026-11-08
decision-makers: George Antoniadis
---

# 0244. An applet names its runtime, and a2ui is the one the server runs

## Context and Problem Statement

[0243](0243-an-applet-is-a2ui-screens-the-server-runs-and-the-console-gives-applets-a-place.md)
made an applet A2UI screens the server runs. A2UI's catalog is small on
purpose, and the screens people will want next (charts, drag and drop,
bespoke layouts) may need a view written as code, React in a sandbox being
the likely one. If the applet record does not say what draws it, the first
second renderer has to guess what every applet written before it meant, and
a reader cannot tell one applet's shape from another's.

## Considered Options

- A required `runtime` on the applet kind, with no default.
- The same property with `a2ui` as its default.
- A kind per renderer (`applets/applet` for A2UI, another kind for code).
- Nothing until a second renderer exists.

## Decision Outcome

Chosen: a required `runtime` with no default, whose one value is `a2ui`.
An applet has two halves: the data (`state`, `sources`, `actions`), which
the server runs whatever draws the screens, and the view (`start`,
`screens`), which belongs to the runtime. The runtime names the view. A
later runtime is a new value with a view of its own that reads and acts
through the same sources and actions over a bridge, so the checks at write
and the viewer's own access hold for it too. No default, because a default
is the guess this exists to avoid: every applet says what it is, and the
owner's assistant writes the word. One kind, because the launcher, the
routes, the designer's grant and the MCP tool are one place for every
applet. Waiting would have meant changing the shape after people hold
applets, where now it is unreleased.

`screens` stops being required by the declaration and is required by the
`a2ui` check instead. Repository migration 4 names `a2ui` on every applet
written before the property existed, before the shipped upgrade that
requires it, which would otherwise be refused while one lacks it.

### Consequences

- Good, because a second runtime is an additive enum value, not a change of
  shape, and an applet written today means the same thing after it lands.
- Good, because a binary that does not run an applet's runtime refuses it by
  name instead of drawing it wrong.
- Bad, because every writer names a word that has one value today: the
  designer, an MCP assistant and a hand-written applet all carry
  `runtime: a2ui`.
- Bad, because the render, act and preview routes and the frame stay the
  a2ui runtime's; a code runtime needs routes of its own that answer data.

### Confirmation

`TestCheckNamesEachMistakeByPath` (a missing and an unknown runtime are
refused) and `TestRepositoryMigrationNamesTheRuntimeOfEveryApplet` (an
applet written before the property is named `a2ui` and the upgrade lands).

## More Information

Amends 0243: an applet is A2UI screens when its runtime is `a2ui`. The
trigger for a second value is an applet the owner wants that A2UI with a
substrate catalog cannot draw; [the applets plan](../plans/applets.md) says
what a code runtime would need.
