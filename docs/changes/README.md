# Upgrade notes

One file per change that somebody using a substrate has to know about: a
break they must act on, a deprecation they should act on, or a feature or
fix they would not find from its commit title. The reader is the person or
the agent upgrading a deployment or a client, and they read the notes for
every release between the version they run and the one they are moving to.

Each release's notes are the files ADDED to this directory since the tag
before it. `mise run changelog` renders them for every release, newest
first, together with the `feat:` and `fix:` commit subjects, and the release
job puts the same notes at the top of the GitHub release. A file is never
moved or renamed once merged, because the commit that added it is what places
it in a release; fix a typo in place.

## When a note is required

- **A break** (a `!` in the PR title or a commit subject, or a
  `BREAKING CHANGE:` footer) needs a note with `type: breaking`.
  `mise run commits:check` refuses the pull request without one.
- **A deprecation** (`deprecated: true` on a shipped declaration, a route or
  flag that still works and will not) needs a note with `type: deprecated`.
- **A feature or a fix** needs a note only when its commit subject is not
  enough to use it: a new env var, a new CLI verb, a behavior an agent
  should start relying on. Most do not.

The agent review on each pull request (`.github/workflows/review.yml`) reads
the diff against this list and comments when a note looks missing or wrong.
The review is advice; the break rule above is the only one CI enforces.

## The format

The file name is the change in a few lowercase words, `a-z`, `0-9` and `-`,
ending in `.md`. The file opens with a frontmatter block holding one key,
then one `#` heading, then the body.

```markdown
---
type: breaking
---

# `oauth/start` refuses a bare account id

A `POST /api/v1/oauth/start` whose `record` is an account's bare id
(`{"record": "owner"}`) is refused with `422`, and the message lists the
account paths the repository holds under that id. The route takes the full
record path only (decision record 0102).

## What to do

1. Send `record` as the full path:
   `{"record": "providers.substrate.reamde.dev/google/account/owner"}`.
2. Upgrade `substratectl` to 0.93.0 or later; `bundle connect` sends the
   full path from that release on.
```

- `type` is one of `breaking`, `deprecated`, `feature`, `fix`.
- The heading states the change the way a commit subject does: what thing,
  what it does now. Backtick every route, flag, env var and kind reference.
- The body says who it hits and gives one exact example. An agent will act on
  it literally, so show the request or the command, not a description of one.
- `breaking` and `deprecated` notes end with a `## What to do` section: the
  steps, in order, that take a client or a deployment from the old behavior
  to the new. Say when there is nothing to do for someone (for example, when
  a repository migration rewrites the rows at first boot).

`mise run lint:docs` holds the shape: the name, the one key and its value,
the heading, and the `## What to do` section where one is required.
