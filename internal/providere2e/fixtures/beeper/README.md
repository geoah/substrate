# The Beeper fixture set

37 recordings, ~244 KB, cut from the owner's own pull of a 476-chat inbox and
pseudonymised. `audit.py` proves it is publishable; it must print
`FINDINGS: 0`, and the commit is gated on that.

```bash
python3 providers/beeper/fixtures/curate.py --out /tmp/beeper-cut
python3 tools/pseudonymise.py /tmp/beeper-cut providers/beeper/fixtures
python3 tools/rawpull/beeper.py --repair providers/beeper/fixtures
python3 providers/beeper/fixtures/audit.py
```

The cast — WHICH seven chats — is in `cast.local.json`, which is gitignored: a
real room id beside "the family group" re-identifies the chat whatever the
pseudonymiser did to the payload. `curate.py --survey` re-derives the
candidates from `raw/beeper` for anyone holding the pull.

## What the cut proves, case by case

| Case | Where |
| --- | --- |
| a direct chat whose roster is COMPLETE in the list | a two-person chat: `participants.hasMore` false, so the list read and the authoritative read agree |
| a roster the LIST TRUNCATES at 20 | a 69-member group: only `GET /v1/chats/{id}?maxParticipantCount=-1` returns all of them, and the e2e asserts the mirror holds 69 |
| a message with NO `type` at all | one message in that group. The schema does not require `type`, and the e2e asserts it is mirrored without one rather than refused |
| every other message type in the corpus | TEXT, NOTICE, IMAGE, VIDEO, VOICE, REACTION |
| a voice note with a transcription | `isVoiceNote`, `duration`, and the only `transcription` object in the pull |
| `sendStatus` | one message, on a second page |
| reactions, including a REACTION row of its own | Beeper mirrors a reaction both as an entry on the message and as a hidden message with `isHidden: true` |
| mentions that resolve | a group whose `mentions` name participants of the same chat |
| link previews with `imgSize` | three chats |
| an edited message, a deleted message | `editedTimestamp`, `isDeleted` |
| a `seen` map keyed by participant | the three-way union the bundle keeps as `json` |
| a MERGED chat | `merge.chatIDs` and `merge.defaultChatID`, whose member chats the list hides — the thin-row case |
| four bridged networks | so `chatAccount`, `network` and the per-bridge id namespaces are all exercised |
| list pagination | two `/v1/chats` pages split on a REAL cursor |
| backfill pagination | a second history page per chat, `direction=before` from a real cursor |
| an INCREMENTAL walk | one `direction=after` page per chat, carrying a message the first sync cannot have seen |
| address books | one contact page per bridge, trimmed to the cast's own people plus a few |

## The three curated seams

A raw pull carries no fabricated seams (`docs/provider-practices.md` §1), so
the ones a test needs are cut here and named. All three are REQUIRED in `e2e`
mode and merely asserted-where-present in `seed` mode.

1. **the terminal page.** The cut's last history page per chat says
   `hasMore: false`, so the backfill ends inside the fixture set instead of
   asking for a page nobody recorded.
2. **the forward page.** `direction=after` from a stored cursor is a request
   that did not exist when the pull ran. Its recording is cut from page one:
   the newest message moves out of the backfill into a forward page keyed on
   the cursor the first page REALLY returned, and the chat list's `preview`
   moves back to the message behind it — which is what the preview WAS at the
   moment that list was read. **No cursor is invented**: the page's own are
   opaque values Beeper produced, and the forward page hands back the one it
   was given, so a third round asks the same question and stops.
3. **the merged chat.** Beeper hides a merged chat's member chats from
   `/v1/chats`, so the pull holds no read of them and none of the merged chat
   itself. Its list entry carries a complete roster, so the authoritative read
   is that entry with `preview` removed — a trim. Its history page is EMPTY,
   which is what the schema says that route returns ("a merged chat holds no
   messages of its own").

## Two repairs the shared mapper cannot make

`tools/rawpull/beeper.py --repair` runs after the map, and does two things
nothing else can:

- **JSON MAP KEYS.** `tools/pseudonymise.py` walks VALUES, which is right for
  every other provider here and wrong for Beeper: `Message.seen` is keyed by
  participant id and `ChatDraft.attachments` by attachment id. The first cut
  published 47 real MXIDs that way, every one of them a dictionary key no
  value-walker could see. They are re-keyed through the SAME functions the
  values went through, imported from `tools/pseudonymise.py`, so a key and its
  value can never disagree — and `audit.py` scans keys in their own right so
  the repair cannot quietly stop running.
- **ONE OBJECT READ TWICE.** The mapper seeds its generated prose on the JSON
  PATH, so one real chat `description` becomes `items[3].description` in the
  list and plain `description` in the single read, and the mapper invents a
  different sentence for each. The mirror then flipped that property on every
  sync. The same applies to a chat's `preview`, which is one of that chat's own
  messages. The list's spelling wins for a chat, the history page's for a
  message; nothing structural is copied, because the roster genuinely differs.

## What this set does NOT cover

A green run over a set that cannot reach a case proves nothing about that case.

- **no labels.** The owner has none: `GET /v1/labels` answers `[]`, so
  `beeper/label` and `chat.labels` ship with no fixture behind them. The
  declaration is the schema's and is UNVERIFIED against a payload.
- **no `@room` mention.** No message in the pull carries the sentinel, so
  `message.mentionsRoom` is declared and never exercised.
- **no STICKER or LOCATION message, and no FILE or AUDIO one inside a cast
  chat.** The enum holds them; the corpus does not.
- **no draft, reminder or snooze on a cast chat.** Nine chats in the wider pull
  carry a draft; none of them is one of the seven with history, so
  `chat.draft`, `chat.reminder` and `chat.snooze` are declared and unexercised.
- **no local (self-hosted) bridge with an account on it.** Every connected
  account here is `provider: cloud`, so `selfhosted`, `local` and `platformsdk`
  are declared enum values with no row behind them.
- **no failure matrix.** `tools/mockserver.py` can inject 429s, 500s and 404s
  per endpoint; this set does not use it, so the retry classes, the
  `retryNotBefore` deadline and the permanent/transient split are exercised by
  reading, not by running.
- **no second account.** One token addresses one Beeper Desktop, and the
  duplicate-account path (`ignored: duplicate account`) has no fixture.
- **no multi-drain continuation.** The cut fits in one drain, so the bounded
  handoff — the queue on the account, `ok (N pending)`, the continuation
  through `syncRequestedAt` — is exercised only by the owner's seed.

Every one of these is a coverage gap of the PULL, not of the mapper:
`tools/rawpull/beeper.py` records what the sync asks for over a `--days`
window and a `--top` ranking, and a case outside that window is simply not in
the recordings. `docs/tickets/T-008` is where pull gaps live.
