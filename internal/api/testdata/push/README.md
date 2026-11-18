# The server's half of the push fixtures

What a substrate server answers an app that takes pushes
(docs/plans/push.md, sections 3 and 4). `internal/api`'s `push_db_test.go`
runs each through the real handler on the real engine: the device put's
bodies and answers, and the answer to `GET /api/v1/push/events/{eventId}`
for each event, whose keys and kinds a live answer must match. The ids in the
answers are examples; a live answer carries its own. The relay's half (the
requests, the signatures, Apple's payloads) is `internal/pushwire/testdata`.

| File | Holds |
| --- | --- |
| `device-put.json` | the device put at the token's own id: its first body (201), an update that leaves `notifies` out (200), and the refusals |
| `event-reply.json` | a `reply`: the message, at its version |
| `event-failed.json` | a `failed`: the thread, at the version that settled |
| `event-request-card-1.json`, `event-request-card-2.json` | two cards on one thread: one the asking token may decide (`actions: true`), one it may not (`actions: false`) |
| `event-read.json` | a `read`: the fingerprints of the alerts it takes back |
