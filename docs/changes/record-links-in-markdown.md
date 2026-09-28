---
type: feature
---

# The console edits `markdown` properties as documents with record links

The console renders every single `markdown` property as Markdown and edits
it in place: the record's body, a `markdown` row on the property sheet, and a
`markdown` field on the create and edit forms. `/` opens a block menu, and
`@`, ⌘K or a collection picked under `/` opens a record search. The value
stays plain Markdown. A record link is a Markdown link whose target is
`substrate://` followed by the record path (decision record 0137), and the
console renders it as the record's chip, linked to its page and titled with
its current title. A writer that means a record in prose (an agent, a script)
writes the same link.

```markdown
Agree the roadmap with [Ada Lovelace](substrate://ada.example.com/people/person/ada).
```

The link is prose, not a reference property: the substrate does not index it,
and the linked record's "Connected to" list does not show it. A repeated
`markdown` property still edits one plain-text item per line.
