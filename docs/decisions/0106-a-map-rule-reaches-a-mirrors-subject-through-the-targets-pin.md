---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0106. A map rule reaches a mirror's subject through the target's pin

## Context and Problem Statement

A consumer mapping `github/issue` onto `task` wanted the task's `assignee`,
and the person is one hop past the issue: issue, then `github/user`, then the
user's `person` slot. [#580](https://github.com/geoah/substrate/issues/580)
reported that a map rule cannot express it and proposed a path through the
reference, `{path: "assignees[].person"}`. The loader refuses that path, so
the consumer wrote a function per provider to patch the assignee on.

The relation was already expressible. A map rule `{path: assignee}` copies
the `github/user` reference, and the recompute writes it into the
person-pinned slot, where the subject hop
([0095](0095-a-reference-may-pin-a-mapping-source-kind.md)) stores the
user's person. Two defects hid it: the offer behind the value still spelled
the `github/user`, so every read showed the value's own source as an
alternative, and two users of one person reached a repeated target as the
same person twice.

## Considered Options

- Extend the path grammar: `a.b` and `a[].b` cross a reference when `b` is
  the referent's subject slot, as #580 proposed
- Keep the grammar: copy the reference, let the target's pin resolve it, and
  make the offer and the dedupe agree with what the hop stores

## Decision Outcome

Chosen: the second. The hop is already the one place a mirror becomes its
subject, for a connector's own writes and for a recompute's. A second
spelling would read the subject slot without the hop's minting and ambiguity
rules, and give one relation two spellings. The target's pin is also the
honest place for the choice: `task.assignee` pinned at `person` says what the
task holds, whichever mirror a mapping copies into it.

A path that crosses a reference stays refused, and the refusal names the
spelling that works. The recompute reads each reference contribution through
the mirror's stored subject before it offers or selects it
(`internal/engine/mapping.go` `throughSubjects`), so the offer equals the
stored value and duplicates collapse. It reads and never mints, because a
rebuild re-derives offers and must append nothing; a mirror with no subject
yet is left to the write's own hop.

### Consequences

- Good, because a consumer declares the relation in its manifest, with no
  function and no grammar change, against every provider whose mirrors have a
  mapping onto the pinned kind.
- Good, because the value, its offer and its provenance agree, so a read
  shows the source and no phantom alternative.
- Bad, because the value follows the mirror's subject only when the SOURCE is
  next written. A split that moves `github/user` to another person leaves the
  task on the old person until the issue syncs again.
- Bad, because the spelling is implicit: a reader has to know the target's
  pin decides, which is why the loader's refusal and
  [projection.md](../projection.md#record-mappings) both say it.

### Confirmation

`TestMappedReferenceLandsOnTheMirrorsSubject` (`internal/engine`) holds the
stored value, the offer, the dedupe and the no-op re-sync.
`TestMappingRules/a_map_path_never_crosses_a_reference` (`internal/vocabulary`)
holds the refusal and its message.

## More Information

Builds on [0095](0095-a-reference-may-pin-a-mapping-source-kind.md), which
let a mirror pin a mapping source, and on the bipartite rule of
[0049](0049-the-owner-of-a-mappings-target-declares-it.md), which keeps the
hop one level deep. Reopen if a consumer needs a mirror's subject in a slot
that cannot be pinned at the subject kind, such as an unpinned reference.
