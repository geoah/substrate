---
type: feature
---

# `GET /api/v1/changes?runs=1` returns run summaries

The history page now has a run form: consecutive changes of the filtered feed
grouped by actor, kind and verb (`create`, `restore`, `update`, `delete`,
`merge`, `split`, `gc`), each with an exact `count`, its distinct `records`,
`newestSeq`/`oldestSeq` and `newestTs`/`oldestTs`. `first` counts runs, and a
page never ends inside one, so a client can say "60 tasks" from one read
instead of folding rows over several pages:

```
GET /api/v1/changes?runs=1&first=6
GET /api/v1/changes?runs=1&first=6&before=4128&generation=7f3a0c2e9b1d4e6f
```

`cursor` is the oldest run's `oldestSeq` when more rows lie below.
[docs/changelog.md](../changelog.md#run-summaries) has the rules.
