# Device-bound and long-tail messengers: what can act as the user

Research report for the substrate per-network connector proposal. Written
2026-11-09. Scope: iMessage/SMS on Apple, Android SMS/MMS/RCS, Viber, LINE,
WeChat, KakaoTalk, Google Chat, Microsoft Teams, Threema, Snapchat, Matrix,
Session, Wire and XMPP. The mautrix **bridgev2 framework** is a sibling
report; here mautrix code appears only as a network library or connector.

Conventions:

- A claim with a link was checked against that page, README or source file
  on 2026-11-09. **⚠️ unverified** marks a claim from a single weak source, from
  memory, or not re-checked.
- Repository facts (stars, last commit, last release, license, archived) come
  from the GitHub API on 2026-11-09.
- "Form" means where the code would run in substrate's terms:
  **(F)** a short-lived sandboxed function (Python, 30 s to 2 min, network
  allowlist, no state); **(S)** a long-lived sidecar next to the server or on
  the user's machine; **(D)** inside our native macOS/iOS/Android app shell,
  pushing to the server's HTTP API with a token.
- Substrate already has both non-function patterns. `cmd/slack-socket` is a
  sidecar that holds Slack's websocket and POSTs batches to a webhook trigger
  that a function processes
  (`kinds/providers.substrate.reamde.dev/slacklive/README.md`). The `apple`
  bundle is a device that writes records and has no sync function
  (`kinds/providers.substrate.reamde.dev/apple/README.md`). Every
  recommendation below fits one of those two shapes.

---

## Summary of conclusions

| Network | Realistic best option | Verdict |
| --- | --- | --- |
| iMessage + SMS/RCS via iPhone relay | On-device macOS connector on [beeper/platform-imessage](https://github.com/beeper/platform-imessage) (MIT, Swift, SIP stays on) | **Feasible, full feature set**, needs an always-on Mac signed in to Messages |
| iMessage on iOS | Nothing a connector can use; Shortcuts "Message" automation is a lossy inbound tap | **Not feasible** |
| Android SMS + RCS | [libgm from mautrix-gmessages](https://github.com/mautrix/gmessages) (Go, AGPL) as a sidecar | **Feasible**; Google account cookies plus an emoji tap; phone must stay online |
| Android SMS/MMS only | Our Android app reading the Telephony provider, or [SMS Gateway for Android](https://github.com/capcom6/android-sms-gateway) (Apache-2.0) | **Feasible, SMS/MMS only**; RCS cannot be read by third-party apps |
| Viber | No public way. Beeper links Viber as a secondary device (closed source) and is reachable through the **Beeper Desktop API** substrate already uses | **Not feasible to build ourselves**; feasible through Beeper |
| LINE | [beeper/line](https://github.com/beeper/line) (MIT, Go; LINE Chrome-extension protocol) | **Feasible with ban and legal risk** |
| WeChat | Beeper (closed source); Tencent's official ClawBot/iLink is a *bot contact*, not the user | **Not feasible to build ourselves** |
| KakaoTalk | Only small unofficial LOCO clients | **Not feasible** (experimental only) |
| Google Chat | Workspace: official Chat API with user OAuth plus Workspace Events. Gmail: [mautrix-googlechat](https://github.com/mautrix/googlechat) | **Feasible** (Workspace cleanly; personal shakily) |
| Microsoft Teams | Microsoft Graph, delegated permissions (work/school) | **Feasible for work/school**; personal Teams **not feasible** |
| Threema | Gateway is bots only; multi-device clients are receive-only experiments | **Not feasible today** |
| Snapchat | Beeper Cloud only; Snap DMCA'd the main mod in 2026 | **Not feasible to build ourselves** |
| Matrix | [mautrix-go](https://github.com/mautrix/go) client (MPL-2.0) | **Native and easiest** |
| Session / Wire / XMPP | Their own open clients and SDKs | **Feasible, low priority** |

The two cross-cutting findings that change the design:

1. **Beeper is now the only working implementation for Viber, WeChat and
   Snapchat as the user, and its local API has a live WebSocket.** Beeper
   added Viber in October 2026, linking as a secondary device like Viber
   Desktop ([Beeper blog, 2026-10-02](https://blog.beeper.com/2026/10/02/september-wrap-up/),
   [Viber guide](https://help.beeper.com/en_US/chat-networks/viber-getting-started-guide)).
   The Beeper Desktop API that substrate polls every 15 minutes exposes
   `/v1/ws` with `message.upserted`, `message.deleted`, `chat.upserted` and
   `chat.deleted` events, plus edit and reaction endpoints
   ([OpenAPI spec](https://github.com/beeper/desktop-api-openapi)). Moving the
   existing provider onto that socket, in the `slack-socket` shape, gives the
   long tail near-real-time delivery without new protocol code.
2. **On Apple, Beeper open-sourced its iMessage engine under MIT.**
   `beeper/platform-imessage` reads `chat.db` and drives Messages.app through
   AppleScript and the Accessibility API. It sends replies, tapbacks, edits,
   undo-send, read state and typing **with SIP enabled**, which BlueBubbles
   cannot do without disabling SIP. It is a Swift package, so it drops
   straight into our macOS app.

---

## 1. Licensing primer for a public Go repository

| License | Embed in the Go server or our apps | Run as a separate process |
| --- | --- | --- |
| MIT, Apache-2.0, BSD | Yes; keep notices | Yes |
| MPL-2.0 (mautrix-go) | Yes; changes to MPL files stay MPL (file-level copyleft) | Yes |
| GPL-3.0 (imessage-exporter, crabstep, sms-ie, purple-teams, kalium, session-desktop) | Only if the combined work is GPL-3.0 | Yes. Talking over a pipe, CLI or HTTP is generally "aggregation"; ship the sidecar's source |
| AGPL-3.0 (mautrix-gmessages/libgm, mautrix-googlechat, mautrix-imessage, Threema Desktop and libthreema) | Makes the server AGPL, including the duty to offer source to network users | Yes, as a separate binary over HTTP or IPC; the sidecar itself must offer its source (fine if it is open) |
| SSPL (pypush, rustpush) | No | Avoid; SSPL's service clause is designed to reach the whole service stack |

mautrix-gmessages grants AGPL exceptions **only to Beeper and Element**
([LICENSE.exceptions](https://github.com/mautrix/gmessages/blob/main/LICENSE.exceptions)),
so substrate gets plain AGPL.

---

## 2. iMessage and SMS on Apple

### 2.1 The Messages database (`~/Library/Messages/chat.db`)

**Files.** `chat.db` plus `chat.db-wal` and `chat.db-shm`. Attachments live
under `~/Library/Messages/Attachments/`. The schema below comes from the
macOS 26 (Tahoe) dump in platform-imessage's fixtures
([schema-tahoe.sql](https://github.com/beeper/platform-imessage/blob/main/fixtures/schema-tahoe.sql)).

**Tables that matter.**

| Table | Use |
| --- | --- |
| `message` | one row per message, tapback, poll vote, group event |
| `chat` | conversation: `guid`, `chat_identifier`, `service_name`, `display_name`, `group_id`, `last_read_message_timestamp`, `is_archived` |
| `handle` | remote party: `id` (phone/email), `service`, `person_centric_id` |
| `chat_message_join`, `chat_handle_join`, `message_attachment_join` | joins |
| `attachment` | `filename` (path, `~` prefixed), `mime_type`, `uti`, `transfer_name`, `total_bytes`, `transfer_state`, `is_sticker` |
| `deleted_messages` | `guid` of every deleted message row, filled by an `AFTER DELETE` trigger. A reliable "deleted on this Mac" feed |
| `chat_recoverable_message_join`, `recoverable_message_part` | Recently Deleted |

**`message` columns that matter.**

- Identity and routing: `ROWID`, `guid`, `handle_id` (→ `handle.ROWID`),
  `service` (`iMessage`, `SMS`, `RCS`), `account`, `destination_caller_id`,
  `is_from_me`.
- Timestamps: `date`, `date_read`, `date_delivered`, `date_edited`,
  `date_retracted`. All are nanoseconds since 2001-01-01 UTC; old rows use
  seconds. Unix seconds = `date/1e9 + 978307200`
  ([imessage-database dates.rs](https://github.com/ReagentX/imessage-exporter/blob/develop/imessage-database/src/util/dates.rs)).
- Body: `text`, often NULL on modern macOS, and `attributedBody` (see below).
- Reactions: `associated_message_guid` (`p:<part>/<guid>` or `bp:<guid>`
  targets one part of a message), `associated_message_type` and
  `associated_message_emoji`. Types 2000–2005 add the classic six tapbacks,
  2006 adds a custom emoji, 1000/2007 a sticker, and 3000–3007 remove the
  same. 4000 is a poll vote
  ([message.rs](https://github.com/ReagentX/imessage-exporter/blob/develop/imessage-database/src/tables/messages/message.rs)).
- Replies: `thread_originator_guid`, `thread_originator_part`,
  `reply_to_guid`.
- Edits and unsends: `date_edited`, `date_retracted` and
  `message_summary_info`, a plist holding each part's edit history and
  unsend state ([edited.rs](https://github.com/ReagentX/imessage-exporter/blob/develop/imessage-database/src/message_types/edited.rs)).
- Rich content: `balloon_bundle_id` (URL previews, Polls, Apple Pay,
  Check In…), `payload_data`, `expressive_send_style_id`.
- Group events: `item_type`, `group_action_type`, `group_title`,
  `other_handle`.
- Send Later: `schedule_type`, `schedule_state`.

The triggers call SQL functions that only Messages registers
(`verify_chat`, `delete_attachment_path`…). **Open the database read-only and
never write to it.**

**Decoding `attributedBody`.** It is an `NSAttributedString` archived with
`NSArchiver`, the NeXTSTEP "typedstream" format. Its attributes carry
`__kIMFileTransferGUIDAttributeName` (attachment order),
`__kIMMessagePartAttributeName` (the part index that tapbacks and replies
target), links and mentions. Three ways to decode it:

1. On a Mac, Foundation's `NSUnarchiver`. mautrix-imessage does this over
   cgo ([meowAttributedString.m](https://github.com/mautrix/imessage/blob/master/imessage/mac/meowAttributedString.m)).
   In a Swift app it is one line and has no license cost. **Use this.**
2. [crabstep](https://github.com/ReagentX/crabstep): Rust, **GPL-3.0**,
   0.7.0 (2026-09-16), the decoder used by imessage-exporter.
3. [python-typedstream](https://github.com/dgelessus/python-typedstream):
   Python, **GPL-3.0**, last commit 2023-06.

**Permissions (TCC).**

- **Full Disk Access** to read `chat.db`. platform-imessage requires FDA on
  macOS 27 and later; on macOS 26 and earlier it can ask for access to the
  Messages folder instead
  ([MacPermissions.swift](https://github.com/beeper/platform-imessage/blob/main/src/IMessage/Sources/IMessage/MacPermissions.swift),
  [README](https://github.com/beeper/platform-imessage)).
- **Automation → Messages** (Apple Events) to send through AppleScript.
- **Accessibility** for UI automation (replies, tapbacks, edits).
- **Contacts** for names.

A Mac App Store build cannot hold these, so the connector must ship in a
Developer-ID or dev-signed app, which ours is ⚠️ (sandbox incompatibility
inferred, not tested).

**History depth.** Whatever this Mac's database holds. With *Messages in
iCloud* on, that is the account's synced history ⚠️ (completeness depends on
iCloud sync and storage optimisation). SMS and RCS appear only if the iPhone
forwards them to this Mac (Settings → Messages → Text Message Forwarding);
imsg notes that macOS can then send RCS through the SMS relay account
([imsg send docs](https://github.com/openclaw/imsg/blob/main/docs/send.md)).

**Tailing for near-real-time.**

- Watch `chat.db`, `chat.db-wal`, `chat.db-shm` and the directory with kqueue
  or FSEvents, debounce 250–500 ms, then query `WHERE ROWID > :cursor`. Keep
  a polling fallback, because macOS drops events and rotates the WAL
  ([imsg watch docs](https://github.com/openclaw/imsg/blob/main/docs/watch.md)).
  mautrix-imessage does the same with fsnotify on the directory
  ([messages.go](https://github.com/mautrix/imessage/blob/master/imessage/mac/messages.go)).
- **Catch-up after downtime.** Persist the ROWID cursor; inserts replay
  exactly. Edits, unsends and reads *mutate older rows* and are not replayed
  by a ROWID cursor. Rescan by `date_edited`, `date_retracted` and
  `date_read` greater than the last scan, and read `deleted_messages` by its
  own ROWID. A ROWID cursor belongs to one database generation; discard it
  if `chat.db` is replaced or restored.

```sql
-- inserts since the cursor: messages, tapbacks, votes
SELECT m.ROWID, m.guid, m.text, m.attributedBody, m.service, m.is_from_me,
       m.date, m.associated_message_guid, m.associated_message_type,
       m.associated_message_emoji, m.thread_originator_guid,
       h.id AS sender, c.guid AS chat_guid
FROM message m
JOIN chat_message_join cmj ON cmj.message_id = m.ROWID
JOIN chat c ON c.ROWID = cmj.chat_id
LEFT JOIN handle h ON h.ROWID = m.handle_id
WHERE m.ROWID > :cursor ORDER BY m.ROWID;

-- mutations since the last scan
SELECT guid, date_edited, date_retracted, date_read, message_summary_info
FROM message
WHERE date_edited > :t OR date_retracted > :t OR date_read > :t;

-- local deletions
SELECT ROWID, guid FROM deleted_messages WHERE ROWID > :del_cursor;
```

### 2.2 Sending without private APIs: AppleScript and Shortcuts

**AppleScript.** Messages' dictionary sends text or a file to `chat id
"<guid>"` or to a buddy or participant of a service
([mautrix-imessage send.go](https://github.com/mautrix/imessage/blob/master/imessage/mac/send.go)):

```applescript
tell application "Messages" to send "on my way" to chat id "iMessage;-;+15555550123"
tell application "Messages" to send (POSIX file "/Users/me/Library/Messages/Attachments/x/photo.jpg") to chat id "iMessage;-;+15555550123"
```

That is all it can do. No replies, tapbacks, edits, unsends, read receipts or
typing; the "mac" connector's roadmap marks each of those 🛑
([ROADMAP](https://github.com/mautrix/imessage/blob/master/ROADMAP.md)).

Gotchas from imsg's send docs
([send.md](https://github.com/openclaw/imsg/blob/main/docs/send.md)):

- Stage files under `~/Library/Messages/Attachments/`. Sending from Desktop
  or Downloads hits sandbox failures.
- The chat must be in Messages' live chat list, or AppleScript returns -1728.
- On macOS 26 AppleScript can report success while writing an empty "ghost"
  SMS row that is joined to no chat. Verify by reading the outgoing row back
  (imsg waits up to 8 s), and never blindly retry an uncertain send.
- SMS needs Text Message Forwarding. With several numbers on one Apple ID,
  AppleScript cannot pick the outgoing number.

**Shortcuts.** The "Send Message" action sends text and files, and `shortcuts
run` drives it from a script. It offers nothing AppleScript lacks for replies
or reactions ⚠️.

### 2.3 Options

#### A. beeper/platform-imessage — recommended

- **What:** Swift package (`IMessage` product) plus `imessage-cli` and N-API
  bindings for Node/Electron. **MIT.** Release v0.25.8 on 2026-11-09, 71★,
  maintained by Beeper (Automattic). It is the engine behind Beeper Desktop's
  iMessage on macOS ([README](https://github.com/beeper/platform-imessage)).
  The `TextsHQ/platform-sdk` shapes in its API trace back to Texts.com,
  which Automattic folded into Beeper.
- **Form:** (D) embedded in our macOS app via SwiftPM. Alternatively (S): the
  CLI on an always-on Mac.
- **Authentication:** the Apple ID signed in to Messages.app. Nothing to
  store. The grants are Accessibility, Contacts, Messages data access (FDA on
  macOS 27+) and Automation.
- **History:** `getThreads(folderName:pagination:)` and
  `getMessages(threadID:pagination:)` over `chat.db`. Beeper's own policy is
  25 recent chats growing to about one year
  ([Beeper support matrix](https://github.com/beeper/beeper-support-matrix)).
  That is Beeper's choice, not the library's limit.
- **Real-time:** an EventWatcher on FSEvents plus database polling, through
  `subscribeToEvents(_:)` and `startEventWatchingFromCurrentState()`.
  Catch-up is our cursor over `chat.db`, as in 2.1. Real-time sync of
  delete-for-me is a known gap; undo-send already syncs
  ([todos.md](https://github.com/beeper/platform-imessage/blob/main/todos.md)).
- **Contacts:** Contacts.framework.
- **Actions (README feature table):**
  - Send text and attachments; replies through `quotedMessageID`.
  - Add and remove tapbacks; any emoji on Sequoia and later, per the
    [support matrix](https://github.com/beeper/beeper-support-matrix).
  - Edit; undo send.
  - Mark read and unread; send typing.
  - Create 1:1 and group chats; "notify anyway"; search.
  - Planned: group management and rich sends. Out of scope: multiple
    accounts and SIP-off features.
- **Interface** (public methods of
  [PlatformAPI.swift](https://github.com/beeper/platform-imessage/blob/main/src/IMessage/Sources/IMessage/PlatformAPI.swift);
  the initializer and the activity-type string are ⚠️ not checked):

```swift
import IMessage   // .product(name: "IMessage", package: "platform-imessage")

let api = PlatformAPI(/* … */)
api.subscribeToEvents { events in forward(events) }        // thread/message upserts
try await api.startEventWatchingFromCurrentState()
let page = try await api.getMessages(threadID: tid, pagination: nil)
_ = try await api.sendMessage(threadID: tid, text: "on my way", filePath: nil, quotedMessageID: replyTo)
try await api.addReaction(threadID: tid, messageID: mid, reactionKey: "heart")
try await api.editMessage(threadID: tid, messageID: mid, content: "fixed")
try await api.deleteMessage(threadID: tid, messageID: mid)  // undo send
try await api.sendReadReceipt(threadID: tid)
try await api.sendActivityIndicator(type: "typing", threadID: tid)
```

- **Risks:**
  - It is UI automation. It needs a logged-in GUI session and an open
    Messages.app; during an action it hides the Messages window behind
    another window (`EclipsingWindowCoordinator`).
  - Two processes driving Messages at once collide; cross-process
    coordination is still a TODO.
  - macOS releases move the UI; Beeper's own parity was "~95% on Tahoe".
  - No ban vector is known, because nothing speaks Apple's protocol.
  - It cannot run on a locked or headless Mac without a user session ⚠️
    (behaviour with the screen locked was not tested).
- **Recommendation:** the primary iMessage connector. Run it in our macOS
  app, which pushes records to substrate the way the `apple` bundle does, on
  the user's always-on Mac or a dedicated Mac mini signed in to their
  Apple ID.

#### B. openclaw/imsg — the simplest sidecar

- **What:** Swift CLI and library (`IMsgCore`), **MIT**. v0.21.0 on
  2026-11-05, about 1.4k★, from the OpenClaw project (moved from
  `steipete/imsg`). macOS 14+; Linux builds read a copied `chat.db` only
  ([README](https://github.com/openclaw/imsg)).
- **Form:** (S) `imsg rpc`, a long-running JSON-RPC 2.0 process on stdio, on
  the Mac. Or (D) link `IMsgCore`.
- **Authentication:** as in A; FDA plus Automation, and Contacts optional.
- **History:**
  - `messages.history {chat_id, limit, start, end, attachments}`
  - `messages.after {since_rowid, limit ≤ 500}`
  - `messages.search`
- **Real-time:** `watch.subscribe {since_rowid, include_reactions,
  attachments, debounce_ms, buffer_limit}` sends `message` notifications.
  Edits and unsends arrive as `type: "edited" | "unsent"` updates that reuse
  the original id; a ROWID resume does **not** replay them. A full buffer
  ends the subscription with `watch.overflow` carrying `resume_after_rowid`
  ([watch.md](https://github.com/openclaw/imsg/blob/main/docs/watch.md),
  [rpc.md](https://github.com/openclaw/imsg/blob/main/docs/rpc.md)).
- **Actions without SIP changes:**
  - Text and file sends through AppleScript, with read-back verification and
    the `not_started` / `may_have_completed` / `still_in_flight` outcomes.
  - The six classic tapbacks through UI automation. These hit the last or
    selected message; imsg **cannot target a specific message** without the
    bridge.
- **Actions with SIP disabled** (a helper injected with
  `DYLD_INSERT_LIBRARIES`): read receipts, typing, threaded replies,
  stickers, polls, `send.tracked`, chat creation. imsg itself warns that
  macOS 26 library validation and new `imagent` entitlement checks may block
  this ([advanced-imcore.md](https://github.com/openclaw/imsg/blob/main/docs/advanced-imcore.md)).
- **Interface:**

```text
→ {"jsonrpc":"2.0","id":1,"method":"watch.subscribe","params":{"since_rowid":9000,"include_reactions":true,"attachments":true}}
← {"jsonrpc":"2.0","method":"message","params":{ … message object … }}     (envelope fields ⚠️)
→ {"jsonrpc":"2.0","id":2,"method":"send","params":{"chat_guid":"iMessage;-;+15555550123","text":"on my way"}}
← {"jsonrpc":"2.0","id":2,"result":{"ok":true,"id":1979,"guid":"8DF…"}}
```

  A message object has `id`, `chat_id`, `chat_guid`, `guid`, `sender`,
  `sender_name`, `is_from_me`, `text` (recovered from `attributedBody`),
  `created_at`, `attachments[]`, `reply_to_guid`, `thread_originator_guid`,
  `edited_at`, `unsent_at`, `is_read`, `date_read`, plus `poll` and the
  reaction fields `reaction_type`, `reaction_emoji`, `is_reaction_add`,
  `reacted_to_guid` ([json.md](https://github.com/openclaw/imsg/blob/main/docs/json.md)).
- **Risks:** the AppleScript limits in 2.2; the SIP-off extras are fragile.
- **Recommendation:** use it when we want a reader plus a plain sender and
  no UI-driven features, or as the fastest prototype. A sidecar can pipe its
  NDJSON into a substrate webhook trigger.

#### C. BlueBubbles Server (+ Private API helper)

- **What:** an Electron/TypeScript app, **Apache-2.0**. v1.9.12 on
  2026-10-27, 1.1k★ ([repo](https://github.com/BlueBubblesApp/bluebubbles-server)).
  The helper, [bluebubbles-helper](https://github.com/BlueBubblesApp/bluebubbles-helper),
  is Objective-C and Apache-2.0; it was rewritten in May 2026. Releases
  stopped between May 2025 and October 2026; v1.9.10+ are notarized again and
  add macOS Tahoe fixes.
- **Form:** (S) an app on an always-on Mac serving REST, Socket.IO and
  outgoing webhooks. Cloudflare, ngrok or zrok tunnels and Firebase are
  optional.
- **Authentication:** a server password sent as a `?password=`, `?guid=` or
  `?token=` query parameter
  ([authMiddleware.ts](https://github.com/BlueBubblesApp/bluebubbles-server/blob/master/packages/server/src/server/api/http/api/v1/middleware/authMiddleware.ts)).
- **History:**
  - `POST /api/v1/message/query`, body `{chatGuid, with:[chat, attachment,
    attributedBody, messageSummaryInfo, payloadData], where, sort, after,
    before (ms), limit, offset}`.
  - `GET /api/v1/chat/:guid/message`, `POST /api/v1/chat/query`.
  - `GET /api/v1/attachment/:guid/download`.
- **Real-time:**
  - Socket.IO events: `new-message`, `updated-message` (delivery, read, edit,
    unsend), `typing-indicator`, `chat-read-status-changed`,
    `group-name-change`, `participant-added` / `-removed` / `-left`,
    `group-icon-changed` ([events.ts](https://github.com/BlueBubblesApp/bluebubbles-server/blob/master/packages/server/src/server/events.ts)).
  - Webhooks registered at `/api/v1/webhook` POST `{type, data}`. The
    dispatcher makes one attempt with no retry
    ([webhookService](https://github.com/BlueBubblesApp/bluebubbles-server/blob/master/packages/server/src/server/services/webhookService/index.ts)),
    so catch up with `message/query` `after` the last date seen.
- **Contacts:** `/api/v1/contact` and `/api/v1/handle/query`.
- **Actions:**
  - `POST /message/text` `{chatGuid, tempGuid, message, method:
    "apple-script"|"private-api", selectedMessageGuid, partIndex, effectId,
    subject}`.
  - `/message/attachment`, `/message/multipart`.
  - `/message/react` `{chatGuid, selectedMessageGuid, reaction, partIndex}`.
  - `/message/:guid/edit` `{editedMessage, backwardsCompatibilityMessage,
    partIndex}`, `/message/:guid/unsend`.
  - `/chat/:guid/read`, `/chat/:guid/typing`, `/chat/new`, participant and
    icon routes.

  Replies, reactions, edits, unsends, read receipts and typing all need the
  Private API (see the mautrix-imessage roadmap and platform-imessage's
  comparison table).
- **Private API requirements:** SIP disabled (`csrutil disable` in Recovery;
  on Apple Silicon hold Left Shift at Continue on later macOS) and library
  validation disabled (`sudo defaults write
  /Library/Preferences/com.apple.security.libraryvalidation.plist
  DisableLibraryValidation -bool true`). With SIP off, an Apple Silicon Mac
  can no longer run iOS apps ([installation guide](https://docs.bluebubbles.app/private-api/installation)).
- **Risks:** a SIP-off security posture; injection breaks on OS updates;
  releases have been irregular.
- **Recommendation:** support it as an alternative backend for users who
  already run it. Its webhooks map cleanly onto a substrate trigger, and
  sandboxed functions can call its REST API (both are plain HTTP). It is not
  our default, because platform-imessage gets the same features with SIP on.

#### D. mautrix-imessage (legacy)

[mautrix/imessage](https://github.com/mautrix/imessage): Go, **AGPL-3.0**,
460★, no releases. The last commit (2026-05-14) only updated mautrix-go. It is
not bridgev2. Its backends:

- `mac`: `chat.db` plus AppleScript.
- `mac-nosip`: Barcelona, which the roadmap says is no longer maintained.
- `bluebubbles`.

The Android SMS backend is deprecated in favour of gmessages
([ROADMAP](https://github.com/mautrix/imessage/blob/master/ROADMAP.md)). Use
it as reference code (the queries, the fsnotify loop, the `NSUnarchiver`
cgo), not as a base.

#### E. imessage-exporter / imessage-database

[ReagentX/imessage-exporter](https://github.com/ReagentX/imessage-exporter):
Rust, **GPL-3.0-or-later**, 5.0.1 (2026-10-29), 5.7k★, maintained by
Christopher Sardegna. Its README claims support for every iMessage feature
"as of macOS Golden Gate 27.2 / iOS 27.2" (naming as printed). The library
parses:

- tapbacks, including emoji and sticker tapbacks;
- edits and unsends with their history;
- polls, app balloons, Digital Touch and handwriting;
- Recently Deleted.

It reads a Mac's `chat.db` **and an iPhone backup, including an encrypted
one** (`--platform iOS`, `--cleartext-password`;
[exporter README](https://github.com/ReagentX/imessage-exporter/blob/develop/imessage-exporter/README.md)).

Use: one-shot historical import, including from an iPhone backup for a user
with no Mac. Run it as a separate process because of the GPL.

#### F. iMessage MCP servers

There are dozens, for example
[adelaidasofia/imessage-mcp](https://github.com/adelaidasofia/imessage-mcp)
and cyberpapiii/imessage-max. All of them read `chat.db` and send through
AppleScript; they are small and add nothing over imsg or platform-imessage.
Not worth a dependency.

#### G. Protocol reimplementations without a Mac — avoid

- [beeper/imessage](https://github.com/beeper/imessage): archived 2024-04.
- [pypush](https://github.com/JJTech0130/pypush): **SSPL**. Its v2 rewrite
  "temporarily removed" iMessage, and the README says it was "purchased by
  Beeper".
- [rustpush / OpenBubbles](https://github.com/OpenBubbles/rustpush): **SSPL**;
  needs hardware identifiers from a real Mac.

Apple shut down Beeper Mini, built on this approach, in December 2023. The
SSPL license and the risk to the user's Apple ID rule all three out.

#### H. Beeper's own approach

Beeper's support matrix: iMessage is **on-device only**, in "Beeper Desktop
for Mac only", on macOS Big Sur or later. "Beeper uses the Messages app on
that Mac, which must stay open", and the connection does not sync to the
user's other Beeper devices ([beeper-support-matrix](https://github.com/beeper/beeper-support-matrix)).
In other words: platform-imessage running inside the desktop app. Beeper
Cloud no longer offers iMessage.

### 2.4 iOS: what is feasible

- **Reading Messages: impossible.** There is no API, no Full Disk Access and
  no background database access.
- **IdentityLookup message filters** see only SMS/MMS **from unknown
  senders**; they never see iMessage or contacts. The extension cannot reach
  the network itself or write to the app's shared container
  ([Apple: SMS and MMS message filtering](https://developer.apple.com/documentation/identitylookup/sms-and-mms-message-filtering)).
  Useless for mirroring.
- **Shortcuts personal automation "Message"** (iOS 17+): Sender = Any Sender,
  or "Message Contains" a phrase, set to *Run Immediately*. Its input is the
  message and sender, which "Get Contents of URL" can POST to a substrate
  webhook. That gives a **lossy inbound tap**:
  - text and sender only, with no attachments ⚠️;
  - no history, edits or reactions;
  - fires only while the phone is on;
  - the catch-all trick (a single space in "Message Contains") misses
    messages without spaces
    ([tinyapps write-up, 2026-04](https://tinyapps.org/blog/ios-autoreply-text.html),
    [Apple Shortcuts guide](https://support.apple.com/guide/shortcuts/apdd711f9dff/ios)).
- **Sending:** a Shortcuts automation can send without interaction ⚠️ (per
  the same write-up). But the server has no way to *start* a shortcut on the
  phone, so outbound sends would need a user-visible step.
- **Verdict: not feasible as a connector.** iPhone users get iMessage, SMS
  and RCS through a Mac running A or B. Optionally ship the Shortcuts
  inbound tap as an opt-in "notify me" feature, not as a mirror.

### 2.5 Recommendation (Apple)

1. **Build the on-device macOS connector on platform-imessage (MIT).** It
   reads `chat.db` with a ROWID cursor plus a mutation rescan, pushes to
   substrate's API, and executes sends, replies, tapbacks, edits, unsends,
   read state and typing that substrate queues for it.
2. **Fallback: an imsg `rpc` sidecar** for read and text/file sends when UI
   automation is unwanted.
3. **Accept BlueBubbles** as a backend for users who already run it, through
   webhooks and REST.
4. **imessage-exporter** (separate process) for one-shot imports, including
   from iPhone backups.

---

## 3. Android SMS, MMS and RCS

### 3.1 Google Messages for Web through libgm (mautrix-gmessages)

- **What:** [mautrix/gmessages](https://github.com/mautrix/gmessages), module
  `go.mau.fi/mautrix-gmessages`, with the client in `pkg/libgm`. Go,
  **AGPL-3.0** (exceptions for Beeper and Element only). Calendar-versioned
  v26.10 (release tag v0.2610.0, 2026-10-16); last commit 2026-11-07; 199★;
  maintained by Tulir Asokan, who maintains mautrix and works for Beeper.
- **Form:** (S) a Go sidecar. Embedding libgm would put the server under the
  AGPL.
- **Authentication as the user.** QR pairing is gone: the bridge disabled it
  in v26.05 "as Google has discontinued it"
  ([CHANGELOG](https://github.com/mautrix/gmessages/blob/main/CHANGELOG.md)),
  and Google is retiring it from Messages for Web through 2026
  ([9to5Google, 2026-03-23](https://9to5google.com/2026/03/23/google-messages-web-qr-removal/),
  [Android Authority](https://www.androidauthority.com/google-messages-qr-web-3692106/)).
  What remains is **Google-account ("Gaia") pairing**
  ([docs](https://docs.mau.fi/bridges/go/gmessages/authentication.html)):
  1. Sign in to `accounts.google.com/AccountChooser?continue=https://messages.google.com/web/config`
     in a *private window*.
  2. Export the cookies `SID`, `HSID`, `SSID`, `OSID`, `APISID`, `SAPISID`,
     and sometimes `__Secure-1PSIDTS`. Browsers with Device Bound Session
     Credentials break this; Firefox works, and mautrix-manager automates it.
  3. `DoGaiaPairing` shows an emoji that the user taps in Google Messages on
     the phone.

  Google Fi's "sync to Google Account" mode is unsupported.
- **Persisted state:** `AuthData` holds the AES-CTR request keys, a refresh
  JWK, the browser and mobile device ids, the Tachyon auth token with its
  expiry and TTL, session and pairing ids, and the Google cookies
  ([client.go](https://github.com/mautrix/gmessages/blob/main/pkg/libgm/client.go)).
  - The token refreshes itself (`AuthTokenRefreshed` event).
  - Cookies rotate from responses.
  - A remote logout arrives as `GaiaLoggedOut`.
  - v26.10 can re-pair with stored cookies by one emoji tap.
  - The cookies are full Google-account session cookies: **treat them as
    the most sensitive secret in the system.**
- **History:** `ListConversations` (folder, count, cursor) and
  `FetchMessages(ctx, convID, count, cursor)` paginate as far back as the
  *phone* holds. The bridge defaults to 25 chats × 50 messages, but that is
  configuration. Media comes through `DownloadMedia` and
  `GetFullSizeImage`.
- **Real-time:** a long-poll receive channel relayed through Google's
  servers to the phone, with acknowledgements. Events
  ([event_handler.go](https://github.com/mautrix/gmessages/blob/main/pkg/libgm/event_handler.go),
  [events](https://github.com/mautrix/gmessages/tree/main/pkg/libgm/events)):
  - `*libgm.WrappedMessage{*gmproto.Message, IsOld}`, `*gmproto.Conversation`
  - typing data, `UserAlertEvent` (phone offline, battery, data), settings
  - `events.ClientReady`, `PhoneNotResponding` / `PhoneRespondingAgain`
  - `ListenTemporaryError` / `ListenRecovered`, `NoDataReceived`,
    `AccountChange`

  Catch-up: an event left unacknowledged is replayed (v26.10 fixed replays
  after logout), and chat sync resynchronises conversations when the phone
  returns ("don't drop the missed-event sync when the phone comes back",
  2026-11-07). **The phone must be online**: "As all messages are proxied
  through the app, your phone must be connected to the internet."
- **Contacts:** `ListContacts`, `ListTopContacts`, `GetContactThumbnail`,
  `GetParticipantThumbnail`, and participants inside each conversation.
- **Actions** ([methods.go](https://github.com/mautrix/gmessages/blob/main/pkg/libgm/methods.go),
  [ROADMAP](https://github.com/mautrix/gmessages/blob/main/ROADMAP.md)):
  - `SendMessage`: text, media, and replies on RCS. Reply metadata is
    dropped when the target went over SMS.
  - `SendReaction`, `SetTyping` and read receipts: RCS only.
  - `MarkRead`; `UpdateConversation` (archive, mute, block);
    `GetOrCreateConversation`; group creation.
  - `DeleteMessage` deletes on our own device only; there is **no RCS
    unsend**. **No edit.**
- **Interface:**

```go
auth := libgm.NewAuthData()
auth.SetCookies(map[string]string{"SID": sid, "HSID": hsid, "SSID": ssid, "OSID": osid, "APISID": apisid, "SAPISID": sapisid})
cli := libgm.NewClient(auth, nil, logger, exhttp.ClientSettings{})
cli.SetEventHandler(func(evt any) {
    switch e := evt.(type) {
    case *libgm.WrappedMessage:            // e.Message (*gmproto.Message), e.IsOld
    case *gmproto.Conversation:
    case *events.PhoneNotResponding, *events.PhoneRespondingAgain:
    case *events.GaiaLoggedOut:            // ask the user to re-pair
    }
})
err := cli.DoGaiaPairing(ctx, func(emoji string) { showToUser(emoji) })
saveJSON(cli.AuthData)                     // AuthData is JSON-marshalable
err = cli.Connect(ctx)
msgs, _ := cli.FetchMessages(ctx, convID, 50, nil)   // cursor-paged backfill
_ = cli.MarkRead(ctx, convID, msgID)
// SendMessage / SendReaction take gmproto request structs (field names not re-checked ⚠️)
```

- **Risks:** it emulates the web client, so it is unofficial; Google already
  killed QR pairing once and can change Gaia pairing; handling Google cookies
  is a security liability; carrier quirks.
- **Recommendation:** the primary path for Android SMS **and RCS**. Run it as
  an open-source Go sidecar that delivers in batches to a webhook trigger
  (the `slack-socket` shape) and exposes a small send API that functions
  call.

### 3.2 Reading and sending from our own Android app

- **Provider:** `Telephony.Sms` (`content://sms`: `_id`, `thread_id`,
  `address`, `date`, `date_sent`, `read`, `seen`, `type` where 1 is inbox and
  2 is sent, `status`, `body`, `sub_id`), `Telephony.Mms` (`content://mms`,
  `…/part`, `…/{id}/addr`), and `content://mms-sms/conversations`.
- **Permissions:** `READ_SMS`, `RECEIVE_SMS`, `RECEIVE_MMS`,
  `RECEIVE_WAP_PUSH`, `SEND_SMS`, `READ_CONTACTS`; `READ_PHONE_STATE` to pick
  a SIM.
- **Google Play policy.** SMS permissions are for default SMS, Phone or
  Assistant handlers, plus declared exceptions. One exception fits us:
  **"Cross-device synchronization or transfer of SMS or calls"** (`READ_SMS`,
  `RECEIVE_MMS`, `RECEIVE_SMS`, `RECEIVE_WAP_PUSH`, `SEND_SMS`), declared in
  the Play Console ([Play policy](https://support.google.com/googleplay/android-developer/answer/10208820)).
  A build distributed outside Play is not bound by this.
- **Do not become the default SMS app.** That would displace Google Messages
  and with it RCS.
- **Sending as a non-default app:** `SmsManager.sendTextMessage`. Since API 19
  the system writes those sent SMS into the provider; it does not do this for
  MMS. A non-default app cannot write to the provider, so it cannot mark
  messages read
  ([Android Developers Blog: SMS apps on KitKat](https://android-developers.googleblog.com/2013/10/getting-your-sms-apps-ready-for-kitkat.html)).
  Sending MMS means building PDUs for `sendMultimediaMessage`.
- **RCS is not readable** by third-party apps:
  - Google Messages owns RCS, and its RCS APIs are allowlisted. Android RCS
    Archival is for approved enterprise archiving apps
    ([Google Workspace admin](https://support.google.com/work/android/answer/13761869)).
  - SMS Backup & Restore's vendor reports that **E2EE RCS rows are marked
    "Restricted"**, readable only by the default SMS app, and that a
    May/June 2026 Google Messages update stopped writing *outgoing* RCS to
    the provider ⚠️ (single vendor source:
    [SyncTech FAQ](https://www.synctech.com.au/faqs/why-are-some-messages-missing-from-the-backup/)).
- **Real-time:** the `SMS_RECEIVED` broadcast, plus a `ContentObserver` on
  `content://sms` and `content://mms-sms/` for both directions. Catch up by
  an `_id` or `date` cursor on resume. Background limits call for a
  foreground service or WorkManager.
- **Reuse:**
  - [SMS Gateway for Android](https://github.com/capcom6/android-sms-gateway):
    Kotlin, **Apache-2.0**, v1.78.2 (2026-11-03), about 6k★. It provides a
    REST API on the phone (local mode) or through a cloud or private server,
    webhooks for SMS, data SMS and MMS, reading earlier messages, MMS send
    with attachments, multi-SIM and E2E encryption. We can run it beside our
    app, or lift code into our shell (Apache-2.0 permits that).
  - [SMS Import/Export](https://github.com/tmo1/sms-ie): **GPL-3.0**, v2.12.0
    (2026-10-09). A one-shot NDJSON export of SMS, MMS and the call log for
    history import.
- **Recommendation:** an SMS/MMS-only path, for users without RCS or who
  refuse to hand over Google cookies. Pair it with libgm for RCS.

### 3.3 Notification listener (any Android app)

`NotificationListenerService` with user-granted Notification Access sees every
notification. A chat app's `MessagingStyle` carries sender `Person`s and the
recent lines, and many apps attach a `RemoteInput` "Reply" and a "Mark as
read" action. That gives **real-time inbound plus quick reply for any app**:
RCS, Viber, WeChat, LINE, KakaoTalk.

Limits:

- No history, and only what the app notifies (muted chats and the
  foreground conversation are silent).
- Text is truncated and media is mostly absent.
- Android 15 hides "sensitive" notifications such as one-time codes from
  untrusted listeners; `RECEIVE_SENSITIVE_NOTIFICATIONS` is `role|signature`
  ([Android Authority](https://www.androidauthority.com/android-15-sensitive-notifications-3416414),
  [KDE bug 495146](https://bugs.kde.org/show_bug.cgi?id=495146)).

A precedent is [ximalu/wechat-matrix-bridge](https://github.com/ximalu/wechat-matrix-bridge)
(an Android notification listener bridging WeChat to Matrix). Use it as a
**degraded fallback** for networks with no other path. Whether each app
offers a reply action is ⚠️ per app.

---

## 4. Viber (explicitly requested)

### 4.1 The official REST bot API: a bot, not the user

- Base URL `https://chatapi.viber.com/pa/`, header `X-Viber-Auth-Token`.
  Endpoints include `set_webhook` (events `delivered`, `seen`, `failed`,
  `subscribed`, `unsubscribed`, `conversation_started`) and `send_message`.
- Only users who subscribed to the bot can be messaged; there is no API to
  list subscribers.
- **"since 5.02.24, Viber bots can only be created on commercial terms"**,
  applied for directly or through partners
  ([Viber REST API docs](https://developers.viber.com/docs/api/rest-bot-api/)).
  Viber's help centre is reported to charge **€100/month per bot**
  maintenance plus per-message fees for bot-initiated messages ⚠️ (help.viber.com
  blocked automated reads; figures from
  [respond.io](https://respond.io/blog/viber-bot-pricing) and SleekFlow,
  which also quotes €115).
- Phone-number messaging is a separate product (Viber Business Messages)
  sold through partners.
- **Verdict:** a paid bot account with its own identity. It cannot read the
  user's chats or send as the user. Out of scope.

### 4.2 Linked-device protocol: exists, but not in public

- Viber Desktop is a secondary device linked by scanning a QR code with the
  phone's Viber app.
- **No public library implements that protocol.** GitHub turns up:
  - [RegionallyFamous/mautrix-viber](https://github.com/RegionallyFamous/mautrix-viber):
    built on the *bot API*.
  - [rusty4444/viber-matrix-bridge](https://github.com/rusty4444/viber-matrix-bridge):
    Windows UI Automation of Viber Desktop, text only, marked "NO LONGER
    ACTIVELY MAINTAINED".
- **Beeper did it, closed source.** Viber launched in Beeper in late
  September or early October 2026 (iOS 4.3.11 on 2026-10-06). The guide:
  "Beeper connects to Viber by linking to the account on your phone, in much
  the same way Viber Desktop does". You scan a QR code from Viber
  (Android: More → QR icon; iOS: More → Settings → Account → Desktop and
  Tablets) and choose **Sync** for history; it runs in Beeper Cloud or
  on-device ([guide](https://help.beeper.com/en_US/chat-networks/viber-getting-started-guide)).
  - Supported (support matrix): reactions, read receipts, replies, files,
    history, deletions, chat creation, edits, typing, formatting,
    disappearing messages, polls; voice notes in only.
  - Limits: "Chat history backfill for Viber is limited"; no calls; no
    joining by invite link
    ([support matrix](https://github.com/beeper/beeper-support-matrix)).
  - No beeper or mautrix repository for Viber exists on GitHub (searched
    2026-11-09).
- Texts.com is folded into Beeper. Unipile does not list Viber
  ([Unipile](https://www.unipile.com/communication-api/messaging-api/)).

### 4.3 Reading Viber Desktop's local database

- **Locations:**
  - macOS: `~/Library/Application Support/ViberPC/<phone>/viber.db`
    ([viber-export-macos script](https://github.com/Anastasia-D-Kravchenko/viber-export-macos)).
  - Windows: `%APPDATA%\ViberPC\<phone>\viber.db` (forensic sources, e.g.
    [Forensic Focus thread](https://www.forensicfocus.com/forums/general/extract-data-from-viber-database/)).
  - Linux: `~/.ViberPC/<phone>/viber.db` ⚠️ (commonly cited, not verified).
- **Encryption:** current Viber Desktop for macOS encrypts `viber.db` with a
  **custom AES SQLite codec, not SQLCipher**, unlocked at run time with
  `PRAGMA hexkey` statements (a per-device key plus a cipher selector) whose
  key "is derived at runtime and never written to disk". The one public tool
  (MIT, 0★, single author, last commit 2026-08) works like this:
  1. Ad-hoc re-sign a copy of Viber.app with `get-task-allow`.
  2. Run it under `lldb` with breakpoints in Qt 6's QtSql.
  3. Capture the keys, then copy the tables out into a plaintext database
     ([viber-export-macos](https://github.com/Anastasia-D-Kravchenko/viber-export-macos)).

  A Ukrainian paper is reported to date the encryption to 2023 ⚠️. Older
  plaintext extractors such as [ViberExtractor](https://github.com/gsakkis/ViberExtractor)
  (2019) no longer work. **⚠️ Single-source; not verified on a live install.**
- **Schema essentials** (from that tool's export query and older forensic
  write-ups): `Events` (`EventID`, `TimeStamp` in ms, `ChatID`,
  `ContactID`, `Direction` where 0 is received), `Messages` (`EventID`,
  `Body`, `Type` where 0 is text and other values are media or sticker
  tokens) and `Contact` (`ContactID`, `Name`, `Number`). There are about 15
  tables linked through `Events`.

  ```sql
  SELECT e.TimeStamp, e.ChatID, e.Direction, c.Name, c.Number, m.Type, m.Body
  FROM Events e JOIN Messages m ON m.EventID = e.EventID
  LEFT JOIN Contact c ON c.ContactID = e.ContactID
  WHERE e.TimeStamp > :cursor ORDER BY e.TimeStamp;
  ```

- **Feasibility:** debugging Viber to lift a key on every launch is brittle
  (it depends on Qt symbol names), needs developer tooling, and edges toward
  circumventing a technical measure. Not a product path.

### 4.4 Sending by UI automation

Viber Desktop is a Qt 6/QML app. Its accessibility tree is thin: rusty4444's
bridge hit zero-size rectangles, misdirected clicks, focus stealing and
breakage on every auto-update. `viber://chat?number=…` deep links open a chat
⚠️ (desktop behaviour with prefilled text not verified), but sending still
needs a synthetic keystroke. Fragile and not recommended.

### 4.5 Honest verdict on Viber

- **Building our own "act as the user" Viber connector is not feasible.**
  There is no open protocol implementation, the bot API is a different and
  paid identity, the desktop database is encrypted with a runtime-only key,
  and UI automation is unreliable.
- **The realistic route is Beeper.** The user links Viber in Beeper (on-device
  keeps it local) and substrate reads and acts through the Beeper Desktop API
  already integrated (see 14.1): list and send messages, reply, edit, react.
  The Desktop API's v0.2.0 spec has **no delete/unsend, mark-read or typing
  endpoints** ([openapi.json](https://github.com/beeper/desktop-api-openapi)).
- A degraded alternative is the Android notification listener (3.3):
  inbound in real time and reply only, where Viber exposes a reply action ⚠️.
- Revisit if Beeper open-sources its Viber connector, as it did with LINE.

---

## 5. LINE

- **Protocol landscape:**
  - LINE's official Messaging API is for Official Accounts (bots) only.
  - User clients speak Thrift to LINE's gateways. 1:1 and group chats are
    end-to-end encrypted with Letter Sealing.
  - Each device type has one slot. The Chrome extension is one slot; LINE
    also offers "sub devices", which need an email and password on the
    account and **Allow login** enabled on the main phone
    ([Beeper LINE guide](https://help.beeper.com/en_US/chat-networks/line-getting-started-guide)).

### A. beeper/line — recommended

- **What:** [beeper/line](https://github.com/beeper/line), module
  `github.com/beeper/line`. Go, **MIT**, bridgev2. v1.2.0 (2026-05-17), last
  commit 2026-11-06, 63★, maintained by Beeper. It powers Beeper Cloud's LINE.
- **Form:** (S) a Go sidecar. `pkg/line` (client), `pkg/e2ee` and `pkg/ltsm`
  can also be used without the Matrix parts; MIT allows that.
- **Authentication:** email and password, then a PIN confirmed on the phone
  (`LoginV2`, `LoginV2WithVerifier`), or QR login when enabled
  (`LoginFlowIDQR`). It identifies as **the LINE Chrome extension**. LINE
  allows one such session, so using LINE for Chrome logs the bridge out and
  the other way round ([README](https://github.com/beeper/line)).
  - E2EE keys are fetched at login. Logins from before 2026-02-14 must log in
    again.
  - Stored: the auth token, refresh data and E2EE key material.
- **History:** `GetMessageBoxes`, `GetRecentMessagesV2(chatMid, limit)`; the
  startup backfill is 50 messages per chat
  ([sync.go](https://github.com/beeper/line/blob/main/pkg/connector/sync.go)).
  LINE serves limited server-side history.
- **Real-time:** `ListenSSE(ctx, localRev, cb)` on
  `https://line-chrome-gw.line-apps.com/api/operation/receive`. It resumes
  from the stored **operation revision** (`GetLastOpRevision`), so missed
  operations replay from the revision, and it prefetches missed chats on
  start ([sse.go](https://github.com/beeper/line/blob/main/pkg/line/sse.go)).
- **Contacts:** `GetAllContactIds`, `GetContactsV2`, `GetBuddyProfile`,
  `FindContactByUserid`.
- **Actions** ([methods.go](https://github.com/beeper/line/blob/main/pkg/line/methods.go)):
  - `SendMessage` for text, images, video, files, voice, mentions and
    replies.
  - `React` / `CancelReaction`; sending is limited to about 120 emoji.
  - `SendChatChecked` (read receipts) and `UnsendMessage`.
  - `CreateChat`, `InviteIntoChat`, accept and reject invitations, block.
  - No edit and no typing in the feature list.
- **Risks:**
  - Letter Sealing works through `pkg/ltsm`, **a Go transpilation of LINE's
    own Emscripten WASM module plus a 4 MiB memory image**
    (`mem_init_0_4m.bin`). Redistributing that inside our public repository
    is a copyright exposure ⚠️ (my assessment, not legal advice). Keep it
    in a separately distributed sidecar, or have the sidecar fetch the
    module at run time.
  - LINE restricts accounts that use unofficial clients; repeated
    email/password logins trigger restrictions
    ([linejs CLI notes](https://github.com/evex-dev/linejs/blob/main/packages/cli/README.md)).
  - Protocol changes break it.

### B. linejs (evex-dev)

[evex-dev/linejs](https://github.com/evex-dev/linejs): TypeScript, **MIT**.
v3.6.4 (2026-11-04), 464★, active.

- A "SelfBot" client with E2EE.
- Device types `DESKTOPWIN`, `DESKTOPMAC`, `ANDROIDSECONDARY`, `IOSIPAD`…
- QR login (recommended because it registers E2EE keys), email/password or
  token login.
- A CLI with `--json` output and an MCP server; ban risk is documented.

A sound alternative if a Node sidecar is acceptable.

### Dead options

- [CHRLINE](https://github.com/DeachSword/CHRLINE): archived 2023.
- matrix-puppeteer-line: its source host returned 502 on 2026-11-09 ⚠️, and
  the Puppeteer DOM-scraping approach was abandoned years ago.

**Recommendation:** beeper/line as a Go sidecar, with the LTSM licensing
caveat handled. Or Beeper Cloud through the Desktop API, with no local
protocol code at all.

---

## 6. WeChat

- **Tencent enforcement:**
  - On 2025-04-30 WeChat's security centre announced a crackdown on
    third-party tools that bypass its protections to read chat records "for
    AI" ([IT之家](https://ithome.com/0/850/207.htm)).
  - On 2025-07-10 it announced tiered penalties up to account bans for
    plugins ([IT之家](https://www.ithome.com/0/867/135.htm)).
- **Taken down:**
  - [sjzar/chatlog](https://github.com/sjzar/chatlog) (Go, 9k★; decrypted
    the local databases and served HTTP/MCP) deleted all code on 2025-10-20
    after "a letter from WeChat".
  - [PyWxDump](https://github.com/xaoyaoo/PyWxDump) deleted its code the
    same day.
  - [Gewechat](https://github.com/Devo919/Gewechat) (iPad protocol) stopped
    service and cites the crackdown.
- **Archived or stale:**
  - [WeChatFerry](https://github.com/lich0821/WeChatFerry) (a DLL hook for
    WeChat PC): archived.
  - [wechaty](https://github.com/wechaty/wechaty) (Apache-2.0): the core
    repo still moves (2.0.0-alpha.13), but its puppets are stale.
    [puppet-wechat4u](https://github.com/wechaty/puppet-wechat4u) (web
    protocol, which new accounts cannot use) last committed 2024-03;
    [puppet-padlocal](https://github.com/wechaty/puppet-padlocal) 2022;
    [puppet-xp](https://github.com/wechaty/puppet-xp) (Windows hook) 2025-07.
  - [wxauto](https://github.com/cluic/wxauto) (Windows UI automation for
    WeChat 3.9.x): README reduced to "2021-2025" and links elsewhere.
  - [duo/matrix-wechat](https://github.com/duo/matrix-wechat): AGPL, last
    push 2025-02.
- **Beeper supports WeChat, closed source.** It links "as a secondary device,
  in much the same way WeChat for Windows or Mac does": scan a QR code,
  confirm on the phone, tick "import recent chat history", and sometimes
  enter a 6-digit security code. It works in Cloud or on-device, and it
  signs out a desktop WeChat session
  ([guide](https://help.beeper.com/en_US/chat-networks/wechat-getting-started-guide)).
- **Tencent's official channel is a bot.** On 2026-03-22 Tencent launched the
  **WeChat ClawBot** plugin for OpenClaw
  ([TechNews](https://technews.tw/2026/03/23/tencent-enables-openclaw-integration-in-weixin/)).
  [`@tencent-weixin/openclaw-weixin`](https://www.npmjs.com/package/@tencent-weixin/openclaw-weixin)
  is MIT, 2.4.10, updated 2026-11-09.
  - It speaks the "iLink" bot API at `https://ilinkai.weixin.qq.com`:
    `ilink/bot/get_bot_qrcode`, `get_qrcode_status`, `getupdates` (long
    poll), `sendmessage`, `sendtyping`, `getuploadurl` (from the package
    source).
  - After a QR binding a **ClawBot contact** appears in the user's WeChat.
    The user chats *with the bot*; it cannot read the user's other chats or
    send as the user, and reportedly cannot join groups.
  - It is a sanctioned way to reach Moss from WeChat, not a connector.
- **Verdict:**
  - **Not feasible to build an "act as the user" WeChat connector**:
    high ban and legal risk, and every open implementation is dead or
    removed.
  - Options in order: Beeper on-device through the Desktop API; an Android
    notification listener for inbound and reply; the iLink ClawBot so the
    user can talk to the assistant inside WeChat.

---

## 7. KakaoTalk

- KakaoTalk speaks the proprietary LOCO protocol; PC and tablet logins are
  sub-device slots.
- **Unofficial projects:**
  - [storycraft/node-kakao](https://github.com/storycraft/node-kakao): MIT,
    last release 2021, last commit 2023. Effectively dead.
  - [KiwiTalk](https://github.com/KiwiTalk/KiwiTalk): Apache-2.0,
    Tauri/Rust/TS client, 764★, pushed 2026-11-05. Its README says Kakao can
    restrict use at any time.
  - A wave of tiny 2026 repositories: loco-wrapper (MIT, 16★);
    kakao-beeper ("mautrix-go bridge + Node LOCO sidecar", 0★);
    frrad/mooo (secondary-device research, 5★).
- Beeper does not list KakaoTalk.
- **Verdict: not feasible** for a product. The only robust degraded path is
  the Android notification listener; KiwiTalk's crates are for research.

---

## 8. Google Chat

### A. Google Workspace accounts: the official Chat API with user OAuth — recommended

- **Requirement:** the Chat API with user authentication works only when the
  developer account and project belong to a Google Workspace organisation.
  Consumer Gmail gets "only available to Google Workspace users"
  ([troubleshooting](https://developers.google.com/workspace/chat/troubleshoot-chat-apps),
  [user auth guide](https://developers.google.com/workspace/chat/authenticate-authorize-chat-user)).
  Whether a Chat app must also be configured in the Cloud project ⚠️.
- **Form:** (F). OAuth and REST suit sandboxed functions; events arrive by
  Pub/Sub, pulled on a schedule or pushed to a substrate webhook trigger.
- **History:** `spaces.list`; `spaces.messages.list` with `filter:
  createTime > "RFC3339"` and ordering, so catch-up is one query per space
  ([messages.list](https://developers.google.com/workspace/chat/api/reference/rest/v1/spaces.messages/list)).
- **Real-time:** the Workspace Events API. Subscribe to target
  `//chat.googleapis.com/spaces/-` (all the user's spaces; user auth only)
  with `google.workspace.chat.message.v1.created`, `.updated` and `.deleted`
  plus batch variants, delivered to a Pub/Sub topic
  ([Chat events](https://developers.google.com/workspace/events/guides/events-chat)).
  Subscriptions **expire**: up to 4 h when the payload includes resource
  data (24 h with domain-wide delegation) and up to 7 days without it ⚠️ (the
  7-day figure is from the Japanese reference). Renew them
  ([subscriptions reference](https://developers.google.com/workspace/events/reference/rest/v1/subscriptions)).
- **Actions:**
  - `spaces.messages.create` (as the user), `.patch` (edit own), `.delete`.
  - `spaces.messages.reactions.create` and `.delete`.
  - `users.spaces.updateSpaceReadState` (mark read).
  - `media.upload` and `media.download`; `spaces.findDirectMessage`;
    `spaces.setup`.
  - **No typing indicator.**
  ([REST reference](https://developers.google.com/workspace/chat/api/reference/rest)).
- **Contacts:** `spaces.members.list`, plus People API for names.
- **Risks:** a Workspace administrator can block the app; restricted-scope
  verification if published widely. Low ban risk: this is the official API.

### B. Personal Gmail: mautrix-googlechat

[mautrix/googlechat](https://github.com/mautrix/googlechat): Python,
**AGPL-3.0**, 127★.

- Last release v0.5.2 (2024-07). `master` last committed 2025-07
  ("Update dependencies"). A `megabridge` branch was last touched 2025-04,
  and a fix branch 2026-04.
- It drives the Chat web app's internal API through cookies (`maugclib`;
  v0.5.0 "switched to web app API").
- Features ([ROADMAP](https://github.com/mautrix/googlechat/blob/master/ROADMAP.md)):
  - Sends text, files, images, video, formatting, threads, replies,
    reactions, text edits, deletions, typing and read receipts.
  - Receives the same plus history backfill; no incoming typing.
- Beeper Cloud offers Google Chat.

**Verdict:** possible as a sidecar, but lightly maintained and cookie-fragile.

---

## 9. Microsoft Teams

### A. Work or school accounts: Microsoft Graph, delegated — fits "act as the user"

The user signs in to our Entra app registration with OAuth 2.0 (authorization
code plus `offline_access`). For `Chat.Read`, `Chat.ReadWrite` and
`ChatMessage.Send`, `AdminConsentRequired` is **No**, though tenants often
disable user consent
([permissions reference](https://learn.microsoft.com/en-us/graph/permissions-reference)).

| Need | Endpoint | Delegated permission |
| --- | --- | --- |
| list chats | `GET /me/chats` | Chat.ReadBasic / Chat.Read |
| history | `GET /chats/{id}/messages` — `$top` ≤ 50; `$orderby` `lastModifiedDateTime` or `createdDateTime` desc; `$filter` `lastModifiedDateTime gt/lt` | Chat.Read ([doc](https://learn.microsoft.com/en-us/graph/api/chat-list-messages)) |
| send, reply with quote, inline images | `POST /chats/{id}/messages` (hostedContents for images) | ChatMessage.Send |
| edit | `PATCH /chats/{id}/messages/{id}` | Chat.ReadWrite |
| delete / undo | `POST /users/{uid}/chats/{cid}/messages/{mid}/softDelete` and `undoSoftDelete` | Chat.ReadWrite |
| react | `POST …/setReaction`, `…/unsetReaction` | Chat.ReadWrite, ChatMessage.Send |
| mark read | `POST /chats/{id}/markChatReadForUser` | Chat.ReadWrite |
| typing | none in Graph | — |

Every row above reads **"Delegated (personal Microsoft account): Not
supported"**.

- **Real-time:** change notifications on
  `/users/{user-id}/chats/getAllMessages` (delegated Chat.Read or
  Chat.ReadWrite; work/school only).
  - At most 10 such subscriptions per user.
  - `includeResourceData` requires an encryption certificate.
  - An expiry over 1 hour requires a `lifecycleNotificationUrl`.
  - Teams `chatMessage` subscriptions last at most 4,320 minutes (3 days).
  - They need a public HTTPS notification URL, which a substrate webhook
    trigger can be
    ([chat message notifications](https://learn.microsoft.com/en-us/graph/teams-changenotifications-chatmessage),
    [overview](https://learn.microsoft.com/en-us/graph/change-notifications-overview)).
  - Catch-up: per-chat `$orderby=lastModifiedDateTime desc` with `$filter gt
    <last seen>`.
- **Contacts:** `/chats/{id}/members`, `/users` (User.ReadBasic.All), People
  API.
- **Form:** (F) for history and sends, plus a webhook trigger for
  notifications. No sidecar needed.
- **Risks:** tenant policy; Graph throttling. This is the official route, so
  ToS risk is nil.

### B. Personal Teams (the consumer service that replaced Skype in May 2025)

- Graph does not support personal accounts for chats (the rows above).
- [purple-teams](https://github.com/EionRobb/purple-teams) (C, **GPL-3.0**, a
  Pidgin plugin, daily builds through 2026-11-06, 218★) is aimed at
  work/tenant accounts. Personal-account support ⚠️.
- **Verdict:** not feasible for personal Teams.

---

## 10. Threema

- **Threema Gateway** issues bot IDs (`*XXXXXXX`) billed per message. It is
  not the user; [Threematrix](https://github.com/bitbetterde/Threematrix)
  (GPL, 2024) is built on it.
- **Multi-device:**
  - [Threema Desktop 2.0](https://github.com/threema-ch/threema-desktop) is
    **AGPL-3.0**, still beta (v2.0-beta67, 2026-11-02), with `libthreema`
    (Rust) inside that repository.
  - [hoehermann/threema-multidevice-client](https://github.com/hoehermann/threema-multidevice-client)
    (Rust, AGPL, 0★) can currently only *receive* reflected messages. It
    needs secrets extracted from a linked Threema Desktop and a patched
    libthreema.
  - Threema's terms forbid creating IDs on self-built clients, so a client
    must link to an official primary device.
- **History:** Threema servers keep no chat history; a linked device sees
  messages from linking onward ⚠️.
- **Verdict: not feasible today.** Revisit when libthreema exposes a stable
  multi-device client API.

---

## 11. Snapchat

- No messaging API: Snap Kit has none.
- Snap enforces aggressively. On 2026-01-29 it sent GitHub a DMCA notice
  (circumvention) that blocked [SnapEnhance](https://github.com/github/dmca/blob/master/2026/01/2026-01-29-snap.md).
- **Beeper supports Snapchat through Beeper Cloud only:** username or email
  and password, CAPTCHA, 2FA, and approval of suspicious logins. Features:
  the eight reactions, replies, deleting own messages, read receipts and
  typing ([guide](https://help.beeper.com/en_US/chat-networks/snapchat-getting-started-guide)).
- Community bridges are works in progress: lalomorales22/snapchat-bridge
  says "95%" but has unverified endpoints; 12th-devs/matrix-snapchat uses
  Playwright on Snapchat Web.
- **History:** chats are ephemeral by default; Snap's "Download My Data"
  export holds saved chat history ⚠️.
- **Verdict: not feasible to build ourselves.** Through Beeper Cloud only;
  the data export for history.

---

## 12. Matrix (native)

- [mautrix-go](https://github.com/mautrix/go): **MPL-2.0**, v0.32.0
  (2026-10-16), 669★. The client supports password and **OAuth login with
  refresh tokens**
  ([CHANGELOG](https://github.com/mautrix/go/blob/main/CHANGELOG.md)), which
  matters because matrix.org moved to the Matrix Authentication Service on
  2025-04-07 ([matrix.org](https://matrix.org/blog/2025/04/matrix-auth-service/)).
  End-to-end encryption uses `crypto` with **goolm** (pure Go) or libolm
  (cgo).
- [matrix-rust-sdk](https://github.com/matrix-org/matrix-rust-sdk):
  Apache-2.0, 0.19.1 (2026-09-18). Use it in the Swift and Kotlin apps
  through its FFI bindings.
- **History:** `/messages` pagination. Decrypting old E2EE history needs
  server-side key backup unlocked with the user's recovery key.
- **Real-time:** `/sync` with a `since` token, so catch-up is built in.
- **Actions:** send, reply, react, edit, redact, read receipts, typing: all
  native.
- **Form:** (S), or inside the server with a database-backed crypto store.
  E2EE device state (Olm/Megolm sessions, device keys) rules out stateless
  functions.
- **Verdict:** the easiest network, and embeddable in the Go server under
  MPL's file-level terms.

## 13. Session, Wire, XMPP (brief)

- **Session.** The account is a key pair (recovery phrase), so a client
  restored from the phrase *is* the user. Messages sit in swarms with a TTL
  (about 14 days ⚠️), and history from before linking is not synced ⚠️.
  Libraries: [session-desktop](https://github.com/session-foundation/session-desktop)
  (GPL-3.0, v1.18.3 on 2026-10-26) and [session.js](https://github.com/sessionjs/client)
  (MIT, small; mirror last pushed 2026-01). Feasible as a sidecar; low
  priority.
- **Wire.** [kalium](https://github.com/wireapp/kalium) (Kotlin, **GPL-3.0**,
  v0.0.8 on 2026-10-02) is Wire's SDK for building clients (Proteus and MLS).
  Personal accounts still exist ⚠️. Feasible as a JVM sidecar; low priority.
- **XMPP.** Users log in natively.
  - History through MAM (XEP-0313), other devices' messages through Carbons
    (XEP-0280), resumption through Stream Management (XEP-0198).
  - Actions through reactions (XEP-0444), corrections (XEP-0308),
    retractions (XEP-0424), displayed markers (XEP-0333) and chat states
    (XEP-0085).
  - Go: [xmppo/go-xmpp](https://github.com/xmppo/go-xmpp) (BSD-3, active)
    and [mellium/xmpp](https://github.com/mellium/xmpp) (BSD-2, last release
    2023).
  - OMEMO is the gap in Go. In Python, [python-omemo](https://github.com/Syndace/python-omemo)
    is MIT and slixmpp-omemo is AGPL.
  - Feasible as a sidecar.

---

## 14. Cross-cutting adapters

### 14.1 The Beeper Desktop API as the long-tail adapter

- **What:** Beeper Desktop's local API, version 0.2.0. The
  [OpenAPI spec](https://github.com/beeper/desktop-api-openapi) and the
  Go, JS, Python and PHP SDKs are **MIT**; Beeper Desktop itself is
  proprietary and needs a Beeper account. Substrate's `beeper` bundle polls
  it every 15 minutes today.
- **Endpoints:**
  - `GET /v1/accounts`, `GET /v1/chats`, `GET /v1/chats/{chatID}`,
    `POST /v1/chats`.
  - `GET /v1/chats/{chatID}/messages?cursor&direction=before|after`.
  - `POST /v1/chats/{chatID}/messages` `{text, replyToMessageID,
    attachment}`, with uploads through `POST /v1/assets/upload`.
  - `PUT /v1/chats/{chatID}/messages/{messageID}` (edit).
  - `POST` and `DELETE /v1/chats/{chatID}/messages/{messageID}/reactions`.
  - `GET /v1/accounts/{accountID}/contacts/list`.
  - Search, archive, reminders.
  - **Missing in v0.2.0:** delete/unsend, mark read, typing.
- **Real-time:** "A live WebSocket endpoint is available at `/v1/ws`.
  Clients authenticate with the same access token and send
  `subscriptions.set` with chatIDs (use `["*"]` for all chats). Server events
  are flat, typed messages: `chat.upserted`, `chat.deleted`,
  `message.upserted`, and `message.deleted`." No resume cursor is
  documented, so catch up with `direction=after` from the last stored message
  per chat.
- **Coverage through one integration:** Viber, WeChat, LINE (Cloud),
  Snapchat (Cloud), Google Messages, Google Chat, iMessage (Mac on-device)
  and the others in Beeper's list
  ([networks](https://help.beeper.com/en_US/chat-networks/which-chat-networks-can-you-connect-in-beeper)).
- **Recommendation:** add a small sidecar that holds `/v1/ws` and POSTs
  batches to a webhook trigger, mirroring `cmd/slack-socket`. Keep the
  15-minute walk for repair. This is the cheapest real-time upgrade for
  every network we cannot implement ourselves.

### 14.2 The Android notification listener

See 3.3: real-time inbound plus reply for any app, lossy. It is the only
self-built fallback for Viber, WeChat and KakaoTalk.

---

## 15. Comparison table

Legend: Hist = how far history goes. RT = real-time and catch-up.
Act = send / reply / react / edit / delete / read / typing, where Y is yes,
– is no, P is partial, R is RCS only and Mine is delete-for-me only.

| Network | Option | Form | License | Maintained | Auth as user | Hist | RT / catch-up | Act (send/reply/react/edit/del/read/typing) | Main risk |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| iMessage | platform-imessage | D (Swift) | MIT | v0.25.8, 2026-11-09 | Mac's Messages.app | all on Mac | FSEvents + ROWID cursor + rescan | Y/Y/Y/Y/Y/Y/Y | UI automation, needs Mac session |
| iMessage | imsg rpc | S (Swift CLI) | MIT | v0.21.0, 2026-11-05 | same | all on Mac | `watch.subscribe` since_rowid | Y/–/P(last msg)/–/–/–/– (more with SIP off) | AppleScript limits |
| iMessage | BlueBubbles | S (Electron) | Apache-2.0 | v1.9.12, 2026-10-27 | same | all on Mac | Socket.IO, webhooks, `query after` | Y/Y*/Y*/Y*/Y*/Y*/Y* (*SIP off) | SIP off, injection fragility |
| iMessage | mautrix-imessage | S (Go) | AGPL-3.0 | legacy, 2026-05 | same | all on Mac | fsnotify + ROWID | via BlueBubbles only | stale |
| iMessage | imessage-exporter | S (Rust CLI) | GPL-3.0 | 5.0.1, 2026-10-29 | Mac or iPhone backup | all | none (export) | read only | — |
| iMessage | pypush / rustpush | S | SSPL | pypush v2.0.2, 2026-03 | Apple ID + Mac identifiers | — | APNs | — | license, Apple ID |
| iMessage (iOS) | Shortcuts automation | D | — | — | phone | none | per-message trigger | inbound text only | lossy |
| SMS + RCS | libgm (gmessages) | S (Go) | AGPL-3.0 | v26.10, 2026-11-07 | Google cookies + emoji | what phone has | long-poll, acked queue, resync | Y/R/R/–/Mine/Y/R | phone online, cookies, QR gone |
| SMS/MMS | Android app (provider) | D (Kotlin) | ours | — | the device | all SMS/MMS | observer + `_id` cursor | Y(SMS)/–/–/–/–/–/– | no RCS; Play declaration |
| SMS/MMS | SMS Gateway for Android | D/S | Apache-2.0 | v1.78.2, 2026-11-03 | device | readable SMS/MMS | webhooks | Y/–/–/–/–/–/– | no RCS |
| Viber | Bot REST API | F | commercial | live | **bot, not user** | none | webhook | bot only | cost, wrong identity |
| Viber | Beeper (Desktop API) | S (ws) | proprietary app; MIT API | launched 2026-10 | QR-linked secondary device | limited | `/v1/ws` + cursor | Y/Y/Y/Y/–*/–*/–* (*not in API v0.2.0) | Beeper dependency |
| Viber | viber.db + lldb key | D | MIT (tool, 0★) | 2026-08 | desktop app | desktop's | file poll | read only | brittle, circumvention |
| Viber | UI automation | D | MIT (unmaintained) | 2026-10 | desktop app | screen | polling | text only | very fragile |
| LINE | beeper/line | S (Go) | MIT (+ LINE WASM blob) | v1.2.0, 2026-11-06 | email+pwd+PIN or QR (Chrome slot) | ~50/chat | SSE + op revision | Y/Y/Y/–/unsend/Y/– | ban, LTSM copyright |
| LINE | linejs | S (TS) | MIT | v3.6.4, 2026-11-05 | QR / email / token | limited | LEGY operation polling (per its examples) | Y/Y/Y/–/unsend/Y/– ⚠️ | ban |
| WeChat | Beeper (Desktop API) | S | proprietary | live | QR as PC device | recent import | `/v1/ws` | Y/Y/P/–/–/–/– | Beeper dependency; kicks desktop session |
| WeChat | iLink ClawBot | F/S | MIT (plugin) | 2.4.10, 2026-11-09 | **bot contact** | none | long-poll getupdates | bot only | not the user |
| WeChat | wechaty / WeChatFerry / chatlog | S | Apache / MIT / – | stale / archived / deleted | various | — | — | — | bans, legal letters |
| KakaoTalk | KiwiTalk / node-kakao | S | Apache / MIT | 2026-11 / 2023 | LOCO sub-device | limited | LOCO push | experimental | ban |
| Google Chat (Workspace) | Chat API + Workspace Events | F | official | live | OAuth (user) | full (filter createTime) | Pub/Sub, renew ≤ 4 h / 7 d | Y/Y(thread)/Y/Y/Y/Y/– | admin policy |
| Google Chat (Gmail) | mautrix-googlechat | S (Py) | AGPL-3.0 | 2025-07 | browser cookies | some (history backfill) | web channel | Y/Y/Y/Y(text)/Y/Y/Y(out only) ([ROADMAP](https://github.com/mautrix/googlechat/blob/master/ROADMAP.md)) | stale, cookies |
| Teams (work/school) | Graph delegated | F | official | live | OAuth (user) | full (`$filter`) | webhooks, ≤ 3 d subs | Y/Y/Y/Y/Y/Y/– | tenant consent |
| Teams (personal) | purple-teams | S (C) | GPL-3.0 | 2026-11 | browser auth | ? | polling | ? | unofficial; ⚠️ personal support |
| Threema | multidevice-client | S (Rust) | AGPL-3.0 | 0★, 2026-08 | linked device (extracted secrets) | from link | reflected msgs | receive only | immature |
| Snapchat | Beeper Cloud | — | proprietary | live | password + 2FA | limited | Beeper | Y/Y/Y/–/Y/Y/Y | Snap enforcement |
| Matrix | mautrix-go / rust-sdk | S or in-server | MPL-2.0 / Apache-2.0 | 2026-10 / 2026-09 | password / OAuth (MAS) | full (+ key backup) | `/sync` since token | all Y | none |
| Session | session-desktop / session.js | S | GPL-3.0 / MIT | 2026-10 / 2026-01 | recovery phrase | ~14 d ⚠️ | swarm polling | Y/Y/Y/?/Y/Y/? | low adoption |
| Wire | kalium | S (JVM) | GPL-3.0 | 2026-10 | email + password | from login ⚠️ | websocket | Y/Y/Y/Y/Y/Y/Y ⚠️ | business focus |
| XMPP | go-xmpp / slixmpp | S | BSD / MIT | active | JID + password | MAM | stream + SM resume | Y/Y/Y/Y/Y/Y/Y | OMEMO in Go |

---

## 16. Ranked recommendations per network

### iMessage and SMS/RCS through the iPhone

1. **On-device macOS connector on platform-imessage (MIT).** It is the only
   SIP-on option with replies, tapbacks, edits, unsend, read and typing. It
   is Swift, the language of our app shell, and Beeper ships it in
   production. Pair it with the `chat.db` cursor-and-rescan scheme in 2.1 for
   lossless catch-up.
2. **imsg `rpc` sidecar (MIT).** Least code for read plus text and file
   sends; its streaming contract (cursor, overflow, mutation events) is
   well specified.
3. **BlueBubbles (Apache-2.0)** as an accepted backend for users who run it.
   Its webhooks fit a trigger and its REST fits functions, but parity needs
   SIP off.
4. **imessage-exporter (GPL, separate process)** for backfill, including
   from iPhone backups.

Avoid pypush and rustpush (SSPL, Apple ID risk) and mautrix-imessage
(legacy). On iOS there is nothing to build; at most an opt-in Shortcuts
inbound tap.

### Android SMS, MMS, RCS

1. **libgm sidecar (AGPL, open-source sidecar).** It is the only way to get
   RCS, with reactions, replies, typing and read state, and Beeper
   maintains it. Accept the Google-cookie pairing and the online-phone
   requirement.
2. **Our Android app reading the Telephony provider**, or SMS Gateway for
   Android, for SMS/MMS-only users and as a backfill source. Declare
   "cross-device sync" on Play, or distribute outside Play. Never take the
   default-SMS role.
3. **Notification listener** for RCS inbound when no Google pairing is
   wanted (lossy).

### Viber

1. **Beeper's Viber connection through the existing Beeper Desktop API**,
   upgraded to the `/v1/ws` socket. It is the only working "as the user"
   path today: QR-linked secondary device, replies, reactions, edits.
2. **Android notification listener**: inbound plus reply, degraded.
3. Do **not** build on the bot API (a different, paid identity), on
   `viber.db` key-lifting (brittle; edges on circumvention) or on UI
   automation (fragile).

Honest bottom line: we cannot own a Viber connector until a linked-device
implementation is published.

### LINE

1. **beeper/line as a Go sidecar (MIT).** Active, SSE with revision
   catch-up, unsend and read receipts. Mitigate the LTSM WASM copyright
   question and warn about ban risk and the single Chrome-extension slot.
2. **Beeper Cloud through the Desktop API**, with zero local protocol code.
3. **linejs (MIT)** if a Node sidecar is preferred.

### WeChat

1. **Beeper (on-device) through the Desktop API.**
2. **Android notification listener**: inbound and reply.
3. **iLink ClawBot**, only so the user can talk to Moss inside WeChat.

Building our own is not feasible: Tencent's 2025 enforcement removed every
viable open implementation.

### KakaoTalk

1. **Android notification listener.**
2. Nothing else production-worthy; KiwiTalk is for research. **Not
   feasible.**

### Google Chat

1. **Workspace: the official Chat API plus Workspace Events.** Sandboxed
   functions plus a Pub/Sub-fed trigger, official, full history, edits,
   reactions, read state.
2. **Personal Gmail: mautrix-googlechat sidecar.** Fragile and lightly
   maintained.
3. Beeper Cloud through the Desktop API.

### Microsoft Teams

1. **Work/school: Microsoft Graph delegated.** Official, it acts as the
   user, needs no sidecar, and webhook triggers receive notifications.
   Typing is missing.
2. Personal Teams: **not feasible**. Graph refuses personal accounts;
   purple-teams is unverified for them.

### Threema

Not feasible now. Track libthreema and Threema Desktop's multi-device work.
The Gateway is bots only.

### Snapchat

Not feasible to build ourselves. Beeper Cloud only; "Download My Data" for
saved history.

### Matrix

**mautrix-go embedded or as a sidecar.** The easiest network and fully
featured; OAuth (MAS) and goolm keep it cgo-free.

### Session, Wire, XMPP

Feasible through their SDKs (session.js or desktop; kalium; go-xmpp) as
sidecars. Build them only on demand.

---

## 17. Unverified items and open questions

- Whether platform-imessage's UI automation works with the screen locked or
  on a headless Mac mini (it needs a GUI session; not tested).
- Whether iCloud-synced Messages history on a fresh Mac is complete.
- That a Mac App Store sandboxed build cannot read `chat.db` or drive
  Messages (inferred from TCC and sandbox rules; ours is Developer-ID anyway).
- What a Shortcuts "Message" automation passes besides text and sender.
- Viber `viber.db` encryption details (single 0★ source), the Linux path,
  whether Viber's Android notifications offer a reply action, and deep-link
  prefill on desktop.
- Viber bot pricing (€100 vs €115 per month; help centre unreachable by
  script).
- Google Messages' internal edit support (libgm exposes none).
- The 7-day Workspace Events maximum without resource data (Japanese
  reference only).
- purple-teams' support for personal Teams accounts.
- Session's swarm TTL and linked-device history; Wire's personal-account
  status.
- The licensing of beeper/line's transpiled LINE WASM (LTSM) when
  redistributed: my reading, not legal advice.
- Beeper Desktop API WebSocket semantics beyond the spec summary (ordering,
  replay, back-pressure) — test before relying on it for lossless delivery.
