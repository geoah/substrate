---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0259. A repository's own kinds live in the local namespace `~`

## Context and Problem Statement

A repository's own kinds were named under its name: Ada's tasks were
`ada.example.com/tasks/task`, and so was every task, every reference to one,
every declaration, every app token's scope over them and every function that
wrote them ([0046](0046-a-repository-owns-one-authority-chosen-at-registration.md)).
So the name could never change without rewriting the data, two people's copies
of the `tasks` sample were two unrelated kinds, and an export could not move
to another name. [0261](0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md)
took storage off the name; the kinds were the name's last hold on the data.
The 1.0 plan fixes the answer (#1313, F2's points 3 and 8, F4's point 2, row
16); this record says how it is held.

## Considered Options

- Keep own kinds under the name, and rewrite every mention when it changes, as
  the one move done by hand did for 2,380 records.
- Name own kinds under the repository's id (`rnd-…/tasks/task`).
- A local namespace, `~`, meaning "this repository's".

## Decision Outcome

Chosen: the local namespace. A repository's own packages are stored and
addressed as `~/tasks`, `~/tasks/task`, `/api/v1/~/tasks/task/01JD8…`, and a
reply always says `~`. A rename then touches nothing stored, and two copies of
a sample are one kind from one origin. The id would also have left the name
out of the data, but it is opaque and unique, so two copies would still differ
and every reference would show it; a rewrite is the cost this decision removes.

- **The full current name is another spelling of `~` on every input**, folded
  where a reference enters: the registry's lookups (a record path, a body's
  `kind`, a filter, a trigger's or a policy's selector, a function or agent a
  body calls), a reference value at the write, a declaration's id at the API,
  the documents of a vocabulary apply and of a declaration's put, an app's
  scopes when its token is minted and when its request is checked.
- **A string moves because of what it is, never because of what it looks
  like.** A kind, a record id that is a name (a declaration's, a setting's, a
  secret's), a reference value where a declaration puts one, a declaration's
  keys that name something (its `authority`, a pin, a trait, a grant), and the
  names the substrate's own records hold at named properties (a token's
  scopes, a trigger's watched kinds, a policy's selector, an approval's grant)
  move. The person's text and values stay as written whatever they spell: a
  description, a prompt, a function's source, a default, a setting's or a
  secret's value, a `json` property's inside, an actor text.
- **A sample is copied into `~`**, landing at `~/<package>` with its origin
  stamp, and the authority document of the sample's authority is not carried:
  the local namespace has no publisher to describe. The sample's authority is
  read by the same rule: where the closure names something, and, in the two
  texts its code reads (a function's source, an agent's prompt), only as a
  kind reference spelled whole, with nothing of an address around it. That
  textual rule is exact for what ships, which a test over every sample holds,
  and nothing else is ever copied. This amends
  [0048](0048-providers-are-published-samples-are-copied.md) and
  [0070](0070-a-copy-is-upgraded-through-its-origin-stamp-and-requires-pins-a-floor.md),
  which copied it under the repository's name.
- **A kind is named in full, and `~` is the one short form**: the authority of
  a reference is a DNS name or `~`, the one without a dot, and a record id may
  open with `~/`. This amends
  [0042](0042-every-kind-carries-an-authority.md),
  [0098](0098-a-declaration-names-a-kind-or-trait-in-full.md) and
  [0101](0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md),
  and [0014](0014-authorities-widen-only-outside-the-id-alphabet.md),
  whose authority keeps a mandatory dot and widens only with characters the
  id alphabet excludes: `~` has no dot and is in that alphabet. A path still
  splits by grammar alone, because `~` is only ever a whole first segment,
  never part of a DNS name, and the alphabet gains no character: only its
  first character may now be `~` before a `/`.
- **A token declares new packages only under `~`.** An app's token stores its
  scopes over own kinds as `read:~/tasks/*`, as the app asked.
- **A local package's hands carry `~` as the package spells it:**
  `bundle:~:tasks`, `function:~:tasks:remind`, `agent:~:tasks:helper`. A hand
  is an actor record whose id is derived from the hand
  ([0270](0270-every-hand-that-writes-is-an-actor-record.md)), so the record
  is the same whatever the repository is called, and nothing about it is
  left to rename.
- **Existing data moves once, by the conversion at 1.0**, by the same rule:
  each history line's kind and its effects' kinds and ids, its reference
  values where a declaration of the record's kind (any version the history
  holds) puts a reference, a declaration's name keys, and the named
  properties of the substrate's own records. Each sealed value is sealed again
  bound to its record's new kind and id, and the vectors take them too. Each
  actor record id derived from a hand that carried the name becomes the id of
  the same hand under `~`, at every position a line holds one: the line's
  actor and the one it acted for, a manager's actor, a link to an actor
  record, and the actor record's own lines.
- **An export imports under another name** with
  `substratectl archive import <id> <archive> --name <name>`: the name is in
  the archive's manifest and in the two records that record it, the
  repository's own and its credential, which take the new name at its first
  open.

### Consequences

- Good, because the name is in the manifest and nowhere in the data of the
  repository's own kinds, so renaming (step 10) and importing under another
  name rewrite nothing.
- Good, because two repositories' copies of a sample are one kind, which is
  what sharing between them will need (F4).
- Bad, because a client that stored `ada.example.com/tasks/task` keeps working
  only while that name is the repository's, and should store `~`.
- Bad, because `~` needs quoting where it means something else: a shell
  expands an unquoted `~/` to the home directory (substratectl reads that form
  back), and YAML reads a bare `~` as null, so `authority: "~"` is quoted.
- Bad, because what the owner wrote as text keeps the name: a function's
  source or an agent's prompt that spells an own kind by the full name works
  only while that name is the repository's, and should spell `~`.
- Bad, because a client that kept the id of the actor record of one of the
  repository's own hands finds another id after the conversion.

### Confirmation

`TestOwnKindsAnswerUnderTheLocalNamespaceInEitherSpelling` and
`TestTwoRepositoriesCopiesOfOneSampleAreOneKind` (`internal/api`),
`TestTheRepositorysNameNeverEntersWhatItStoresOfItsOwn` (`internal/engine`),
`TestAnArchiveSetAsideForItsNameImportsUnderAnother` (`internal/testenv`),
and the conversion's own tests, `TestLocalizeLineMovesOwnKindsAndLeavesTheRest`,
`TestTheRepositorysOwnHandsMoveToTheLocalNamespace` and
`TestCarryLocalResealsAndRekeysTheRowsOfOwnKinds`.

## More Information

The step that publishes a package from `~` under a proven name is the plan's
step 12; renaming a repository, and how long an old name resolves, is step 10.
