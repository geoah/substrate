# The Slack fixture set

**Committed, and public.** Every one is a real Slack Web API
response with every identity in it replaced: the workspace, its people, its
channels, its files, its apps, its automation, its custom emoji, its avatars
and its clock. `audit.py` is what says so, and it says **0**.

`bin/mnemectl e2e slack` runs against it.

## What "pseudonymised" has to mean

A rename is not anonymisation. The first version of this set replaced names,
ids and prose and still published 4,592 original avatar URLs, 605 avatar
hashes, 192 Gravatar hashes derived from real addresses, every original
`client_msg_id` and `block_id`, the app / workflow / trigger / usergroup /
call / tab / file id namespaces, three opaque cursors that base64-decode to
original user ids, custom emoji names, two DM bodies that survived inside
`channel.latest.text`, personal profile links, third-party resource UUIDs,
and every timestamp — which is a join key back to the original workspace,
message by message.

So the rule this set is held to is: **a real literal must not survive
anywhere.** Not in a body, not in a nested block, not in a URL path or query,
not inside base64, not in a recording's NAME.

`audit.py` checks exactly that. It traverses every scalar leaf of every
recording, expands each one into every form an identity can hide in (the
value, its URL-decoded self, its base64 plaintext, its path and query
components, its sub-words), and compares against two independent sources of
truth:

1. the persona maps (`tools/persona.local.json`,
   `tools/persona.learned.local.json`) — gitignored;
2. **the raw pull itself**, harvested for every id namespace, avatar hash,
   custom emoji name, non-public host, identifier-bearing URL component,
   cursor and message body it contains.

```bash
python3 providers/slack/fixtures/audit.py            # 0, or it lists them
python3 providers/slack/fixtures/audit.py --paths    # findings by field
```

## Where the real data lives, and does not

| Thing | Where |
| --- | --- |
| the owner's raw pull | `raw/slack/` — gitignored |
| real → fake, every class | `tools/persona.local.json`, `tools/persona.learned.local.json` — gitignored |
| **the cast: which real conversations were cut** | `providers/slack/fixtures/cast.local.json` — gitignored |
| the staged cut, before pseudonymisation | a temp directory, never committed |
| the invented people this set holds | `providers/persona.public.json` — committed, and fake by construction |

The cast used to be a list of real channel ids and names in `curate.py`, and
this file used to print the original-to-pseudonymous mapping for each of
them. Both undid the substitution in the bodies for anybody reading the repo.
Neither is here now, and neither should come back: `curate.py --write-cast`
regenerates the local file from the raw pull for anyone who holds it.

## How the set is built

```bash
python3 providers/slack/fixtures/curate.py --survey        # the window, ranked
python3 providers/slack/fixtures/curate.py --write-cast    # -> cast.local.json
python3 providers/slack/fixtures/curate.py --out /tmp/slack-cut
rm -f providers/slack/fixtures/GET_*.json     # the set is REPLACED, not merged:
                                              # the pseudonymiser writes what the
                                              # cut holds and leaves a recording
                                              # from an older cut standing
python3 tools/pseudonymise.py /tmp/slack-cut providers/slack/fixtures
python3 providers/slack/fixtures/audit.py
```

`curate.py` is the selection step: the raw pull is the whole workspace and a
fixture set is meant to be a readable two days. It picks the cast, cuts each
history page to the window, closes the pagination the cut no longer holds,
and writes the one recording no pull can capture (below). It also writes
`_requests.local.json`, the manifest of what each recording was a response
TO — without it a cursor or a timestamp inside a recording's name cannot be
moved with its body, because a long request slug is truncated and hashed.

`tools/pseudonymise.py` does the rest, under `RULES["slack"]`.

## What the cut proves

Six conversations, named only by the pseudonymous ids the committed set
carries:

| conversation | kind | carries |
| --- | --- | --- |
| `CYN8N70KH7F` | public | a mention, **a message from the account's own user**, a reaction, a thread |
| `C8HHHV78RVH` | public | **a bot message**, **a file share**, threads, reactions |
| `CSP27K97J3K` | private | **a mention of the account's user**, threads, an app-posted message with workflow and trigger ids |
| `C0HDRLQ9144` | **MPIM** | a group DM with a mention |
| `D65MJ8AYJH4` | **DM** | the busier 1:1 |
| `DJYGW778PTV` | DM | a 1:1 with a thread and a file |

Everything `docs/providers-plan.md` asks for is present: two public channels,
a private channel, an MPIM, two DMs, threaded replies, a mention of the
account's user, a message from them, a bot message, a file share and
reactions.

Beside them: `auth.test`, `team.info`, `users.list` (two pages, split on a
cursor), `conversations.list` (two pages, same), `conversations.info` and
`conversations.members` per conversation, `conversations.replies` per
threaded parent, and `users.info` for every user any of it names.

## The four things that are CUT rather than captured

Every other recording is a real response. These are not, and each is labelled
because a fabricated seam nobody points at is a lie about the data.

1. **`…history__channel-<C>_limit-200_oldest-<ts>.json`** — the incremental
   page. The second sync asks each conversation for `oldest=<the ts the first
   sync stored>`, and no pull can have recorded that request: the cursor did
   not exist when the pull ran. The curator cuts it from the conversation's
   own newest message, one second later — a message the first sync provably
   did not see. It is why the cold walk sends no `oldest` at all: a cold
   request carries no wall-clock parameter, so its recording is one the sync
   can reproduce exactly at replay time.

2. **The MUTATION ENVELOPES on that page**, in one conversation: a
   `message_changed` carrying a whole edited message, a `message_deleted`
   naming a `deleted_ts`, and a message re-stated with `reactions: []`. The
   whole set was otherwise append-only and `ok: true`, so nothing exercised
   an edit, a delete or a cleared collection — the three ways a mirror goes
   stale silently.

3. **The UNCHANGED page**, `oldest=<the newest ts the incremental page
   carried>`: an empty `messages` list. The third sync asks with the cursor
   the second one stored, and without this recording the mock answered by
   DROPPING `oldest` and handing back the cold page — a fallback that makes a
   broken replay look idempotent.

4. **The second ROSTER page.** One conversation's `conversations.members`
   response carries 200 members and a real `next_cursor` with nothing behind
   it in the pull. A recording must never advertise a page the set cannot
   serve, so the members are split across that cursor: the set proves the
   union of two pages instead of pretending the question does not arise.
   `users.list` and `conversations.list` are split the same way.

## The hydration recordings (T-021)

`files.info` for every file the cut's messages name and `bots.info` for every
bot they were posted under. Both are REAL responses, and both carry facts no
embedded payload holds: measured across the owner's pull, `files.info` adds
exactly `shares`, `channels`, `groups`, `ims`, `comments_count` and
`has_more_shares` to an embedded file object, and the cut's one bot arrives
with no `bot_profile` at all, so its row is an id until `bots.info` answers.

Two things had to change before they could be published:

* **`files.info` answers `{"file": {…}}`, not `{"files": [{…}]}`**, so every
  `**.files[].<x>` rule in `tools/pseudonymise.py` missed it — `**` matches a
  run of KEYS and there is no `files[]` above `file.url_private`. A `file.*`
  and a `bot.*` family were added beside them.
* **A map keyed by a real id was never touched.** `shares` is
  `{public: {<channel id>: […]}}` and the pseudonymiser walked VALUES only.
  `Rewriter.key` now maps a key when a rule names its path with the `{}`
  segment (`("**.shares.*.{}", "slackid")`), and `audit.py` audits keys — a
  key that is not a lower-snake API field name is a leaf like any other.

## The failure matrix (T-021)

Three seams, each a `__responses` envelope: the FIRST call gets the failure
and every later one gets the real answer, so what the run asserts is that the
mirror RECOVERED rather than that the set is broken. Fabricated and labelled,
under the same contract as the mutation envelopes above.

| seam | on | what it proves |
| --- | --- | --- |
| `429` with `Retry-After: 1` | one `users.info` | the drain ends politely, `retryNotBefore` is stamped and honoured, and the resume picks up the id it was holding |
| `HTTP 500` | one `conversations.info` | a TRANSIENT failure is kept, is retried at the top of the next walk and lands. The conversation is chosen so its `latest` is a message the history page holds anyway — a seam must test the retry, not remove a message from the cut |
| `invalid_cursor` | a roster's second page | a PERMANENT failure is recorded once and never retried, and the next walk re-reads the roster from page one |

And two pages behind a cursor that were never cut: one conversation's
history and one thread's replies are each SPLIT across a cursor Slack really
minted, the same way `users.list`, `conversations.list` and one roster
already are. Without them the cold walk's paging — the branch that decides
whether a 916-conversation backfill ever reaches page two — was never
replayed.

## What the set does NOT yet cover

Named here rather than left to be discovered, and each one is a recording
somebody has to cut:

* no `bot_profile` inlined on a message: no conversation in the cast has one,
  and the bot row is filled by `bots.info` instead;
* **no inlined `user_profile`**, so guide amendment 6's presence-aware merge
  is exercised by the SEED run and not by the e2e: 1,471 of the owner's
  messages carry one and none of them is in the cast's six conversations. A
  cast chosen for its features cannot also be chosen for this;
* **no non-empty `profile.fields`, and none is possible**: the owner's
  workspace defines no custom profile fields at all (0 of 501 users), so the
  only way to cover the keyed map would be to fabricate one — which is now at
  least SAFE to do, since the pseudonymiser maps map keys;
* no missing-scope response, no enterprise-grid user or team;
* no `has_more` page without a cursor (the window continuation);
* no `backfillDepth` other than `all`: the e2e window is deliberately
  time-independent, and testing a bounded one needs recordings older than it.
