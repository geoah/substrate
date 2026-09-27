---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0132. Console preferences follow the person, and a window's own facts stay in the browser

## Context and Problem Statement

Written after the fact, from the console review of 2026-09-24
([the first review](../console/reviews/2026-09-24-first-review.md)), the
second review's finding S2
([2026-09-26](../console/reviews/2026-09-26-design-review.md)) and the
owner's rulings over the redesign in PR #648. The console already kept its
sidebar state (folded groups, favorites, whether the sidebar is open) on one
record, `substrate.reamde.dev/core/consolepreference/navigation`. The
redesign added layout settings (record page width, table width, row height,
Technical details, appearance), and something had to decide where each lives.
Keeping the sidebar's open state on the record then collapsed it on every
device the moment it was tucked away on one.

## Considered Options

- This browser's `localStorage` for everything
- Core `setting` records, the key-value records bundles keep
- The `consolepreference` record for everything, the open sidebar included
- The `consolepreference` record for what describes the person, and this
  browser for what describes one window or one moment

## Decision Outcome

Chosen: the record for the person, the browser for the window. Record width,
table width, row height, Technical details, appearance, favorites and folded
groups are properties of the `consolepreference` kind, so every browser the
person signs in from looks the same. Whether the sidebar is open is a fact
about one window (a laptop tucks it away, a desktop monitor keeps it), so it
is kept in `localStorage` alone, never written to the record, and a value an
older console stored there is ignored. So are the moment's facts: a
collection's last filters, sort, nesting and columns, and the Search page's
ranking.

A write is a read-modify-write under `ifVersion`, retried on a conflict, so
two sessions changing different settings keep both. A repository whose stored
`consolepreference` declaration predates a setting refuses the undeclared
property, so the console writes a setting only where the stored declaration
names it and otherwise keeps it in `localStorage`; every written setting is
mirrored there too, so the sign-in page starts from it.

`localStorage` alone makes the person set the console up again on every
device. Core `setting` records belong to bundles and are read by their
functions ([0076](0076-a-bundle-ships-its-settings-as-core-setting-and-secret-records.md)),
the wrong home for how the console looks. The record for everything is what
S2 found wrong.

### Consequences

- Good, because the console follows the person: the same widths, density,
  theme and technical mode on every browser.
- Good, because a repository that has not taken the kind's upgrade still
  works: the setting is kept in the browser until the declaration names it.
- Bad, because every preference change is a changelog entry. History hides
  them in everyday mode as system changes, and names them in the Settings
  page's words when shown.
- Bad, because a new setting is a change to a core kind with a version bump,
  and a repository sees it only after the boot upgrade.
- Bad, because the kind still declares `sidebarOpen`, which no console writes
  now; retiring it is a later declaration change.

### Confirmation

`lib/console-preferences.test.ts` holds the split: the sidebar kept in this
browser and never written to the record, an old stored sidebar value ignored,
an undeclared setting kept in this browser and never written, a declared one
written, and the record read before the browser and the browser before the
default.

## More Information

[The web console](../console.md#layout-preferences) describes the settings.
Saved views on a collection (issue #679) would be the next preference of the
person's, and belong on the same record. Reopen trigger: a preference that is
neither the person's nor one window's, such as one shared by everyone who
signs in to a repository from one device.
