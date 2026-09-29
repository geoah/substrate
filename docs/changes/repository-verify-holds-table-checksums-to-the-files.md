---
type: fix
---

# `repository verify` holds table checksums to the files; `--recanonicalize` recomputes them

`repository verify` and `repository snapshot` no longer recompute every
changelog table row's checksum from its stored columns. The table pass holds
each row's stamped checksum to the `sum` its line carries, and the file pass
checks each line without decoding it, several segments at a time. On a
history of 2 million entries the old walk took hours. A row edited in the
database with its checksum left alone is now found only with the new flag:

```
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository verify --recanonicalize ada.example.com
```

The snapshot's read-back hashes each copied finished segment against its
copied sidecar and the source's digest instead of walking its lines.
