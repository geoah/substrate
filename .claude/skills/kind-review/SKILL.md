---
name: kind-review
description: Review the kinds a change adds or renames under kinds/ and samples/ for family fit, reference use and plain descriptions. Use when asked to review a vocabulary change, before merging a pull request that declares or renames a kind, and when choosing a name for a new kind. Advisory and run on demand; `mise run kinds:check` is the gate.
---

# Kind review

`mise run kinds:check` refuses the part of naming a kind that a program can
decide: a dead word from [docs/terms.md](../../../docs/terms.md) in a new kind
id, and a name that ends like a family member without the family's stem
(`cmd/vocabularydiff/names.go`). This skill is the part that needs reading:
whether a new or renamed kind fits the package it joins, whether its links are
references, and whether its descriptions say what one record is. It reports
findings. It changes files only when asked to.

## 1. The gate first

```bash
git fetch origin
mise run kinds:check
```

Fix what it refuses before reading anything. The same check also holds
version bumps and retired names
([0055](../../../docs/decisions/0055-a-retired-name-is-declared-and-never-inferred-from-a-prune.md)).

## 2. Find the new and renamed declarations

```bash
git diff --name-status origin/main...HEAD -- kinds/ samples/
git diff origin/main...HEAD -- kinds/ samples/ | grep -E '^[-+]  id: '
```

A `+  id:` line with no `-  id:` beside it is an added declaration. A `-` and
`+` pair in one file is a rename. Read each whole declaration, then list the
kinds of the package it lands in:

```bash
grep -rh -A1 '^metadata:' kinds/<authority>/<package>/ | grep '  id: '
grep -rh -A1 '^metadata:' samples/<package>/ | grep '  id: '
```

## 3. Family fit

For each new name, compare it with the package's other kind names:

- **The package's own spelling.** Core spells its record machinery
  `record<verb>` (`recordmerge`, `recordsplit`, `recordmapping`) and the
  things around one verb as that stem plus a noun (`recordpatchpolicy`,
  `recordpatchrequest`, `recordmergerequest`). A new name reuses the stem and
  the noun the package already has: `recordpatchpolicy`, not `writepolicy`,
  and `request`, not `proposal`, where the package already says request.
- **The owner first.** A kind whose records belong to one other kind starts
  with that kind's name: `triggerrun`, `calendarevent`,
  `conversationmessage`.
- **The upstream's noun in a provider.** A provider kind keeps the name the
  upstream uses (`contactgroup`, `issuetype`), prefixed with the service
  where one provider syncs several (`gmailmessage` beside `calendarevent`).
  A dead word is the exception: Notion's "integration" is written around, in
  the docs and in a kind name alike.
- **The dead words the gate lets through.** `type`, `group`, `log`,
  `schema`, `extension`, `edge`, `incoming` and `plural` each have an honest
  compound, so the gate refuses them only as a whole name or glued to a live
  word (`recordtype`, `kindgroup`); the list and each reason are
  `honestCompounds` in `names.go`. Any other compound is read here:
  `fieldtype` for what core calls a property type is wrong, and GitHub's
  `issuetype` is the upstream's noun and stays.
- **A common ending.** The gate refuses a name ending like a family member
  even when the idea is new (`accessrequest` beside `recordpatchrequest`).
  If the new kind is not a member, start its name with the kind it belongs
  to (`repositoryaccessrequest`).

## 4. Links are references

A reference is the only link between records
([0044](../../../docs/decisions/0044-a-reference-is-the-only-link-between-records.md)):

- A property that holds another record's id or path is `type: reference`
  with its `kind:`, never a `string`.
- Data about the link itself goes in the reference property's own
  `properties:` block. `organizations` on
  `samples.substrate.reamde.dev/people/person` carries the `role` held there.
  A new kind whose records only join two others and carry the link's data (a
  `membership` holding a person, an organization and a role) is that
  reference, declared on one end.
- A kind for the link is right when the link has a life of its own: states,
  references of its own, a title people look for. `recordpatchrequest`
  names its `target`, `policy` and `thread` and moves through its own states.
- The other end reads a link through `filter.referencing`, and the
  reference's `inverse:` names that side (an agent's `threads`). A second
  reference declared back from the other end duplicates the link.

## 5. Plain descriptions

Hold each `description:` to the repo's writing rules:

- A kind's description says what one record is, in a sentence a newcomer
  understands without the rest of the package open.
- A property's description says what the value is, with its unit or format
  where it has one.
- No dead words, no house metaphors, no evaluative words ("robust",
  "seamless").
- A kind with a heading declares its own property and a `displayTemplate`;
  the built-in `title` is never its input
  ([0016](../../../docs/decisions/0016-a-kind-titles-itself-from-a-declared-property.md)).

## 6. Report

One block per declaration: its id, then one line per finding under the
section that found it, each with the fix. Say "no findings" for a clean
declaration. Findings are advice; the author decides.

## Worked example

A change adds this declaration to core, beside `recordpatchpolicy` and
`recordpatchrequest`. The name is the one a draft in PR 73 used before review
caught it; the rest of the declaration is made up for the example.

```yaml
kind: substrate.reamde.dev/core/kind
metadata:
  id: substrate.reamde.dev/core/writepolicy
data:
  authority: substrate.reamde.dev
  package: core
  description: >-
    Governs the write door for agentic mutations.
  names:
    singular: writepolicy
  properties:
    request:
      type: string
      description: the recordpatchrequest id this policy last gated
```

`mise run kinds:check` refuses it first:

```text
substrate.reamde.dev/core/writepolicy.yaml: kind substrate.reamde.dev/core/writepolicy ends in "policy" like substrate.reamde.dev/core/recordpatchpolicy but starts with neither that family's stem "recordpatch" nor another kind of the package; spell a member of the family with its stem (recordpatchpolicy, recordpatchrequest), or start the name with the kind it belongs to
```

The review then reports what the gate cannot see:

```text
substrate.reamde.dev/core/writepolicy
  family fit: core already has recordpatchpolicy for the writes of agents
    and functions. Extend that kind rather than declaring a second one; a
    new kind in this family is recordpatch<noun>.
  links: `request` holds a record id as a string. Declare it as
    `type: reference` with `kind: substrate.reamde.dev/core/recordpatchrequest`.
  description: "the write door" is a house metaphor and "agentic mutations"
    names nothing a reader can look up. Say what one record is: "One rule for
    the writes of agents and functions: which writes it matches, and whether
    they land, wait for the owner, or are refused."
```
