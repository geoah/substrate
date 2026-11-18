# The push wire's fixtures

Every side of the push wire tests against these files: the relay
(`internal/pushrelay`), the server, and the apps through copies of the ones
they read. They are written by `TestFixtures` in `internal/pushwire` from the
code, and a test fails when a file and the code disagree:

```bash
go test ./internal/pushwire -run TestFixtures -update
```

A diff here is a change of the wire. Read it before you commit it, and bump
the path's version (`/v2/`) rather than change the meaning of version 1.

Every key and token in them is derived from a label in the test. None is
anybody's, and none is an Apple key.

| File | Holds |
| --- | --- |
| `keys.json` | the relay's seal key, the server's signing seed and its public half, a second key that signs nothing a push key names, the topic, when the push keys were sealed (`registeredAt`) and when every push was sent (`now`) |
| `register.json` | an app's register request, made at `registeredAt`, and the relay's answer: the push key every push fixture but the largest carries |
| `push-reply.json` | a `reply`: the signature, the exact signed body, what the push key seals, the request the relay makes to Apple and the relay's answer |
| `push-failed.json` | the same for `failed` |
| `push-request-card-1.json`, `push-request-card-2.json` | two cards waiting on one thread: one `thread-id`, two event ids, and no `apns-collapse-id`, so Apple delivers both |
| `push-read.json` | a `read`: the badge and `content-available`, priority 5 |
| `push-reply-largest.json` | every bound at its largest: a 400-character Apple token, a 128-character thread (too long to collapse by), badge 999 and a 1,024-character event id, on `development` |
| `errors.json` | every refusal the relay answers, one per row of the plan's table, 413 among them |
| `signatures.json` | an Ed25519 seed, a time, a body, the bytes signed and the header; then cases: inside the window either way, stale either way, another key, a changed body, a malformed header, and a repeat, which verifies and which a relay refuses as `replayed` |
| `fingerprints.json` | event ids and the fingerprint (`pushwire.EventFingerprint`) a `read` and the app name each alert by |
| `endpoints.json` | relays and the two addresses each makes (`pushwire.Endpoint`), and relays it refuses |

`internal/pushwire/pushwiretest` reads them for Go tests.
