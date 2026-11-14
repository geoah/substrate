# Substrate Chat: design and feature list

A native chat client for a substrate's agents, for iPhone, iPad and Mac. It
does one thing the console also does, the agent chat, and does it as a
native app: instant from a warm cache, streaming, Liquid Glass, voice input,
notifications, and an Action Button shortcut. It speaks only the substrate's
API, like Substrate Sync, and carries no web view.

The console's chat is the reference for behaviour. Its rules are ported, not
reinvented: the transcript fold (`web/console/src/lib/api/transcript.ts`),
the stream and its handover (`web/console/src/components/agent/conversation.tsx`),
the chat words (`web/console/src/lib/agent-chat.ts`) and the stream reader
(`web/console/src/lib/api/agents.ts`). Where this document says "as the
console does", those files are the specification.

This is the second version. Two independent reviews (Claude Fable 5.1 and
GPT-6 Astra) checked the first against the server and the console; what they
changed is listed at the end.

## Goals

- **Instant.** Every screen draws from what the app holds before it asks the
  server anything. Opening the app, a chat or Settings never shows a spinner
  where cached content exists.
- **Live.** A reply streams as the model writes it; a chat that is running
  when it is opened is attached to and streams too; the chat list moves as
  threads change anywhere.
- **Honest.** No message is dropped without a word, nothing is sent later on
  its own, and a send whose delivery is unknown is checked before it is
  offered again (the console's rules, #1038).
- **Native.** One SwiftUI target for iOS 26, iPadOS 26 and macOS 26, Liquid
  Glass on the control layer, platform keyboard and menu conventions.

## Non-goals (v1)

- Registering a repository. Sign-in only.
- Editing agents, grants, tools or policies. The app reads agents and writes
  only what a chat writes: messages, a thread's subject, answers, decisions,
  and a provider key under a password sign-in; and the owner's memories and
  instructions, switched on or off, reworded or deleted in Settings.
- Deleting threads. The console offers none; a thread is a run and its cost
  counts after deletion. A thread deleted elsewhere says so when opened.
- Push through APNs. The server has no push channel; the app notifies from
  what it observes (below), and says so in Settings.
- Reply from a notification. A background send that expires before its
  `thread` event needs the whole unknown-delivery path; later.
- Attachments. The chat endpoint takes text.

## Platforms and target

| Item          | Value |
| ------------- | ----- |
| Target        | `SubstrateChat`, one multiplatform SwiftUI target (`supportedDestinations: [iOS, macOS]`), not Catalyst |
| Bundle id     | `dev.reamde.substrate.chat` |
| Display name  | Substrate Chat |
| Minimum OS    | iOS 26, iPadOS 26, macOS 26 |
| Dependencies  | ChatKit (`chat/ChatKit`: the list and its conversations on the kit's runtime, the fold, the words, what the device keeps) over the kit's portable libraries; SubstrateKit for the screens, the sign-in flow and the device services; speech is the system's |
| Keychain      | service `dev.reamde.substrate.chat`, `AfterFirstUnlockThisDeviceOnly` |
| macOS sandbox | network client, audio input |
| Tests         | `ChatKitTests` (`swift test`, on the Mac and on Linux), and `SubstrateChatTests` for what only the Apple app holds |
| Distribution  | `mise run apps:release prerelease chat` to `<release folder>/chat/`, as Sync; listed on the internal catalogue |

`mise run apple:build:chat` builds both platforms, `apple:sim:chat` runs the
iOS app on a simulator, `apple:test:chat` runs ChatKit's tests and then the
app target's, and `apps:test:linux` runs ChatKit's on Linux.

## Architecture

```text
ChatKit/Sources/ChatKit/      no UI framework; builds for Linux and Android
  ChatApp/       the root under the kit's link: the list, the agents, the cards, the rows, the watch, the device's folder
  Conversation/  one chat, a child of the list: the run, the overlay, Stop, the delivery check, the poll
  Model/         the transcript fold and the live overlay, the notices, the agents and threads, the tool words, Markdown blocks
  API/           the agent streams over the kit's line stream, errors as values
  Local/         the cache folder (ChatStorage over the kit's RepositoryFolder), the owner's choices
  Presentation/  every value the app draws: the list, a conversation's screen, its turns and cards, Settings
apple/Sources/
  App/       ChatHost (the one PresentationModel; the browser sign-in, Change Access, the password door), the device services, App Intents
  Views/     the split view, the list, the conversation, the composer, cards, settings, sign-in: presentations drawn, commands sent
  Markdown/  the blocks as SwiftUI, inline spans through `AttributedString`
  Voice/     Dictation: SpeechAnalyzer + SpeechTranscriber over AVAudioEngine
```

**On the kit's runtime** (docs/plans/app-kit.md §5.2, the composition proof).
The kit's `LinkController<ChatApp>` is the root: the session and its
revocations, the status check and connectivity are the link's, and it keeps
`ChatApp` as its one child. `ChatApp` keeps each `Conversation` as a child of
its own, keyed by the id it minted for it. The views read one
`PresentationModel`'s tree and send commands down a path of keys; a step that
changes the list and a conversation publishes one tree.

| What | Where it lives now |
| ---- | ------------------ |
| A turn's stream | `Conversation`'s `.sequence` effect under the key `run`: a send, an attach (its read, then the events), the handover read |
| Streamed text | `Conversation.coalescing(of:)`: a delta or a piece of reasoning presents within 50 ms, structure at once |
| The watch | the kit's `WatchFollower`, its cursor and the rows still to read again in the kit's `RecordCache` (`records/watch.json`) |
| The cache | `ChatStorage` over the kit's `RepositoryFolder`, at the path every build used (`Application Support/SubstrateChat/<hash>/`): `state.json` and `drafts.json` adopted at the top, transcripts moved from `messages/` into the cache area, bounded by age and budget; a session reaches it only through the `ChatStorage.Lease` its open answered |
| Notifications | the kit's `Notifications` device service (`SystemNotifications` on an Apple device): scheduled (at once), cancelled and badged as effects; the kit's delegate, installed at launch, hands a tap to `responses()`, which the list follows and turns into a route; the engine never notifies a chat on screen |
| Background | the kit's `BackgroundWork` device service: the refresh job's runs, scheduled with the repository and the link generation and checked against both, stopped when the system takes a run back or its budget runs out; and continued processing for a streaming turn |
| The owner's choices | `ChatPreferences`, kept in UserDefaults under the keys every build used |

### The API, as this app uses it

Every list is `GET /api/v1/records?first=N&filter=<json>&orderBy=<spec>[&after=<cursor>]`,
the filter one JSON object, query values percent-encoded strictly
(`+` included). A record's path is its kind's three segments and its id as
one segment, encoded exactly once: an agent's id
`x.example.com/pkg/assistant` travels as `x.example.com%2Fpkg%2Fassistant`.

| Need | Request |
| ---- | ------- |
| Server check | `GET /.well-known/substrate/server.json` (`server.version`) |
| Sign-in metadata | `GET /.well-known/oauth-authorization-server` (RFC 8414; 404 on a server without app authorization; `scopes_supported` must list `full`) |
| Sign in | the browser on `GET /oauth/authorize` (code, PKCE S256, `scope`, `state`), the redirect on `http://127.0.0.1:<port>/oauth/callback`, then `POST /oauth/token` (form) → `{access_token, scope, expires_in?, repository}` |
| Sign out | `POST /oauth/revoke` (form `token`, `client_id` it was approved under, none where the session never kept one); kept and tried again at launch until it answers 200 |
| Still set up | `POST /api/v1/authorizations/check` `{scope, details}` → `{scopes: {missing}, setup, expiresAt}`, on a server whose metadata lists detail types |
| Sign in, older server | `POST /login` `{repository, password, totpCode?, label}` → 201 `{token: {id}, secret, repository}`, or 401 `totp_required` to ask for the code and send it again; out with `DELETE /tokens/{id}` |
| Agents | `{"kinds":["substrate.reamde.dev/core/agent"]}`, every page |
| Providers | `{"kinds":["substrate.reamde.dev/llm/provider"]}`, every page |
| Whose key | `GET /api/v1/operator/defaults` → `{items: [{id: "llm/<row>", source, readiness}]}`, read with full access alone; 404, 405 or 501 on a server older than it, where each row's own `apiKey` says |
| Chats | `{"kinds":["…/llm/thread"],"properties":{"mode":{"eq":"chat"}}}`, `updatedAt:desc`, first 100 |
| Automated runs (when shown) | same, `mode in [manual, call, record, schedule, webhook]`, first 50 |
| Titles without a subject | `{"kinds":["…/llm/message"],"properties":{"role":{"eq":"user"},"thread":{"in":[ids]}}}`, `turn:asc,createdAt:asc`, first 500 |
| One thread | `GET /api/v1/substrate.reamde.dev/llm/thread/{id}` (404 or `deletedAt`: deleted) |
| A transcript | `{"kinds":["…/llm/message"],"properties":{"thread":{"eq":id}}}`, `turn:desc,createdAt:desc`, first 500, reversed |
| Pending questions | `{"kinds":["…/llm/interaction"],"properties":{"state":{"eq":"pending"}}}`, first 100 |
| Pending suggestions | `{"kinds":["substrate.reamde.dev/core/recordpatchrequest"],"properties":{"decision":{"eq":"proposed"}}}`, first 100 |
| Pending approvals | `{"kinds":["…/llm/approval"],"properties":{"state":{"eq":"pending"}}}`, first 100; a refusal (a server older than the kind) reads as none, and the watch names the kind only once this read was answered |
| Send | `POST /api/v1/substrate.reamde.dev/core/agent/{agent}/chat` `{thread, message}` → ndjson |
| Attach | `GET /api/v1/substrate.reamde.dev/llm/thread/{id}/events` → ndjson |
| Stop | `POST /api/v1/substrate.reamde.dev/llm/thread/{id}/stop` → the settled result |
| Rename | `PATCH …/llm/thread/{id}` `{properties: {subject}}` |
| Answer questions | `PATCH …/llm/interaction/{id}` `{properties: {state: answered, answers: [{question, selected: [stored option values]}]}, ifVersion}`; or `{state: dismissed}` |
| Decide a suggestion | `PATCH …/core/recordpatchrequest/{id}` `{properties: {decision: accepted\|rejected}, ifVersion}` |
| Approve a call | `PATCH …/llm/approval/{id}` `{properties: {state: approved\|denied}, ifVersion}`; the engine runs the stored call, and the card reads the record again as the watch reports it move |
| Add a model key | `PATCH …/llm/provider/{id}` `{properties: {apiKey}}`, a password sign-in only |
| Memories and instructions | `{"kinds":["…/llm/memory"]}` and `{"kinds":["…/llm/instruction"]}`, `createdAt:desc`, every page |
| Switch one, reword one | `PATCH …/llm/memory/{id}` or `…/llm/instruction/{id}` `{properties: {active} or {text}, ifVersion}` |
| Delete one | `DELETE …/llm/memory/{id}?ifVersion=` or `…/llm/instruction/{id}?ifVersion=` |
| Live | `GET /api/v1/changes?watch=1&kinds=…&from=&generation=` |

Resolving an interaction or a request must carry the version the owner read
(`internal/engine/write.go:650`); a `409` means it moved: the card re-reads
and asks again, never retries blind.

**The actor.** A request names no actor: the server derives it from the
token (decision 0270). An app token's writes link the app's actor record and
act for the person, its token labelled with the client's host; a password
sign-in's link the person's, its token labelled "Substrate Chat on <device>",
which is what tells them apart in History.

### Streams: every event, every exit

| Event | On a send | On an attach |
| ----- | --------- | ------------ |
| `thread` | binds the conversation to the thread (below) | ignored |
| `reasoning`, `delta`, `toolStarted`, `toolFinished`, `compacted` | fold into the overlay | dropped if a stored row carries its `turn`, else folded |
| `truncated` | not sent | the replay lost events: the overlay is marked partial ("Part of this answer arrived before you opened the chat; it shows in full when it finishes") |
| `idle` | not sent | the whole answer: `running` is a turn this server does not stream (background, below); any other status ended between the row read and the attach: hand over |
| `done` | settle and hand over | same |
| `error` (after the 200) | settle; a card saying what happened, the server's words beneath it | same |
| `{}` | heartbeat, every 30 s; ignored | same |

| Exit | What follows |
| ---- | ------------ |
| 4xx with the server's envelope before `thread` | refused: 409 a turn already running or the repository's 4 at once, 422 a hidden agent, 404 no such agent. Text back in the composer, the server's words on a card |
| 5xx, 503 shutdown, a gateway's answer, a dropped connection, a stream that ends before any event | unknown delivery (below) |
| Stream ends after `thread`, no `done` | the turn runs on: attach again, backing off 0, 1, 2, 4, 8, 15 s |
| Attach answers 4xx other than 409/429 | permanent (404 a deleted thread): settle, say so, release the composer |
| 401 anywhere | the session ended: one screen, Sign In Again |

**Background runs.** `idle` with `running` means this server process has no
stream for the turn (a trigger, a schedule, a turn another replica owns).
The chat says "<Agent> is working in the background", Stop is not offered,
and the thread is read every 5 s (and on every feed row for a thread or a
message) until its status settles; then the rows are read.

**Stop** posts `stop`. A `409` means nothing streamable runs: the thread is
read again, and what it says is shown.

### The store and its concurrency rules

`ChatApp` is the one chat list for the whole process; every window reads its
tree. It holds agents, providers, the chat list, each thread's rows, the
pending questions and suggestions, and the conversations as its children.
Each rule below is now the runtime's behaviour or an engine's step, and has a
test by its number in `ChatKit/Tests/ChatKitTests/RuleTests.swift`:

| Rule | Where it lives | Its test |
| ---- | -------------- | -------- |
| 1 | `ChatApp.Command.show`/`newChat` mint the child's key; the conversation's `named` event reaches the list through `event(fromChild:_:after:)`, which binds the thread and drops any other conversation on it (`ChatAppEngine.reported`) | `rule1_aConversationIsKeyedByItsClientIDAndTheThreadEventBindsIt` |
| 2 | the stream runs under the key `run`; `Conversation`'s `opened` step starts nothing while a run is active | `rule2_oneStreamPerConversation` |
| 3 | the conversation's ownership reaches the list with each of its events; `load` refuses an owned thread, and `rowsRead`, `prefetched` and `cachedLoaded` drop one, while a conversation's own reads publish | `rule3_noRowsPublishUnderALiveOverlay`, `rule3_aCachedReadThatLandsUnderALiveOverlayIsDropped` |
| 4 | `ChatAppState.readEpochs`, carried by every read, cached read and prefetch, and moved in `ChatAppEngine.reported` by a binding, a send or an attach (the conversation takes the thread) and a settle (it hands it back), each of which also cancels the read under `read:<thread>`; a read that fails clears what is outstanding, so the next asks again | `rule4_aReadIsDroppedWhenItsThreadsEpochMoved`, `rule4_aCachedReadIsDroppedWhenItsThreadsEpochMoved` |
| 5 | the runtime's key epochs: a new run restarts `run` and cancels `retry`, `poll`, `pollTimer`, `stop`, `stopWait` and `check`, so a late result is dropped | `rule5_aNewRunDropsWhatAnOlderRunStillDelivers` |
| 6 | the runtime's controller generation: `ChatApp.resetting(of:on:)` starts the list over when the link's repository changes or is gone, closing every conversation; the folder's lease is retired, so nothing still on its way reaches the folder, the link generation in it moves, the rest of it is deleted on Sign Out, and the notifications are removed; the next sign-in's open runs after all of it | `rule6_aSignOutDropsEverythingStillOnItsWay`, `aSaveStillOnItsWayAcrossASignOutWritesNothing` |
| 7 | `ChatAppState.isCurrent` | `rule7_cachedRowsAreCurrentOnlyWhileTheirThreadIsSettledAndUnmoved` |

1. **A conversation is keyed by a client UUID**, never by an agent. A new
   chat has a UUID and no thread; the `thread` event binds it, atomically
   moving its draft, overlay and selection, and from then the store finds it
   by thread id. There is exactly one `Conversation` per thread in the
   process: two windows on one chat share it, its stream and its composer
   state; two New Chat windows have two UUIDs.
2. **One stream per conversation.** The app's own send is never attached to;
   opening a chat the store already streams opens nothing.
3. **No rows under a live overlay.** While a conversation sends, streams or
   hands over, no read for its thread publishes: not the open-time read,
   not the feed's, not a prefetch. Only its own handover read does.
4. **Read epochs.** Every thread has an epoch. A transcript read records the
   epoch it started under, and its result is dropped if the epoch moved.
   Send, attach and settle move it, and cancel reads in flight.
5. **Run epochs.** Every run of a conversation has a number; a callback from
   an older run (a cancelled stream still delivering) does nothing.
6. **Session epoch.** Signing out moves it: streams, reads, the watch, the
   prefetch and pending disk writes are cancelled or dropped, memory is
   cleared, notifications removed, then the directory and the Keychain item
   are deleted. Nothing of one session publishes into the next.
7. **Freshness by version.** Cached rows are stored with the thread
   `version` they were read under. They are current only when the thread is
   settled and its version is unchanged; a `running` thread's rows are
   incomplete by definition and are re-read on open. A message write does not
   move its thread, so the thread's `updatedAt` is never the test.

**The handover** (as the console): on `done`, the rows are read under a new
epoch; when they land, they replace the overlay in the same update, so the
transcript never blinks: the conversation's `handedOver` step clears the
overlay and the list publishes the rows inside the same step of one tree
(`aSendStreamsThenHandsOverInOneStepWithoutDoubling`). A read that fails or outlasts 15 s keeps the
overlay, releases the composer and offers Try Again.

**Unknown delivery.** The app waits a second and reads.

- On an existing chat: the thread fresh, and a user row past the newest turn
  held before the send (unknown if the last reload failed) with the same
  text. Found: the card clears and a running turn is attached to. Not found:
  "It may not have arrived", with the text to copy. Same text is evidence,
  not proof, and the card says "looks like it arrived".
- On a new chat: the agent's newest `chat` threads (`startedAt:desc`,
  first 5); one started after the send whose first user row is the text is
  adopted. Otherwise the same card, and the text stays.
- Nothing is ever sent again on its own.

Deltas are coalesced and flushed every 50 ms, so a fast model does not
redraw the transcript hundreds of times a second.

### Caching: why everything is instant

`ChatStorage` writes JSON under Application Support through the kit's
`RepositoryFolder`, one directory per signed-in identity (server and
repository, hashed), the directory every build has used. On iOS the
files are `completeUntilFirstUserAuthentication` (the Keychain item's class),
and the directory is excluded from backup: all of it is the server's and can
be read again.

| File | Holds |
| ---- | ----- |
| `state.json` | format version, agents, providers, the chat list, opening lines, pending cards, read marks, the notified set; a build before ChatKit kept the feed cursor here too |
| `cache/<thread>.json` | one thread's rows, the thread `version` they were read under, and whether the window was full (in `messages/` before ChatKit; moved at the first open, the newer copy kept where both areas hold one, and `messages/` removed only once every file in it is moved) |
| `records/watch.json` | the watch's cursor and the rows it marked to read again (the kit's `RecordCache`); the cursor `state.json` kept before ChatKit moves into it at the first open, which removes it from `state.json`, so it is read once |
| `drafts.json` | each chat's unsent draft |
| `link.json` | the link generation (the kit's `LinkGeneration`): moved by every sign-out, and the one file Sign Out keeps, so a refresh job scheduled before it does nothing after the next sign-in |

- **Launch** decodes `state.json` (small: rows a list needs) before the
  first frame; transcripts are decoded when a chat opens or is prefetched.
  An unreadable or older-format file is discarded, never trusted.
- **Opening a chat** draws its cached rows at once, then reads if they are
  not current (rule 7).
- **Prefetch**: after each list refresh, while the app is in the
  foreground and nothing streams, up to 8 settled chats whose cached version
  differs are read one at a time, 100 rows each; a read that fills the window
  is marked partial and re-read whole when opened.
- **Writes are behind**: atomic, coalesced per file, off the main actor,
  each through the session's lease, which reaches nothing once a sign-out
  retired it.
- **Bounded**: transcripts of chats not opened in 60 days, and the oldest
  past 100 MB, are dropped. Settings → Storage shows each area's size, the
  transcripts against the budget, and clears them (the kit's Storage rows:
  "Saved copies", "Clear Saved Copies").
- **The live feed** runs while a scene is active (and while the Mac app
  runs): kinds thread, message, agent, provider, interaction and request. It
  resumes from the list read's `head` and `generation` (taken before the
  reads, so nothing between them is missed), reads the opening `bookmark`,
  advances its cursor after applying each row, and persists it. A `410` or a
  terminal `error` frame with code `compacted` drops the cursor, which is
  never sent again, re-lists everything and opens again at the new head: one
  recovery a minute. Another `compacted` within 60 seconds of the start of
  the last recovery is a failure, not another re-list, even after the watch
  opened at its bookmark or delivered rows: the feed stops with a status
  line and opens again on the next return to the foreground or to the
  network. Only the minute passing, on the app's clock, makes the allowance
  whole again (`WatchFollower.recovers`). A list read part way refused
  `compacted` is read again from its first page once. Another terminal frame
  stops it with a status line.
  A thread row re-reads the list (debounced 300 ms) and that thread's rows
  where the rules allow; a message row re-reads any background-running chat
  that is open; an agent or provider row re-reads those; an interaction or a
  request row re-reads the pending cards.
- **On return to the foreground**: the watch reopens, every streaming
  conversation re-attaches, and running chats in the list are attached to
  (at most 4, the server's own limit).

### Notifications

The server has no push channel, so the app promises what it can observe:

- **A reply settles** while its chat is not on screen (no key window shows
  it, or the app is in the background): a notification with the agent's
  name, the chat's title and the start of the reply, grouped by thread.
  Tapping opens the chat. Keyed by thread and `finishedAt`, so a rename never
  notifies.
- **Attention**: a new pending interaction ("Assistant has 2 questions"),
  a proposed request ("Assistant suggested a change") or a pending approval
  ("Secretary wants your approval to run gmail_send") notifies once per
  record and opens the chat that asked.
- **On iOS**, when the app leaves the screen with a turn streaming, it asks
  for a `BGContinuedProcessingTask` (iOS 26, strategy `.fail`, a fresh
  identifier each time, as Sync's `ContinuedSync`): iOS keeps the stream
  open and shows the progress with a Stop button. The bar moves on every
  event and every heartbeat, since a long tool call emits nothing else and a
  bar that stands still is expired. On `done` the notification goes and the
  task completes. Where iOS will not run one, a background task gives about
  30 s. A `BGAppRefreshTask` is the backstop: it reads the chats the app saw
  running and the pending cards, and notifies what settled or appeared. It
  runs when iOS decides; Settings says so. Its job names the repository and
  the link generation it was scheduled under, and a run for another, or one
  that outlives its budget or that iOS takes back, writes and notifies
  nothing.
- **On macOS** the stream and the feed run while the app is open, so replies
  and cards notify when they happen (unless the Mac sleeps).
- **Opening a chat** takes back its notifications, delivered and still
  pending (each one's id starts with the thread's).
- **Permission** (alerts, sound and the badge) is asked after the first
  message is sent. Settings has a switch for replies and one for questions
  and suggestions; automated runs never notify unless they ask something.
- **Unread**: a chat whose `finishedAt` is newer than when it was last seen
  settled shows a dot; the iOS badge counts them. At the first sign-in every
  chat is marked seen, so history is not unread.

### Voice typing

The microphone runs `SpeechAnalyzer` with a `SpeechTranscriber` (on-device),
falling back to `DictationTranscriber` where the transcriber is unavailable.

- **Assets**: the locale is the one picked in Settings → Voice, else the
  device's, matched by `supportedLocale(equivalentTo:)` and then by language;
  a model not installed is downloaded with `AssetInventory` on first use, its
  progress in the composer.
- **Audio**: iOS uses a `.playAndRecord` session in `.spokenAudio` mode,
  activated before the engine and deactivated after; an interruption or a
  route change stops cleanly and keeps the words. The tap's buffers go
  through an `AVAudioConverter` to `bestAvailableAudioFormat`.
- **Words**: tap the microphone and listen; finalized words in the primary
  colour, volatile ones dimmed, inserted at the cursor with a space, never
  replacing typed text. Tap again to stop (`finalizeAndFinishThroughEndOfInput`,
  1 s at most); Send stops, finalizes and sends what was heard.
- **Permissions** are asked at the first tap; a refusal says where to change
  it. Leaving the app stops listening. The keyboard's own dictation still
  works in the field.

### Markdown

Replies are Markdown. A block parser (paragraphs, headings, nested ordered
and unordered lists, task items, fenced and indented code, quotes, tables,
rules) renders blocks as SwiftUI views and inline spans through
`AttributedString(markdown:)` with `inlineOnlyPreservingWhitespace`. It is
safe on any prefix of a reply: an unclosed fence is code, a half table is
text. Settled messages are parsed once and memoized; only the streaming one
re-parses on each flush; text past 200,000 characters is plain. Code blocks
scroll sideways and have Copy. A link opens only when it is `http`, `https`,
`mailto`, a console path (`[Books](/applets/books)`, which opens that page
of the console on the signed-in server) or a record link (below), and one
that opens outside the app carries the external-link mark beside its text,
which VoiceOver reads as "opens in your browser".

## The interface

**Layout.** `NavigationSplitView`: chats in the sidebar, the conversation in
the detail. iPhone collapses it to a stack. The selected chat and the
sidebar state are scene storage, so a relaunch returns where it was.

**Glass is the control layer only** (HIG): the toolbars (already glass on 26),
the composer and the Jump to Latest button, which share one
`GlassEffectContainer` so they merge rather than stack. Content is not glass:
suggestion chips, cards, code blocks and bubbles use plain fills; the user's
bubble is a tinted fill. The transcript softens under the composer with
`scrollEdgeEffectStyle(.soft, for: .bottom)`.

### Chat list

- Grouped Today, Yesterday, Previous 7 Days, Earlier, by last activity (the
  console has no week group; the list here is longer).
- A row: the title (`subject`, else the opening line, else "New chat") and
  when, then the agent's name in its colour running into the start of the
  last reply ("Working…" while it runs), and a state: a dot when unread, a
  question mark when a card waits on the owner, and the console's mark in
  words for a last turn that didn't finish: "Didn’t finish" (orange) for a
  restart or a limit, "Stopped" (grey) for a stop, "Failed" (red) for an
  error. No avatar and no agent pill: each took a column and said no more
  than the name.
- Automated runs are behind a toggle in the list's filter menu (off by
  default), with a glyph for what started each.
- Search filters titles, previews and cached transcripts, and says it is
  searching what is on this device.
- Swipe and context menu: Rename, Mark as Read or Unread, Copy Thread ID.
- Pull to refresh; an offline banner ("Offline. Showing saved chats.").
- Toolbar: New Chat (⌘N); Settings (⌘, on the Mac). On a phone the search
  field sits in the bottom bar with New Chat to its right.

### Conversation

- Title: the chat's title (tap to rename, optimistic); subtitle: "with
  <Agent>", or "Working…" while it runs.
- **User turns**: right-aligned tinted bubbles; Copy in the context menu.
- **Assistant turns**: full width, Markdown, no bubble; Copy and Share.
- **Thinking**: "Thinking…" with a shimmer while the model reasons, then
  "Thought for 6 s", collapsed; tap to read.
- **Tool calls**: one line per call in the console's words ("Searched for
  “dentist”, found 3", "Changed the description of “Dune”", "Added “Dune”
  to your books", "Asked Researcher", "Edited the body of “Garden” in 2
  places"), a spinner while it runs, a check or a cross when it settles;
  tap for what it found or failed on, and the raw arguments and output. A
  call that worked on one record names it in its line, quoted and cut at 60
  characters: by the heading its answer carried, else the one the chat's
  own lookups returned, else the one read for it (the kit's record
  directory, which the pills read names through); without one, the kind
  with its article ("Changed an author"). A failed call names none. One
  that found or changed several records shows the first three as pills
  under its line, open or not, and "and 2 more" shows the rest; the one
  record a line names keeps its pill in the opened details (a write's, in
  its panel). A
  call that runs in the background says "Working in the background" until
  its job ends, and its end is a line of its own ("Librarian finished in the
  background") that settles the call with what it returned. A write the
  owner's policy held for review says "Waiting for you" above its
  suggestion, not a failure. A sub-agent's call opens to "Open its
  conversation", its own thread.
- **Results of a known shape** (the console's `lib/tool-views.ts`): a
  function whose output is a list of web links draws them under its line as
  numbered source pills (the site and the title, the snippet as a long
  press's preview and the Mac's help), and one page read as one pill, each
  opening the page in the browser. The match is on the shape, never a
  provider's name, and only an `http` or `https` address is a link.
- **Changes**: a `write` shows what it wrote, open, under its line, as a
  panel ("Changed [Groceries]", each value it set). A function's own
  changes show under its line as pills after the verb ("Saved [a] [b]",
  one run per verb), and so do several changes of any call; a host
  function's or a sub-agent's one change is in its opened details, beside
  what a sub-agent's chain wrote ("Wrote 3 records across task and note").
  All of it comes from the transcript (the call's arguments and answer, the
  row's change stamps), so it shows whatever the token may read; a record's
  name comes from what the chat's own lookups returned.
- **Linked records** are pills everywhere (SubstrateKit's `RecordPill`): the
  kind's glyph and the record's name, the same as the console draws them. A
  tap opens the record read-only in a sheet, its own links pushing within
  it. A session an earlier build kept with scopes reads what they reach; a
  kind out of that token's reach is said so ("This app can't read people", and where to allow it), never
  shown as missing, since the server answers both alike.
- **Record links in a reply** (`[Q4 budget](substrate://<kind>/<id>)`, or the
  older `ref:<kind>/<id>`) stay in the text, tinted, and a tap opens the
  record like a pill. A link the engine found no record for (the reply row's
  `mentions`, marked `missing`) is its text alone, so an invented id never
  reads as a record. Under a settled reply, "Linked records" repeats each
  record it links as a pill, since a link inside a line of text cannot be one.
  The `done` event's mentions reach the reply at once, so its links read so
  from the run's end, before its rows land.
- **Sources**: under a finished reply, the web pages it links as numbered
  pills, once each, in the order it first cites them.
- **Questions** (`ask`): a card with each question's options (one or many,
  as the console), Answer and Dismiss; a resolved one shows the choices.
- **Suggestions** (`propose`): a card for a create, a patch or a delete:
  the record, each property's before and after, Apply and Dismiss, drawn
  from the `propose` call at once; Apply reads the request's version if the
  card holds none, and refuses if the change is no longer the one shown.
  Once decided, the card moves to the decision (below).
- **Compactions**: a divider, "Earlier messages were summarized".
- **Decisions and answers** the substrate wrote into the thread: a line in
  the tool calls' shape ("You applied the suggestion", a check or a cross);
  a decision is followed by the change as a panel, marked Applied ("Changed
  “Groceries”", the values applied, the owner's own where they adjusted
  them) or Dismissed (the values struck through). It is the transcript's:
  no read, so nothing to fail.
- **Triggered openings**: "When <record> changed", not JSON.
- **A last turn that didn't finish**: once nothing runs (no stream, no
  attach, no handover waiting), a chat whose thread is `stopped`,
  `overbudget` or `error` says so under the transcript in the console's
  words, from the thread's stored status and the first line of its reason:
  "Moss didn’t finish" with why ("The server restarted while it was
  working.", "It ran out of time.", "It used all 8 of its turns."), "Moss
  was stopped before it finished", or "Moss ran into a problem and didn’t
  finish" with "What failed: …". Continue sends "Continue where you left
  off." as the owner's own message, since the server resumes a stopped
  thread only when its owner writes, and leaves a half-typed draft in the
  composer; while a send cannot go it waits, with the composer's reason.
- **Errors**: a failed run's card says what happened ("OpenAI refused the
  API key", "OpenAI is turning requests down", else "<Agent> couldn't
  answer"), with the server's words beneath it under "The server said".
  Once the thread says the turn ended in an error, its notice (above) says
  what failed, and the card stays beside it only where it names a refused
  key or a rate limit, which the notice cannot. Refused and
  unknown-delivery cards; a reload failure with Try Again; "This chat was
  deleted".
- **A provider without a key**: when no key resolves for it, its own or one
  the operator supplies (the operator read's `llm/<row>` item; before that
  read answers, no row is said to lack one), a callout, "Add an API key for OpenAI",
  in a new chat above its openers and scrolling with them, else above the
  composer, with a sheet that names the provider's endpoint and, for
  a password sign-in, writes the key (an app token links to the provider's
  record in the console). The key lives only in the field until it is sent;
  it is never cached or logged.
- **More than 500 rows**: "Older messages aren't shown here" at the top.
- The view follows the bottom while turns arrive (`defaultScrollAnchor(.bottom)`),
  stops when the reader scrolls up, and offers Jump to Latest;
  `scrollDismissesKeyboard(.interactively)`.

### New chat

- The agent: its name and description as the header, and its pill as the
  picker in the composer, 32 points high in the 46-point field and close to
  its type, the chevron inside; default from Settings, else the last chatted with, else the
  first offered.
- Up to three openers by what the agent can do, as plain chips.

### Composer

A floating glass bar (`safeAreaBar`) holding a field that grows 1 to 8 lines,
the microphone, and Send, which morphs into Stop while a turn runs
(`glassEffectID`); on a new chat, the agent chip.

- **Mac**: Return sends, Shift-Return and Option-Return are new lines
  (`onKeyPress`). **iPad with a keyboard**: ⌘Return sends. **Both**: ⌘. stops.
- While a send cannot go, the text stays and the bar says why ("Waiting for
  the answer", "Checking whether your last message arrived", "Offline", "Add
  an API key first").
- Drafts are kept per chat across launches.

### Settings

- **Assistant**: the owner's memories and instructions (what their
  assistants know about them and how they should work), each list a screen
  of its own: switch one off, reword it or delete it, each write over the
  version shown, at once on screen and put back if the server refuses.
- **Account**: SubstrateKit's: the server, the repository, the server's
  version, the approved access in the approval page's words and until when,
  Change Access and Sign Out. **App**: the version and updates.
- **Agents**: every chat agent with a "Show in chat" switch and the default
  for new chats; each with its provider and model, and "No API key" with Add
  Key. Agents hidden from chat are listed apart, "Works for other agents".
  What is stored is the set the owner hid and the set they added, so a new
  primary agent appears on its own; with no choice made, the primary agents
  are offered, or all of them when none is primary.
- **Chats**: show automated runs; expand thinking; show technical details
  (the thread id, status and tally under the title).
- **Notifications**: the two switches, the system's state with a link to its
  settings, and what the app can and cannot see.
- **Voice**: the language.
- **Storage**: size; Clear Saved Chats.
- **About**: the version.

### Action Button and Shortcuts

App Intents, offered as App Shortcuts so the iPhone's Action Button can run
them without a Shortcuts workflow:

- **New Chat** (`OpenIntent`-style, opens the app): optional **Assistant**
  (an `AppEntity` over the offered agents, from the cache), optional **Start
  Listening**. Opens a new chat with that assistant (else the default), and
  with Start Listening on, the microphone is already on.
- **Open Chats**: opens the app on the list.
- Siri phrases "New chat in Substrate Chat", "Ask <assistant> in Substrate
  Chat".

### Sign in

Through the substrate's app authorization (decision 0194 and its follow-up
that opens agents, decisions and the feed to apps), on SubstrateKit's
connect screen and flow: the server is checked against its discovery
document and its OAuth metadata, then the server's own sign-in and approval
page opens: in the default browser on the Mac, where its address bar and the
person's password manager work, and the window comes forward once the
redirect lands; in the system's Safari sheet (`ASWebAuthenticationSession`,
sharing Safari's cookies) on iPhone and iPad. The owner chooses what the app
may do and for how long; the app never sees the password.

- **Scope asked for**: `full` (decision 0204). A scoped token talks only to
  an agent whose reads its grant covers, and no scope covers the core and
  llm kinds, so the owner's assistant, which reads every kind, would be out
  of reach; and only full access reads which keys the operator supplies.
  A session an earlier build kept holds `agents:*`, `decide:*`, the provider
  rows and `read:*`, or what the owner kept of them, and works with that:
  without `agents:` the app says it cannot talk to the assistants and offers
  Change Access; without `decide:` a suggestion card links to the console;
  without the providers it shows no key callout; without `read:*` a pill
  still names its record from the chat, and opening it says the app may not
  read that kind. Its status check asks the owner to approve `full`.
  A scoped app never writes a provider's key: the callout links to the
  provider's record in the console.
- **Client**: a released build's `client_id` is the URL of a client ID
  metadata document the release publishes beside itself
  (`<release folder>/client.json`), so the approval page names the host that
  serves it; any other build is a local app with a loopback id.
- **Redirect**: a native app cannot claim an `https` redirect without a web
  host, so it listens on a loopback port for the sheet's redirect (RFC 8252
  §7.3), which the server admits on any port; the sheet is closed once the
  code arrives. PKCE `S256`, the `state`, and the `iss` of RFC 9207 are
  checked before the code is exchanged.
- **The token** is an app token: writes are recorded as `app:<host>`, it may
  expire (the approved lifetime) and has no refresh. A refusal (401) shows
  the kit's access ended screen with Sign In Again, which keeps the saved
  chats. Change Access opens the kit's review, which offers back what the
  owner left out last time, then runs the flow again and keeps the old
  access until the new one is kept; the old token is kept for revocation
  until the server revokes it. Sign Out revokes it at the revocation
  endpoint under the client id it was approved for, and a revocation the
  server does not answer 200 for is kept and tried again at each launch,
  which Settings → Account says while it waits.
- **Still set up**: at launch and on return to the foreground, at most every
  five minutes, the kit's status check; the chat list shows its banner for
  access ending soon or ended, a server too old, and a scope a newer build
  asks for. A scope the owner left out at the last approval (the session
  keeps what its request asked for) is never asked about again.
- **A server without app authorization** (no OAuth metadata) gets the
  password form, as before, so a deployment that predates it still works.
  One whose metadata lacks `full` would refuse the request with an error
  page, so it gets the kit's too-old screen instead.

### Feel and access

- Haptics on send, on a reply settling on screen, on an error.
- Springs for the send/stop morph and the growing field; rows move to the top
  animated.
- The live turn carries `.updatesFrequently`; a settled reply is announced
  once, and felt, where its chat is on screen (the list's `answered`
  signal, acted on once per raise by `PresentationModel.onSignal`); Reduce Motion stills the shimmer and the pulse; code scales with
  Dynamic Type; every control has a label; light and dark.

## Errors, by where they happen

| Where | What the person sees |
| ----- | -------------------- |
| Offline | A banner on the list; the composer keeps the text and says "Offline" |
| A read fails | The cached content stays; "Couldn't refresh: …" under the title |
| Send refused | A card with the server's words; the text back in the composer |
| Send unknown | "Checking whether your message arrived…", then the outcome |
| The stream drops | Nothing; the app attaches again with backoff |
| The turn errors | A card in the transcript: what happened, then "The server said" and its words |
| The last turn didn't finish | A notice under the transcript: what happened, why, and Continue; the list marks the chat |
| Reload after a turn fails | The run stays shown; "The conversation didn't reload", Try Again |
| A card's write fails | "That didn't go through: …"; a 409 re-reads and asks again |
| The chat was deleted | "This chat was deleted"; the draft stays to copy |
| 401 | The signed-out screen |

## Feature list

**Must (v1)**

1. Sign in through the app authorization flow (the password and an optional code on an older server); Keychain; Sign Out with revoke and the full teardown.
2. Chat list from cache, refreshed, live through the feed; titles, previews, day groups, run state, unread, attention marks.
3. Conversation from cache, current by version; prefetch of recent chats.
4. Streaming send: reasoning, tools, compaction, result; every event and exit in the tables above.
5. Attach to running chats, re-attach on drops and on returning to the foreground; background runs.
6. Stop.
7. Refused and unknown-delivery sends, with the reconcile reads, for existing and new chats.
8. The handover with no blink; reload failure with Try Again.
9. Markdown replies with code blocks and Copy.
10. Agent picker; Settings → Agents for which agents are offered and the default.
11. Voice typing with live transcription.
12. Notifications for settled replies and attention items; continued processing on iOS.
13. Questions and suggestions answered inline, CAS'd.
14. A provider key added in-app (a password sign-in; an app links to the console).
15. Rename.
16. Drafts per chat across launches.
17. Offline banner and blocked-send reasons.
18. Mac menus and shortcuts, Settings window, Return to send.
19. Glass composer, toolbar and Jump to Latest.
20. Action Button / App Shortcuts: New Chat with an assistant, optionally listening.
21. Scene restoration of the open chat.

**Polish (v1)**

22. Delta coalescing; memoized Markdown; equatable turn views.
23. Haptics; the send/stop morph; the thinking shimmer.
24. Search over titles, previews and cached transcripts.
25. Technical details.
26. Unread dot and badge.
27. Accessibility for streaming text.

**Later**

- Reply from a notification.
- Handoff of the open chat (`NSUserActivity`).
- A share extension that starts a chat with the shared text.
- Widgets; a Live Activity of its own.
- Older messages past the 500-row window.

## Testing

- Unit tests (`mise run apple:test:chat`, on the Mac; ChatKit's also on
  Linux, `mise run apps:test:linux`): the transcript fold
  and the live overlay against the console's cases (pairing by id, orphans,
  compaction, reasoning), ndjson and watch lines (heartbeats, garbage,
  terminal frames), replay dropping by set membership, the Markdown parser on
  every prefix of a document, previews, titles and day groups, the tool words
  and path encoding.
- The list's rules against a scripted server (`ChatServer`, a transport
  that holds a stream part way), through the kit's `ControllerHarness`, each
  of the seven by its number: a send streams and hands
  over without doubling; no read publishes under a live overlay; a refusal
  gives the text back and nothing runs; a gateway's answer is unknown
  delivery, never a refusal, and nothing is sent again; older rows never
  replace newer ones; a closed session publishes nothing; a window leaving
  keeps a conversation another window shows; a transcript read that failed
  is asked for again; the cache folder a build before ChatKit wrote reads
  after the upgrade (`DiskTests.swift`, over a fixture of that format), and
  its transcripts move only as a whole, the newer copy kept; a save still on
  its way across a sign-out writes nothing, and a sign-in right after one
  keeps its own folder; a refresh job from before a sign-out does nothing,
  and one the system takes back or that outlives its budget lands nothing
  (`BackgroundTests.swift`, over the kit's `ScriptedBackground`).
- A run against the dev substrate (`mise run dev`) with a model stub: sign
  in, the list, Markdown, streaming, a tool call, a question answered and the
  agent resumed, Stop, a new chat, Settings.

## What the reviews changed

- The full event and exit tables (`truncated`, `error` after the 200, a
  settled `idle`, refusal statuses, permanent attach failures, drains).
- The store's rules: UUID-keyed conversations bound by `thread`, one per
  thread, read, run and session epochs, no rows under an overlay.
- Freshness by thread `version`, never `updatedAt`; message rows on the feed.
- The feed's handoff from the list's `head` and `generation`, and its
  terminal frames.
- Unknown delivery on a new chat (adopt by the agent's newest chat), and same
  text called evidence.
- iOS notifications through a continued processing task, with the refresh
  task as a backstop and honest copy; Reply from a notification moved to
  Later; notification and unread keys on `finishedAt`, not `updatedAt`.
- The chat list reads `mode eq chat`; automated runs behind a toggle;
  prefetch cut to 8 chats of 100 rows.
- No actor header: `console` would be a false attribution.
- Glass only on the control layer; keyboard specifics; voice pitfalls;
  accessibility for streaming text; file protection and backup exclusion;
  the pending-card reads; the provider key's handling.

## What the code reviews changed

Both reviewers read the first implementation against this design, the
console and the server.

- A conversation owns its rows while a handover read it is waiting on has
  failed, and the next move of the thread retries it, so no other read lands
  under its overlay.
- The feed's batches apply one at a time; a new row restarts only the wait,
  what failed to re-read is kept, and the saved cursor moves only past rows
  whose re-reads landed.
- A conversation stays registered while any window shows it; a route
  (a shortcut, a notification, ⌘N) is taken by one window.
- Delivery checks, background polls and Stop's answers carry their run, and
  every read routes a refused token to the signed-out screen; an older thread
  row or transcript never replaces a newer one.
- A bound chat takes its agent from its thread row, read when the list does
  not hold it; a chat opened from a notification is never stuck on "Choose an
  assistant".
- The continued processing task is asked for while the app is still in
  front (inactive), as iOS requires.
- Typing dots between Send and the first event; Shift-Return at the cursor;
  New Window on the Mac; one binding per shortcut; Sign In Again after a
  refused token keeps the saved chats; a turn that ended in an error notifies
  as one; large transcripts decode off the main actor; previews lose the
  Markdown the server's one-line `lastReply` keeps.
