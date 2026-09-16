---
status: accepted
date: 2026-09-16
decision-makers: George Antoniadis
---

# 0085. A mapping synthesises its subject slot on the source kind

## Context and Problem Statement

[0049](0049-the-owner-of-a-mappings-target-declares-it.md) moved the
declaration of a `recordmapping` to the package that owns its `to` kind, and
left one thing behind: item 3, the rule that the SOURCE kind declares the
`property:` the mapping names, as a `subject: true` reference. So a GitHub
provider that wanted its issues mappable onto a consumer's task had to declare
`task` on `github/issue`, and `person` on `github/user`. The provider knew
about its consumer, every consumer with a different word needed the provider
to have anticipated it, and a second consumer could not have the same source
under its own noun at all. Owner's review of Mneme v6's mirrors (2026-09-16):
*"the provider for GitHub should be a well-known thing … it should provide only
the GitHub-specific things, and then Mneme should map GitHub issues into
tasks. Why did you have to create a task property on the GitHub issue?"*
([#570](https://github.com/geoah/substrate/issues/570)).

## Considered Options

- Keep the declaration on the source, and have each provider ship one slot per
  consumer noun it can guess
- Let the mapping declare the slot as a document of its own, a second kind of
  property declaration
- Synthesise the slot on the source kind's live registry entry when the mapping
  installs, and take it back off when the mapping goes

## Decision Outcome

Chosen: the third, and it supersedes ITEM 3 OF 0049 ALONE. Items 1, 2 and 4 of
that record stand unchanged — the owner of `to` declares the mapping, one
mapping per (source kind, subject property), and uninstalling a package another
package's mapping names is refused.

Admitting a mapping synthesises a reference on its source kind, named by
`property`, pinned at `to`, single-valued, `mustExist: true`, never
`onDelete: cascade`, and `managed: true` in the sense
[terms.md](../terms.md) uses: a client may echo the value, never change it.
The slot is filled by the write path's own match-or-mint or by a create that
already knows the subject, and moved only by merge and split — which is
`checkSubjectWrite`'s rule, not `managed`'s, so `checkManagedProps` skips a
subject slot deliberately.

Storage and the fold are untouched. The slot was already a property the write
path filled; what changed is who declares it, and how the registry comes to
know about it.

Four rules hang off it:

1. **The reconcile is registry-wide and re-runs on every door that mutates the
   registry** — `Finalize`, `Install`, `InstallAll` and `Remove`. A mapping
   installs long after the vocabulary it names, so a pass that only walked the
   packages in hand would never reach back to the source kind.

2. **It is copy-on-write.** A candidate registry is a clone sharing its
   packages and kinds (`Registry.Clone`), so a kind that changes is REPLACED —
   a copy of the kind inside a copy of its package, visible to that registry
   alone. Writing through would leak a candidate's mapping into the live
   registry and outlive the apply that was refused.

3. **A collision refuses the mapping.** A `property` naming something the
   source kind declares for itself is refused, naming both, because the
   mapping would otherwise take a declared slot over silently.

4. **A source kind that DECLARES the slot keeps its declaration**, and the
   mapping stamps the pin and the marker onto it. That is not a concession, it
   is what makes the change invisible where it must be: a bundle written before
   this record still loads, and a declaration read back with `get -o yaml` and
   applied again round-trips.

**Removing a mapping removes the slot, and is REFUSED while live records still
link through it.** The alternative — clearing the links as a lossy null step,
which is what the ordinary drop path would do (0067) — was rejected: a subject
link is not an ordinary optional value, `checkSubjectWrite` refuses every move
of one, and the only road back from clearing them is a full resync of every
mirror row. Deleting the records, or keeping the mapping, are the two honest
answers and the guard names them.

The shipped closures follow: `github`, `google` and `linear` drop the six slots
they declared, with the package version bumps `kinds:check` demands. Linear's
`issue` gains `assigneeUser`, a reference at its own `user` mirror — legal
since [0084](0084-a-reference-may-pin-a-mapping-source-kind.md) — for the one
write its sync made into the old `assignee` slot when the viewer's address is
hidden.

### Consequences

- Good, because a provider bundle is the provider and nothing else. `github`
  ships GitHub; what a GitHub issue MEANS to a repository is the repository's,
  and two repositories may disagree about the word without either touching the
  provider.
- Good, because a second consumer can map the same source onto its own kind
  under its own noun, which the declare-it-on-the-source rule made impossible
  for any noun the provider had not guessed.
- Good, because `subjectPinned` is gone. It re-pinned an unpinned slot on every
  single write; the registry carries the pin now, resolved once at admission.
- Good, because `propertyMeta` finally answers honestly for the slot: the
  manager is `mapping:<id>` at the machine tier, whoever's write filled it.
  Crediting the connector said a bundle held a link only merge and split can
  move.
- Bad, because a kind's properties are no longer a pure function of its own
  document. The registry entry for `github/user` depends on what the
  repository has imported, so two repositories holding the same bundle version
  hold different property sets — which is the point, and still a thing a
  reader has to know.
- Bad, because the stored declaration ROW does not carry the synthesised
  property: the row is the document, and the document does not declare it. The
  console reads declarations from those rows, so a synthesised slot is invisible
  there until the console reads the repository's mappings beside them. That is
  the one piece of this change left undone, and it is tracked on #570.
- Bad, because `subjectSlots` — the engine's "this is a mirror waiting for a
  mapping" hint, which turns a pin mismatch into "declare a mapping onto X" —
  now fires only for a kind that declares its own slot. A shipped provider no
  longer does, so that write gets the generic both-readings message instead.
- Bad, because the reconcile copies a kind and a package on every mutation
  that changes a slot. It is cheap (registry-sized, not data-sized) and it runs
  at admission, not per write, but it is a copy the old rule did not make.
- Bad, because an uninstall gains a refusal whose remedy — delete every mirror
  row that linked — is heavier than the one it replaces.

### Confirmation

`TestMappingRules` (`internal/vocabulary`) holds the loader half: a mapping
whose source declares nothing loads and the slot appears pinned, `managed` and
carrying `MappedBy`; `collides with a declared property`, `subject marker
missing` and `collides with a scalar` hold the refusal; `foreign from onto an
owned to` holds the adoption of a declared slot. The provider suites
(`internal/providertest`) assert the shipped github, google and linear closures
declare NO subject slot at all, which is the rule stated from the other side.
The engine suite's mapping tests run the whole projection against a registry
whose slots are synthesised rather than declared.

## More Information

Supersedes item 3 of
[0049](0049-the-owner-of-a-mappings-target-declares-it.md) and nothing else of
it. Companion to
[0084](0084-a-reference-may-pin-a-mapping-source-kind.md): together they let a
provider kind be written as the complete, first-class object its API describes,
with references for every relation and no pointer at a vocabulary it does not
own. `docs/kind-design.md` in geoah/mneme-v6 is the design rule this unblocks.

Reopen if a repository needs two mappings from one source kind onto one target
kind under two names, which item 2 of 0049 still refuses: that is a different
question — how many links one mirror may hold — and it changes 0049's key
rather than this record.
