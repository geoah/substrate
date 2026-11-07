---
status: accepted
date: 2026-11-07
decision-makers: George Antoniadis
---

# 0229. A collection's shipped saved views are records of a seeded console kind

## Context and Problem Statement

A collection's saved views (the tabs above its table) live only in the
owner's `substrate.reamde.dev/core/consolepreference/navigation` record,
which the console writes. A package cannot ship one, so every collection
opens on All, and `core/contact`
([0231](0231-a-contact-is-a-core-kind-of-the-owners-data-and-a-setting-names-the-owners-own.md),
[contacts plan, "Console views"](../plans/contacts.md#console-views)) cannot
open on a People tab. Issue #1208 holds the work. Shipping views also needs
something the tree lacks: a seeded package has no way to ship a data record.
The only seeded records are the units, written by a seed of their own
([0186](0186-units-and-conversions-are-core-records-and-a-value-converts-through-the-repositorys-graph.md)).

## Considered Options

- Presentation keys on the kind declaration (a `views:` block on a kind).
- Shipped entries written into each owner's preference record.
- Records of a new seeded console kind, `substrate.reamde.dev/console/collectionview`,
  following [0166](0166-a-record-page-layout-is-a-record-in-a-seeded-console-package.md).

## Decision Outcome

Chosen: records of a seeded console kind. 0166's reasons carry over: keys on a
declaration would make every tab change a vocabulary upgrade of the package
that owns the kind and need a new dialect key on every binary
([0020](0020-dialect-keys-are-reserved-not-tolerated.md)), and the console
would still name no kind of its own accord. Writing into the preference
record would mix shipped views with the owner's in one record, so no seed
could tell which entry an owner changed, and a package's views would reach
only repositories whose console had run.

- A record holds `appliesTo` (a reference to the kind's declaration, as
  `recordview.appliesTo`), `name`, `where`, `sort` (`property:asc` or
  `property:desc`), `columns` (the property columns the view shows, in order;
  the rest are hidden), `group` (the property the rows are grouped by),
  `nest` (false shows a nesting collection flat) and `default`. `where` is one condition per property in the filter grammar,
  the same shape as a mapping's `where`
  ([0118](0118-a-mapping-where-narrows-its-sources-in-the-filter-grammar.md)).
  It is a `json` property, because the loader keeps `json` out of declared
  fields and an operand takes its property's shape, so the engine admits any
  JSON there and the console checks it as it reads the view. Every name a view
  holds is a property name, never the console's own column or token spelling.
- A seeded package ships its data records beside its declarations, in its
  own directory, as a bundle does. The vocabulary loader reads the
  declarations and leaves every other document to the **record seed**, which
  writes them at registration in the creation transaction, after the
  declarations, and again at every open, after the shipped vocabulary
  upgrade. It writes a record the repository lacks and refreshes one whose
  stored properties differ, unless any hand but the seed's and the engine's
  ever wrote or deleted it
  ([0191](0191-a-shipped-record-is-held-by-any-hand-but-the-one-that-ships-it.md)).
  A record whose kind the repository does not declare, or whose read or
  write fails, is skipped and logged; the open never fails for it. The refresh
  reads content, not versions, so a changed shipped record needs no package
  bump to land, though `kinds:check` still asks for one as it does for a
  bundle's records. The units keep their own seed, whose graph checks no
  other record needs.
- A provider, catalogue or sample ships views through its install or import,
  as it ships record views today; an import rehomes `appliesTo` with the kind.
- The console shows a collection's views after **All** and before the
  owner's own, in id order. A bare address opens on the first view marked
  `default` while the browser remembers nothing for the collection, and on
  whatever the owner chose last after that, All included: the stored state
  names the chosen tab. A view lights when the page shows what it names. Its
  menu renames, replaces or deletes the record through the console's door,
  which holds the record against later seeds. A view whose `where` uses an
  operator the filter bar cannot show is listed but cannot be opened, and its
  tab says why.
- The console's filter bar gains `exists` (has a value, has none), which the
  API already took, so a view such as "contacts with no type yet" shows and
  lights.

### Consequences

- Good, because a package gives its collection a sensible first tab by
  shipping a record, and the owner edits or deletes it like any record.
- Good, because any seeded package can now ship data records, not only
  views, with 0191's hold rule.
- Bad, because a view the owner edited or deleted never receives a newer
  shipped one, and the owner is not told one shipped (issue #1047).
- Bad, because two views marked `default` on one collection are not refused;
  the console takes the first by id.
- Bad, because the refresh merges: a property a later release drops from a
  shipped record stays on the rows already seeded, so a release changes a
  value rather than removing it.
- Bad, because every open reads each shipped record's row and changelog to
  find nothing to do, a cost that grows with what the seeded packages ship.
- Bad, because the engine admits any `where`: a view naming a property the
  kind does not declare, or an operator the grammar lacks, is stored, and
  only the console says it cannot show it.

### Confirmation

`internal/engine/recordseed_db_test.go` holds the seed at registration, the
catch-up of a repository created without the kind, a refresh of a record
only the seed wrote, and an edited and a deleted record that the next open
leaves alone. `kinds/kinds_test.go` holds every shipped record to a kind the
seed declares, its references to the stored spelling, and every shipped
view's `where` to the filter grammar. The console's tests hold the conversion
between a view's `where` and the filter bar both ways, `exists` in the bar
and the address, the default tab, and a remembered All.

## More Information

The first shipped views are on the two seeded kinds that are the owner's
data: `llm/memory`, Active (the default) and Retired, and `core/contact`,
the six views of the contacts plan with People the default. A number in a
contact view's id (`contact-1-people`) sets the tab order, since the console
orders a collection's views by id.
