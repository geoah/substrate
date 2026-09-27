---
type: fix
---

# GitHub mirrors the owner's review of a pull request they were asked to review

`githubsync` adds a fourth search, `type:pr reviewed-by:<login>`, as the
`pullsReviewed` stage with its own `syncCursors` entry. GitHub drops a user
from a pull request's requested reviewers once they review it, so before this
an approval sent through `submitreview` on a pull request the owner was only
asked to review never reached the `review` mirror (#710). It now lands on the
next sync, for example:

```http
GET /api/v1/records?filter={"kinds":["providers.substrate.reamde.dev/github/review"],"properties":{"state":{"eq":"approved"}}}
```

The first sync on version 26 walks the new search from the account's
`backfillDepth` floor, because the stage has no watermark yet.
