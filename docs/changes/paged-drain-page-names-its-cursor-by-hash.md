---
type: fix
---

# A paged drain's page entry names its cursor by hash instead of copying it

Each page of a paged drain appended a `delivery` changelog entry carrying the
body's whole resume cursor. Slack's cursor is about 380 KB at 9,000 pending
items, so its triggers grew the segment files by that much on every page. A
page entry now records the cursor's `cursorSha256` and `cursorBytes`, and the
cursor stays in the `paged_cursors` table:

```
{"kind": "page", "ref": "substrate.reamde.dev/core/trigger", "id": "<trigger>",
 "page": {"chain": "<chain>", "cursorSha256": "<SHA-256 of the cursor JSON>",
          "cursorBytes": <its length>, "version": <n>, "pages": <n>,
          "effects": <n>, "bytes": <n>, "startedAt": "<first page>",
          "kind": "fire", "identity": "<fire id>"}}
```

A park still writes the cursor whole into its entry, so a parked drain
resumes from its last committed page after a restore, as before.
`substratectl repository rebuild` keeps every cursor the database holds.

One case behaves differently after an import of a repository directory into
an empty database (a restore from `substratectl export` or a copied data
root): a paged drain that stopped between pages without parking (the server
crashed or was stopped mid-drain) starts over from its first page, where it
used to resume. Its next delivery re-runs the pages it had committed.

Entries written before this release keep their cursors and replay as they
did; nothing rewrites them, so the space they take stays. Decision record
0141 has the design.
