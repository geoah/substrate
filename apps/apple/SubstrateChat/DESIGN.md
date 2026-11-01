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
  a provider key.
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
| Dependencies  | none in the app's own code; Markdown and speech are in-tree and the system's |
| Keychain      | service `dev.reamde.substrate.chat`, `AfterFirstUnlockThisDeviceOnly` |
| macOS sandbox | network client, audio input |
| Tests         | `SubstrateChatTests`, run on macOS (the rules are platform-free) |
| Distribution  | `mise run apple:release:chat` to `<release folder>/chat/`, as Sync; listed on the internal catalogue |

`mise run apple:build:chat` builds both platforms, `apple:sim:chat` runs the
iOS app on a simulator, `apple:test:chat` runs the tests.

## Architecture

```text
SubstrateChat/Sources/
  App/       the app, scenes, commands, session lifecycle, notifications, background, App Intents
  API/       the HTTP client, the ndjson stream reader, the change-feed watch, errors, a JSON value
  Model/     records, agents, providers, threads, the transcript fold and live overlay, cards, words
  Store/     ChatStore, Conversation (one chat's run), DiskCache, Preferences
  Voice/     Dictation: SpeechAnalyzer + SpeechTranscriber over AVAudioEngine
  Markdown/  a block parser and its SwiftUI renderer, streaming-safe
  Views/     the split view, the list, the conversation, the composer, cards, settings, sign-in
```

### The API, as this app uses it

Every list is `GET /api/v1/records?first=N&filter=<json>&orderBy=<spec>[&after=<cursor>]`,
the filter one JSON object, query values percent-encoded strictly
(`+` included). A record's path is its kind's three segments and its id as
one segment, encoded exactly once: an agent's id
`x.example.com/pkg/assistant` travels as `x.example.com%2Fpkg%2Fassistant`.

| Need | Request |
| ---- | ------- |
| Server check | `GET /.well-known/substrate/server.json` (`registration.totpRequired`) |
| Sign-in metadata | `GET /.well-known/oauth-authorization-server` (RFC 8414; 404 on a server without app authorization) |
| Sign in | the browser on `GET /oauth/authorize` (code, PKCE S256, `scope`, `state`), the redirect on `http://127.0.0.1:<port>/oauth/callback`, then `POST /oauth/token` (form) → `{access_token, scope, expires_in?, repository}` |
| Sign out | `POST /oauth/revoke` (form `token`), best effort |
| Sign in, older server | `POST /login` `{repository, password, totpCode?, label}` → 201 `{token: {id}, secret, repository}`; out with `DELETE /tokens/{id}` |
| Agents | `{"kinds":["substrate.reamde.dev/core/agent"]}`, every page |
| Providers | `{"kinds":["substrate.reamde.dev/llm/provider"]}`, every page |
| Chats | `{"kinds":["…/llm/thread"],"properties":{"mode":{"eq":"chat"}}}`, `updatedAt:desc`, first 100 |
| Automated runs (when shown) | same, `mode in [manual, call, record, schedule, webhook]`, first 50 |
| Titles without a subject | `{"kinds":["…/llm/message"],"properties":{"role":{"eq":"user"},"thread":{"in":[ids]}}}`, `turn:asc,createdAt:asc`, first 500 |
| One thread | `GET /api/v1/substrate.reamde.dev/llm/thread/{id}` (404 or `deletedAt`: deleted) |
| A transcript | `{"kinds":["…/llm/message"],"properties":{"thread":{"eq":id}}}`, `turn:desc,createdAt:desc`, first 500, reversed |
| Pending questions | `{"kinds":["…/llm/interaction"],"properties":{"state":{"eq":"pending"}}}`, first 100 |
| Pending suggestions | `{"kinds":["substrate.reamde.dev/core/recordpatchrequest"],"properties":{"decision":{"eq":"proposed"}}}`, first 100 |
| Send | `POST /api/v1/substrate.reamde.dev/core/agent/{agent}/chat` `{thread, message}` → ndjson |
| Attach | `GET /api/v1/substrate.reamde.dev/llm/thread/{id}/events` → ndjson |
| Stop | `POST /api/v1/substrate.reamde.dev/llm/thread/{id}/stop` → the settled result |
| Rename | `PATCH …/llm/thread/{id}` `{properties: {subject}}` |
| Answer questions | `PATCH …/llm/interaction/{id}` `{properties: {state: answered, answers: [{question, selected: [stored option values]}]}, ifVersion}`; or `{state: dismissed}` |
| Decide a suggestion | `PATCH …/core/recordpatchrequest/{id}` `{properties: {decision: accepted\|rejected}, ifVersion}` |
| Add a model key | `PATCH …/llm/provider/{id}` `{properties: {apiKey}}` |
| Live | `GET /api/v1/changes?watch=1&kinds=…&from=&generation=` |

Resolving an interaction or a request must carry the version the owner read
(`internal/engine/write.go:650`); a `409` means it moved: the card re-reads
and asks again, never retries blind.

**The actor.** Requests send no `X-Substrate-Actor`, so they are attributed
`api`, as Sync's are. `console` would claim a door this app is not, and a
name of its own would lose the owner's annotation namespace
(`internal/engine/dataset.go:1243`). The device is named by the token's
label, "Substrate Chat on <device>".

### Streams: every event, every exit

| Event | On a send | On an attach |
| ----- | --------- | ------------ |
| `thread` | binds the conversation to the thread (below) | ignored |
| `reasoning`, `delta`, `toolStarted`, `toolFinished`, `compacted` | fold into the overlay | dropped if a stored row carries its `turn`, else folded |
| `truncated` | not sent | the replay lost events: the overlay is marked partial ("Part of this answer arrived before you opened the chat; it shows in full when it finishes") |
| `idle` | not sent | the whole answer: `running` is a turn this server does not stream (background, below); any other status ended between the row read and the attach: hand over |
| `done` | settle and hand over | same |
| `error` (after the 200) | settle; "<Agent> couldn't answer: <words>" | same |
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

`ChatStore` is one `@Observable` object on the main actor for the whole
process; every window reads it. It holds agents, providers, the chat list,
each thread's rows, the pending questions and suggestions, and the
`Conversation`s.

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
transcript never blinks. A read that fails or outlasts 15 s keeps the
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

`DiskCache` is an actor writing JSON under Application Support, one
directory per signed-in identity (server and repository, hashed). On iOS the
files are `completeUntilFirstUserAuthentication` (the Keychain item's class),
and the directory is excluded from backup: all of it is the server's and can
be read again.

| File | Holds |
| ---- | ----- |
| `state.json` | format version, agents, providers, the chat list, opening lines, pending cards, read marks, the notified set, the feed cursor |
| `messages/<thread>.json` | one thread's rows, the thread `version` they were read under, and whether the window was full |
| `drafts.json` | each chat's unsent draft |

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
  each tagged with the session epoch.
- **Bounded**: transcripts of chats not opened in 60 days, and the oldest
  past 100 MB, are dropped. Settings → Storage shows the size and clears it.
- **The live feed** runs while a scene is active (and while the Mac app
  runs): kinds thread, message, agent, provider, interaction and request. It
  resumes from the list read's `head` and `generation` (taken before the
  reads, so nothing between them is missed), reads the opening `bookmark`,
  advances its cursor after applying each row, and persists it. A `410` or a
  terminal `error` frame with code `compacted` re-lists everything and opens
  again at the new head; another terminal frame stops it with a status line.
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
- **Attention**: a new pending interaction ("Assistant has 2 questions") or
  a proposed request ("Assistant suggested a change") notifies once per
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
  runs when iOS decides; Settings says so.
- **On macOS** the stream and the feed run while the app is open, so replies
  and cards notify when they happen (unless the Mac sleeps).
- **Permission** is asked after the first message is sent. Settings has a
  switch for replies and one for questions and suggestions; automated runs
  never notify unless they ask something.
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
scroll sideways and have Copy. Only `http`, `https` and `mailto` links open.

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
  when, then the agent's name in a pill tinted by its id beside the start of
  the last reply, and a state: a pulsing dot in the pill while running, a
  dot when unread, a question mark when a card waits on the owner, an
  exclamation when the last turn ended in an error. No avatar: an initial
  on a disc took a column and said less than the name.
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
  “dentist”, found 3", "Saved a task", "Asked Researcher"), a spinner while it
  runs, a check or a cross when it settles; tap for what it found, changed or
  failed on, and the raw arguments and output.
- **Questions** (`ask`): a card with each question's options (one or many,
  as the console), Answer and Dismiss; a resolved one shows the choices.
- **Suggestions** (`propose`): a card for a create, a patch or a delete:
  the record, each property's before and after, Apply and Dismiss. Once
  decided, the card moves to the decision (below).
- **Compactions**: a divider, "Earlier messages were summarized".
- **Decisions and answers** the substrate wrote into the thread: a line in
  the tool calls' shape ("You applied the suggestion", a check or a cross);
  a decision is followed by the suggestion's card, marked Applied or
  Dismissed, showing what was written.
- **Triggered openings**: "When <record> changed", not JSON.
- **Errors**: "<Agent> couldn't answer: …"; refused and unknown-delivery cards;
  a reload failure with Try Again; "This chat was deleted".
- **A provider without a key**: a callout above the composer, "Add an API
  key for OpenAI", with a sheet that names the provider's endpoint and writes
  the key. The key lives only in the field until it is sent; it is never
  cached or logged.
- **More than 500 rows**: "Older messages aren't shown here" at the top.
- The view follows the bottom while turns arrive (`defaultScrollAnchor(.bottom)`),
  stops when the reader scrolls up, and offers Jump to Latest;
  `scrollDismissesKeyboard(.interactively)`.

### New chat

- The agent: its name and description as the header, and its pill as the
  picker in the composer; default from Settings, else the last chatted with, else the
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

- **Account**: the server, the repository, the server's version, Sign Out.
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
that opens agents, decisions and the feed to apps): the server is checked
against its discovery document and its OAuth metadata, then the system's
sign-in sheet (`ASWebAuthenticationSession`, sharing Safari's cookies) opens
the server's own sign-in and approval page. The owner chooses what the app
may do and for how long; the app never sees the password.

- **Scopes asked for**: `agents:*` (talk to the agents: chats, questions,
  turns, renames), `decide:*` (apply or dismiss what they suggest) and
  `read:substrate.reamde.dev/llm/provider` (whether an agent has a key). The
  owner may leave any out. Without `agents:` the app says it cannot talk to
  the assistants and offers to sign in again; without `decide:` a suggestion
  card links to the console; without the providers it shows no key callout.
  An app never writes a provider's key: the callout links to the provider's
  record in the console.
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
  "Your approval ended" with Sign In Again, which keeps the saved chats.
  Sign Out revokes it at the revocation endpoint.
- **A server without app authorization** (no OAuth metadata) gets the
  password form, as before, so a deployment that predates it still works.

### Feel and access

- Haptics on send, on a reply settling on screen, on an error.
- Springs for the send/stop morph and the growing field; rows move to the top
  animated.
- The live turn carries `.updatesFrequently`; a settled reply is announced
  once; Reduce Motion stills the shimmer and the pulse; code scales with
  Dynamic Type; every control has a label; light and dark.

## Errors, by where they happen

| Where | What the person sees |
| ----- | -------------------- |
| Offline | A banner on the list; the composer keeps the text and says "Offline" |
| A read fails | The cached content stays; "Couldn't refresh: …" under the title |
| Send refused | A card with the server's words; the text back in the composer |
| Send unknown | "Checking whether your message arrived…", then the outcome |
| The stream drops | Nothing; the app attaches again with backoff |
| The turn errors | "<Agent> couldn't answer: …" in the transcript |
| Reload after a turn fails | The run stays shown; "The conversation didn't reload", Try Again |
| A card's write fails | "That didn't go through: …"; a 409 re-reads and asks again |
| The chat was deleted | "This chat was deleted"; the draft stays to copy |
| 401 | The signed-out screen |

## Feature list

**Must (v1)**

1. Sign in with discovery, password and optional code; Keychain; Sign Out with revoke and the full teardown.
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
14. A provider key added in-app.
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

- Unit tests (`mise run apple:test:chat`, on the Mac): the transcript fold
  and the live overlay against the console's cases (pairing by id, orphans,
  compaction, reasoning), ndjson and watch lines (heartbeats, garbage,
  terminal frames), replay dropping by set membership, the Markdown parser on
  every prefix of a document, previews, titles and day groups, the tool words
  and path encoding.
- The store's rules against a scripted server (`ScriptedServer`, a
  `URLProtocol` the API's sessions take in tests): a send streams and hands
  over without doubling; no read publishes under a live overlay; a refusal
  gives the text back and nothing runs; a gateway's answer is unknown
  delivery, never a refusal, and nothing is sent again; older rows never
  replace newer ones; a closed session publishes nothing; a window leaving
  keeps a conversation another window shows.
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
