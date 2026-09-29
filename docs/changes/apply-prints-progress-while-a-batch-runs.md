---
type: fix
---

# `substratectl apply` prints progress to stderr while a vocabulary batch runs

Before this release, `substratectl apply -f` printed nothing until the server
answered a vocabulary batch; a batch of 96 documents onto a repository with
336k records was ten minutes of silence. While the request is in flight,
`apply` now prints a line to stderr every 10 s, and stdout carries the same
summary as before. A script that treats any stderr output as a failure sees
these lines on a slow apply that succeeds.

```
$ substratectl apply -f kinds/capture/*.yaml -f kinds/tasks/*.yaml
applying 96 documents in 12 packages, 10s elapsed
applying 96 documents in 12 packages, 20s elapsed
package/example.com/capture applied
```

The server logs each step of the batch at info as the step starts, so an
operator tailing the log sees where the batch is while it holds the registry
lock. A step that can run long (waiting for the batch ahead, preparing
function bodies, building an index, a walk over stored records) logs only
when it has work:

```
time=2026-09-29T12:24:11.911Z level=INFO msg="substrate: vocabulary apply: holding the registry lock, checking the batch against the stored records" repository=ada.example.com documents=5 packages=2
time=2026-09-29T12:24:11.912Z level=INFO msg="substrate: vocabulary apply: writing the declarations of one package" repository=ada.example.com package=progress.example.com/depot declarations=2 index=1 packages=2
time=2026-09-29T12:24:11.929Z level=INFO msg="substrate: vocabulary apply: writing the declarations of one package" repository=ada.example.com package=progress.example.com/shop declarations=3 index=2 packages=2
time=2026-09-29T12:24:11.943Z level=INFO msg="substrate: vocabulary apply: re-deriving the search index" repository=ada.example.com kinds=3
time=2026-09-29T12:24:11.948Z level=INFO msg="substrate: vocabulary apply: committed" repository=ada.example.com took=42ms
```
