---
type: feature
---

# Slack, Beeper and GitHub providers ship `postmessage`, `sendmessage` and `submitreview`

Each of the three providers ships one write function, callable through
`POST /api/v1/substrate.reamde.dev/core/function/{name}/call` or named as an
agent tool. No trigger fires them. Each spends the credential the provider's
sync already uses and writes no record; what it sent reaches the mirror only
when the sync reads it back. Two sends are never read back: a Slack first
reply in a thread that had no replies (#711), and a GitHub review by an owner
who was only asked to review (#710). Do not wait for the mirror to confirm a
send before retrying.

- `providers.substrate.reamde.dev/slack/postmessage` takes `channel`, `text`
  and an optional `threadTs`. The pasted user token needs Slack's
  `chat:write` user scope: a token minted for the read-only sync lacks it,
  and the call then fails with `missing_scope` until the token is reminted
  with the scope and pasted on the `config` record again.
- `providers.substrate.reamde.dev/beeper/sendmessage` takes `chat`, `text` and
  an optional `replyTo`.
- `providers.substrate.reamde.dev/github/submitreview` takes `repository`
  (`owner/name`), `number`, `event` (`approve` or `comment`), `body` and an
  optional `account`. It needs the `repo` scope, which the pull request,
  issue and repository toggles already ask for.

```http
POST /api/v1/substrate.reamde.dev/core/function/providers.substrate.reamde.dev%2Fslack%2Fpostmessage/call
Authorization: Bearer <token>
Content-Type: application/json

{"input": {"channel": "C0123456789", "text": "On it.", "threadTs": "1747674380.100249"}}
```
