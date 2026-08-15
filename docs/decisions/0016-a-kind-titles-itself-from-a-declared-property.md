---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0016. A kind titles itself from a declared property, never the built-in slot

## Context and Problem Statement

Every record carries a built-in `title`: one untyped string with its own
column (`internal/engine/fold.go`), written straight through unless the kind
declares a `displayTemplate`, in which case the engine derives the column and
drops what the writer sent. Kinds are meant to declare their own heading and
render it, but three shipped authorities used the slot as their only display
storage instead, and `media/bundle.yaml` stated that as policy
([#123](https://github.com/geoah/substrate/issues/123)). `media` has since
left the tree ([0015](0015-unproven-kinds-stay-out-of-the-stable-set.md)),
leaving `tasks/task` and `calendar/transcript` to be settled before the
vocabulary freezes.

## Considered Options

- Keep the slot as a legitimate display path for kinds whose heading really is
  a title, and narrow the written rule to "do not DECLARE `title`/`body`"
- Every kind declares its own heading property and renders the title from it;
  the column becomes derived storage the vocabulary never writes
- Wait for [#68](https://github.com/geoah/substrate/issues/68) (the
  `titleTemplate` datatype and the retirement of the authored title) and move
  every kind in one pass afterwards

## Decision Outcome

Chosen: a kind's heading is a property it declares, and its title is rendered
from that property. A slot shared by every kind carries no type, no
description, no `required`, no per-kind filter and nothing a narrowing check
can hold, so two kinds spelling their heading `title` agree on the word and
nothing else: `filter.properties.title` reads a stored string on one kind and
a derived one on the next. Keeping the slot would have frozen that split into
v1.

Waiting for #68 was the close option and lost on order: repositories accrete
task and transcript records under the slot for as long as it takes, and every
one of them is work for the backfill that #68 has to write anyway.

`task` and `transcript` therefore declare `name` and render
`{name|title}` (transcript falls through to its meeting). The `title`
alternative is the legacy read, not a second spelling to write: records
written before `name` existed hold their heading in the column, and rendering
it keeps them titled instead of blanking them on their next write. It goes
when the column does.

### Consequences

- Good, because a heading is now a declared property like every other value:
  typed, described, filterable per kind, and held by the narrowing check.
- Good, because the freeze lands on kinds that already own their display text,
  so #68 changes how a template is spelled and not what each kind stores.
- Bad, because the engine IGNORES a `title` written to a kind that has a
  displayTemplate. The Linear task projection moves to `name` here; any client
  that still writes a task title loses it silently, with no refusal.
- Bad, because records written before this hold their heading only in the
  column. They still display, but `filter.properties.name` does not find them
  until something rewrites them; the changelog backfill belongs to #68.
- Bad, because the rule ships with its own exception in the tree: the
  alternation names the slot it retires, and 15 kinds (core's six and nine
  bundle mirrors) still title themselves from it.
- Bad, because the alternation reads the column the render writes into, and
  the column cannot say which of the two a value was: clearing `name` on a
  record that had one leaves the heading it last rendered, until a write sets
  `name` again. Separate storage for an authored title is #68's, so this
  stands until then, pinned by
  `TestClearingTheHeadingKeepsTheRenderedTitle`.

### Confirmation

`TestEveryKindDeclaresADisplayTemplate` in `kinds/kinds_test.go` lists those
15 by name, so a new shipped kind cannot join them and a fixed one has to
leave the list. `TestLegacyTitlesSurviveADisplayTemplate` in
`internal/engine/title_slot_db_test.go` holds the legacy alternative:
a kind that gains `{name|title}` keeps titles written before the property
existed. `mise run kinds:check` holds the version bumps.

## More Information

[#123](https://github.com/geoah/substrate/issues/123) is the contradiction
this settles; [#68](https://github.com/geoah/substrate/issues/68) carries the
mechanism (`titleTemplate` as a datatype, `title` and `body` as ordinary
declared properties, the changelog backfill) and
[#51](https://github.com/geoah/substrate/issues/51) the composed templates
core's six template-less kinds want. Revisit when #68 retires the column: the
`|title` alternative and this record's exception list go with it.
