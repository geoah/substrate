---
type: fix
---

# A repository that needs a restart answers `503 unavailable`, not `500 internal`

A client that disconnected while its write committed could leave the
repository refusing every later write with `500 internal` until the server
restarted (#516). A write that is ready to commit now commits whatever the
client does, and a commit whose answer was lost after Postgres applied it is
appended to the directory by the next write, so neither case refuses
anything.

When that repair fails (the changelog writer or the directory is broken),
every write to the repository answers `503` with `Retry-After: 30`, and the
message names the restart:

```http
HTTP/1.1 503 Service Unavailable
Retry-After: 30

{"error":{"code":"unavailable","message":"substrate: refused until the server restarts: the repository directory is behind the tables after a failed write; restart the server so the boot check catches it up: repository alice.example.com: ..."}}
```

A client that waits `Retry-After` on a `503` and retries keeps working. The
operator restarts the server, and the boot check catches the directory up.
