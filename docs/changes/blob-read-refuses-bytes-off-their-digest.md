---
type: fix
---

# `GET /api/v1/blobs/{digest}` answers `500` when the stored bytes do not hash to the digest

A blob read now hashes the stored bytes before it sends them. Bytes that do
not hash to the digest in the path, or are not the size the manifest
declares, were served with `200`; they now answer `500` with code `internal`
and a message naming the digest, and nothing of the blob is sent:

```http
GET /api/v1/blobs/blob-sha256-4f2a…

HTTP/1.1 500 Internal Server Error
Content-Type: application/json

{"error": {"code": "internal", "message": "substrate: stored data is corrupt: blobbytes: the stored bytes do not match their digest: blob-sha256-4f2a… hashes to blob-sha256-9c01…"}}
```

A client may rely on a `200` from this route: its body hashes to the digest.
The export (`GET /api/v1/export`) stops before a damaged blob's last byte and
cuts the connection. `substratectl repository verify` lists every damaged
blob; the repair is copying the file `blobs/<digest>` back from a backup.
