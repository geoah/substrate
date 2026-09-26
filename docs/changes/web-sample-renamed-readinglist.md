---
type: breaking
release: v0.76.0
---

# Rename the `web` sample to `samples.substrate.reamde.dev/readinglist`

The catalog no longer lists `samples.substrate.reamde.dev/web`. Its
successor is `samples.substrate.reamde.dev/readinglist`, which reads its deny
list from a core `setting` record (`<authority>/readinglist/denyDomains`,
decision record 0076) instead of the `web/config` kind and its `connector`
input. This hits a repository that imported `web` before v0.76.0: its copy
keeps working, but no catalog entry offers it an upgrade.

```bash
before: substratectl import samples.substrate.reamde.dev/web
after:  substratectl import samples.substrate.reamde.dev/readinglist
```

## What to do

1. If you never imported `web`, nothing.
2. If you did and want the maintained version, import `readinglist` and
   write the deny list into its `denyDomains` setting. The two packages have
   different kind references, so records of `<authority>/web/page` are not
   carried to `<authority>/readinglist/page`.
