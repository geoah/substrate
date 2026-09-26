---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0131. The console writes for two readers, and one switch tells them apart

## Context and Problem Statement

Written after the fact, from the console review of 2026-09-24
([the first review](../console/reviews/2026-09-24-first-review.md)) and the
owner's rulings over the redesign in PR #648. The console spoke the engine's
language to everyone: kind references and record ids as names, actors as raw
ids, the mono identifier style on times and verbs, and help text such as "a
mapping-owned slot" or "owner tier". The people the console is for are mostly
not developers, and the developers who do use it need exactly those facts,
exactly spelled, to copy into a command or a declaration.

## Considered Options

- One voice, the developer's: keep every reference, id and tier on screen
- Two consoles: an everyday one, and a separate admin or developer area
  that holds the technical pages
- Local toggles: each page decides what to reveal (an ids switch here, a YAML
  tab there)
- One voice per reader, chosen by one per-person switch, **Technical
  details**, that every surface reads

## Decision Outcome

Chosen: one switch. Everyday copy, written for a person who is not a
developer, is the default. **Technical details** is one setting for the whole
console, stored with the other preferences
([0132](0132-console-preferences-follow-the-person-and-a-window-fact-stays-in-the-browser.md)),
and turning it on adds what a developer needs without taking anything away:
full kind references, record and actor ids, property keys, holding tiers,
changelog sequence numbers, mappings, permissions, the YAML source, the
authority and package tree in the sidebar and the supporting and internal
kinds. Everything technical stays reachable in everyday mode through the hover
card every identity mark opens, whose footer carries the full reference.

Two rules follow. A kind is identified by its full reference
`{authority}/{package}/{name}` wherever identity matters
([0101](0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md)),
and its display name ("Tasks", "People") is a label only: plural names are a
console fact, built from the kind's name, never sent to the API and never
standing where a kind is identified. And the mono identifier voice is kept for
what a reader would copy (ids, references, YAML, URLs, code), never for times,
verbs, statuses or counts.

The developer's voice is what the review found failing the owner's reader. Two
consoles would duplicate every page and drift. Local toggles make a reader
learn one switch per page, and a fact revealed on one page and hidden on the
next reads as a bug.

### Consequences

- Good, because a person reads plain words by default and a developer gets
  the exact facts one switch away, on the same page.
- Good, because there is one place to ask: `useTechnicalDetails()`, read by
  the identity components, so a surface that uses them is right in both modes
  without extra code.
- Bad, because every surface has two renderings to write and test, and a
  technical fact shown without the switch is a regression no linter catches:
  the review proposed a rule refusing an `authority/package/name` string
  outside `useTechnicalDetails()`, and it was not built.
- Bad, because display names come from a word list in the console
  (`lib/kind-names.ts`) that another client does not share, and some shipped
  kinds read wrong however they are split (issue #675).

### Confirmation

`components/identity/identity.test.tsx` holds the identity marks in both modes
(a raw actor inline only in technical mode, a kind's full reference beside its
label only in technical mode, a state's stored value only in technical mode,
never a bare id for a record). `lib/kind-names.test.ts` holds the display
names. The rest is held by review.

## More Information

[The web console](../console.md#technical-details) lists what the switch adds
on each page. Reopen trigger: a second reader the two voices do not serve,
such as an operator's view of many repositories.
