---
type: feature
---

# The console edits a record's `markdown` body as a document with record links

The record page reads its body property (the kind's `markdown` property, see
`bodyProperty` in the console) as rendered Markdown and edits it in place:
`/` opens a block menu, and `@`, ⌘K or a collection picked under `/` opens a
record search. The value stays plain Markdown. A record link is a Markdown
link whose target is `ref:` followed by the record path, and the console
renders it as the record's chip with its current title. A writer that means a
record in prose (an agent, a script) writes the same link.

```markdown
Agree the roadmap with [Ada Lovelace](ref:ada.example.com/people/person/ada).
```

The link is prose, not a reference property: the substrate does not index it,
and the linked record's "Connected to" list does not show it.
