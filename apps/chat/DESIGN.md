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
- **Honest.** No message is dropped without a word, and nothing is sent
  again under a new id on its own. A message goes under an id the app mints
  when Send is pressed, so one whose answer was lost is sent again as it
  was and can never run twice; on a server that takes no id, a send whose
  delivery is unknown is checked before it is offered again (the console's
  rules, #1038).
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
- Reply from a notification on a server below `agents` 3. A notification
  is answered where it stands ([Notifications](#notifications)): a reply
  with a line of text, an approval with Approve or Decline, a single
  question with one of its options. The reply is a message under an id of
  its own, sent again if its answer is lost and kept on the device first,
  which is what makes a send from the background safe; a server that takes
  no id would leave a reply that expired as an unknown delivery, so there
  the notification only opens the chat.
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
  Conversation/  one chat, a child of the list: the run, what its stream has said, Stop, the message sent again under its id, the delivery check, the poll
  Model/         the transcript fold, the stream's rows and the one fold of both, the notices, the agents and threads, the tool words, Markdown blocks, the highlighter of a reply's code
  API/           the agent streams over the kit's line stream; a failure as a value: what was attempted and why
  Local/         the cache folder (ChatStorage over the kit's RepositoryFolder), the owner's choices
  Presentation/  every value the app draws: the list, a conversation's screen, its turns and cards, what stands under the last reply, Settings; the one presenter of failures; how a table is laid out and when code offers to wrap
apple/Sources/
  App/       ChatHost (the one PresentationModel; the browser sign-in, Change Access, the password door), the device services, App Intents
  Views/     the split view, the list, the conversation, the composer, cards, settings, sign-in: presentations drawn, commands sent
  Markdown/  the blocks as SwiftUI, inline spans through `AttributedString`; a code block in its colours, a table in its layouts
  Voice/     Dictation: SpeechAnalyzer + SpeechTranscriber over AVAudioEngine
```

**On the kit's runtime** (docs/plans/app-kit.md §5.2, the composition proof).
The kit's `LinkController<ChatApp>` is the root: the session and its
revocations, the status check and connectivity are the link's, and it keeps
`ChatApp` as its one child. `ChatApp` keeps each `Conversation` as a child of
its own, keyed by the id it minted for it. A step that changes the list and
a conversation publishes one tree, and the tree goes out to the views in
parts (the kit's `Controller.parts(of:)`): the link's own screens, the
app's own (the list, each window, Settings, the agents) and each
conversation's screen under its key. A view reads the part it draws
(`ChatHost.link`, `chats`, `conversation(_:)`) and sends commands down a
path of keys, so a piece of a reply draws its conversation again and
neither the list, another conversation nor Settings.

**The presenter is made of parts**, each kept until what it reads changes
(`PresenterParts`): the list's rows under their days, each window's search,
each conversation's screen, its stored rows' turns, each live turn, each
tool's row, Settings. What a part reads is the list's state with the
fields it never reads emptied (`ChatAppReads.swift`), so a new field is
read by every part until it is listed there, and the test rig makes every
kept part again and compares, which stops the process on a part kept past a
change to what it reads. The list reads the instant ("now", a time, a
weekday), so it is kept until the instant one of its rows stops being true.
A streamed delta makes its conversation's screen and the turn being written
again, and nothing else; a key typed makes nothing.

| What | Where it lives now |
| ---- | ------------------ |
| A turn's stream | `Conversation`'s `.sequence` effect under the key `run`: a send, an attach (its read, then the events), the handover read |
| A turn | one fold of the thread's stored rows and what the stream has said (`TurnFold` over `LiveRows`), keyed by the row's `turn` and a call's id |
| Streamed text | `Conversation.coalescing(of:)`: a delta or a piece of reasoning presents within 50 ms, structure at once; the presenter then draws it at a steady rate (`TextReveal`) |
| A draft | the composer's field while it is typed; the conversation after a pause, before a send and when the field stops editing (`DraftReports`), and from there `drafts.json`; the tree only as the engine wrote it |
| The watch | the kit's `WatchFollower`, its cursor and the rows still to read again in the kit's `RecordCache` (`records/watch.json`) |
| The cache | `ChatStorage` over the kit's `RepositoryFolder`, at the path every build used (`Application Support/SubstrateChat/<hash>/`): `state.json` and `drafts.json` adopted at the top, transcripts moved from `messages/` into the cache area, bounded by age and budget; a session reaches it only through the `ChatStorage.Lease` its open answered |
| Notifications | the kit's `Notifications` device service (`SystemNotifications` on an Apple device): scheduled (a reply five seconds ahead where the server keeps marks, at once otherwise), cancelled and badged as effects; the kit's delegate, installed at launch, hands a tap to `responses()`, which the list follows and turns into a route; the engine never notifies a chat on screen |
| Read state | the person's `llm/threadmark` records where the server keeps them (`ChatAppMarks.swift`, [Read state](#read-state)); the device's own `seen` map on a server that does not |
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
| Chats | `{"kinds":["…/llm/thread"],"properties":{"mode":{"eq":"chat"}}}`, `updatedAt:desc`, first 100; the next hundred with the page's `cursor` as `after`, when the list is scrolled to its end ([The list's window](#the-lists-window)) |
| Chats, where `records` is at revision 9 or more | the same with `"referencedBy":{"kind":"…/llm/threadmark","property":"thread","properties":{"readBy":{"eq":"me"},"archived":{"eq":true}},"exists":false}`: the chats the person has not archived. Beside it, each read whole: the pinned ones (the arm with `"pinned":{"eq":true}` and no `exists`) and the ones that wait on a person (`"waitingOnPerson":{"gt":0}`, no arm). The archived ones (the arm with `archived` and no `exists`), a page at a time, while a window lists them ([Pinned and archived chats](#pinned-and-archived-chats)) |
| Automated runs (when shown) | same, `mode in [manual, call, record, schedule, webhook]`, first 50 |
| Titles without a subject | `{"kinds":["…/llm/message"],"properties":{"role":{"eq":"user"},"thread":{"in":[ids]}}}`, `turn:asc,createdAt:asc`, first 500 |
| One thread | `GET /api/v1/substrate.reamde.dev/llm/thread/{id}` (404 or `deletedAt`: deleted) |
| A transcript | `{"kinds":["…/llm/message"],"properties":{"thread":{"eq":id}}}`, `turn:desc,createdAt:desc`, first 500, reversed |
| The rows before the ones held | the same with `"turn":{"lte":<the first held row's turn>}`, first 200: a keyset of the transcript's own order, so it needs no cursor from the read that brought the rows held (the folder's, a conversation's own). The row at that ordinal comes again and is dropped by its id |
| A search's messages | `GET /api/v1/records?q=<words>&mode=lexical&first=50&filter={"kinds":["…/llm/message"]}`: the ranked read, by its words alone, so every hit holds a word to show. Full access only: an app's token reads messages thread by thread and is answered `403`, after which the app searches what the device holds and asks no more until the server's document is read again |
| Pin, Unpin, Archive, Unarchive | the mark's `POST` with `{thread, pinned}` or `{thread, archived}`, alone or beside `readThrough` in one write; only where `records` is at revision 9 or more. An older server refuses the properties (`422`, or `403` to an app's token) |
| My marks | `{"kinds":["…/llm/threadmark"],"properties":{"thread":{"in":[ids]},"readBy":{"eq":"me"}}}`, first 100, a hundred threads a request, with every list of threads; only where `records` is at revision 8 or more. `me` is the person the token acts for, so the app never learns its actor |
| Read a chat | `POST /api/v1/records` `{kind: …/llm/threadmark, properties: {thread, readThrough}}`, with no id: the server builds it from the thread and the person. `201` or `200` with the mark as it stands. Never `readAt`, which is the server's |
| Mark as Unread | the same `POST` with `{thread, markedUnread: true}` and no `readThrough`: the position never moves |
| Pending questions | `{"kinds":["…/llm/interaction"],"properties":{"state":{"eq":"pending"}}}`, first 100. Where the server keeps marks it also counts what waits on each thread's row (`waitingOnPerson`), and this read and the two below name only the threads that say something waits (`"thread":{"in":[ids]}`), so with none waiting none of the three is made |
| Pending suggestions | `{"kinds":["substrate.reamde.dev/core/recordpatchrequest"],"properties":{"decision":{"eq":"proposed"}}}`, first 100 |
| Pending approvals | `{"kinds":["…/llm/approval"],"properties":{"state":{"eq":"pending"}}}`, first 100; a refusal (a server older than the kind) reads as none, and the watch names the kind only once this read was answered |
| Send | `POST /api/v1/substrate.reamde.dev/core/agent/{agent}/chat` `{thread, message, messageId}` → ndjson. `messageId` is a lowercase UUID the app mints when Send is pressed, the id of the message's own record, sent only where `agents` is at revision 3 or more: an older server refuses a body field it does not know (`400`). The same body again writes nothing and runs nothing: its stream opens with the same `thread` event, then is the turn's, or one `idle` line |
| A message kept from a launch before | `GET /api/v1/substrate.reamde.dev/llm/message/{messageId}` (404: no copy was written) |
| Attach | `GET /api/v1/substrate.reamde.dev/llm/thread/{id}/events` → ndjson |
| Stop | `POST /api/v1/substrate.reamde.dev/llm/thread/{id}/stop` → the settled result |
| What the chat may call | `GET /.well-known/substrate/server.json`, with each list refresh: the `agents` feature's `revision` (3 or more takes a `messageId` and has the continue route) and the `records` feature's (8 or more has the person's marks, 9 or more their pins and archives and the `referencedBy` arm) |
| Try Again, Continue | `POST /api/v1/substrate.reamde.dev/llm/thread/{id}/continue` `{}` → ndjson: the thread run again with no new message, called only where `agents` is at revision 3 or more; below it, or before the document is read, the app sends "Continue where you left off." through Send, as it did before the route. The route is never probed. `409` `thread_busy` or `nothing_to_continue`: the thread is read for what it holds |
| Rename | `PATCH …/llm/thread/{id}` `{properties: {subject}}` |
| Answer questions | `PATCH …/llm/interaction/{id}` `{properties: {state: answered, answers: [{question, selected: [stored option values]}]}, ifVersion}`; or `{state: dismissed}` |
| Answer questions in words (`agents` 7) | the send, with `repliesTo: <interaction id>` beside `thread` and `messageId`; a server below 7 refuses the field (`400`), so it is sent only where the document says 7 |
| Whose resolved cards are the person's (`agents` 7) | `{"kinds":["…/llm/interaction"],"properties":{"thread":{"eq":"<thread>"},"resolvedBy":{"eq":"me"}}}`, first 100; the same for `…/llm/approval` and `core/recordpatchrequest` with `decidedBy` |
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
| `reasoning`, `delta`, `toolStarted`, `toolFinished`, `compacted` | fold into the stream's rows (`LiveRows`) | dropped if a row read with this attach carries its `turn`, else folded: what is held of the turn is written over, never emptied |
| `truncated` | not sent | the replay lost events: what is held stays, and the chat says so ("Part of this answer arrived before you opened the chat; it shows in full when it finishes") |
| `lagged` | not sent | this reader fell behind and the server ends its stream: nothing failed, so the app attaches again at once and draws nothing for it |
| `messageQueued` (`agents` 6) | a message was written into the running turn, from this device or another: its row is drawn at its ordinal, and it is queued until a call reads it ([a message sent while the assistant works](#a-message-sent-while-the-assistant-works)) | same; one a reply the rows already hold came after is read, not queued |
| `messageTaken` (`agents` 6) | a model call read the message: it is queued no more, and is never said to be again | same |
| `idle` | after `thread`, to a repeat of a message the server already holds: the thread's status is the whole answer, read as on an attach | the whole answer: `running` is a turn this server does not stream (background, below); any other status ended between the row read and the attach: hand over |
| `done` | settle and hand over | same |
| `error` (after the 200) | a failed turn: settle, with the `cause` and `retryAfter` the event carries where the server sends them. With no `cause` and a `code`: `auth` is the sign-in ended (the signed-out screen), `internal` a failed turn, `unavailable` a reader cut off for reading too slowly (the turn runs on: attach again, nothing drawn) | same |
| `{}` | heartbeat: every 10 s where `agents` is at revision 5 or more, every 30 s below it. It only says the stream is open | same |

| Exit | What follows |
| ---- | ------------ |
| 4xx with the server's envelope before `thread` | refused: 409 a turn already running or the repository's 4 at once, 422 a hidden agent, 404 no such agent. Text back in the composer, and the strip above it says so (the envelope's `reason` is read where it has one) |
| No connection, or a server that cannot be reached | the request never left: text back in the composer, Send Again, no check |
| 5xx, 503 shutdown, a gateway's answer, a dropped connection, a timeout, a stream that ends before any event | the answer was lost (below): under its id the message is sent again; without one, unknown delivery |
| Stream ends after `thread`, no `done` | the turn runs on: attach again, backing off 0, 1, 2, 4, 8, 15 s; after `lagged`, at once |
| Nothing arrives, not a heartbeat, for 25 s (75 s below revision 5) | the connection is gone: the request times out and the app attaches again; the composer says "Reconnecting…" until a stream opens |
| Attach answers 4xx other than 409/429 | permanent (404 a deleted thread): settle, say so in the strip above the composer, release the composer |
| 401 anywhere | the session ended: one screen, Sign In Again |

**One turn, one fold.** A turn has one model from its first token to its
stored row. A fold here is the function that makes the list of turns: the
console's makes it from a thread's rows, and this one makes it from the
rows and the stream together. The stream's events become provisional rows
(`LiveRows`), each keyed as its stored row will be: an assistant row by the
`turn` its `delta` and `reasoning` events carry, a tool row by its call's
id, and the message you sent by its id (`messageId`): the bubble is drawn
under that id from the press, its row has it, and the two are paired by it
alone, whatever the words. Only a message sent under no id (a server below
`agents` 3, a server whose document is not read yet, and Continue's own
words there) is found by its words, and by where it stands: a message with
no id starts its run or is refused, so its row is the last of its words
before the first row its own stream names of that run, with no reply
between the two. An older row of the same words has its own reply after
it and is never taken for it. Until the stream has named a row, the
message is paired by what the device knew when it was sent (past the
newest row held then, where those rows were the server's word to this
launch and not the folder's from a launch before; else by when the row
was written), which is a guess: it is drawn so that nothing stands twice,
and a conversation's own read gives the row no id for good while the run
goes on. The transcript is one list made
from the stored rows and the provisional ones (`TurnFold`): a stored row
takes the place of the provisional row with its key and keeps the id that
row was drawn by, so every view over it (an open Thinking section, an
expanded tool row, a selection) is the same view before and after. What the stream said and the rows do not hold yet (a call
settled past the row read, text past it) is drawn on the row until they do.
Nothing is emptied to be filled again: not when the rows land, and not when
the stream is joined again. What a run drew and no read of the
conversation's own has paired with its rows yet (a reload that failed, a
run a sign-in's end cut off) stays held through the next send: the fold
draws it once with the rows whoever read them, and under the id it has.

**A stream joined again** (an attach: on a return to the front, after a
drop, after `lagged`) replays the turn from its start. Where `agents` is at
revision 5 or more, `delta` and `reasoning` carry `at`, the Unicode code
points of that text before the piece, and each piece is written where it
belongs (`TextRuns`): over the same text, so the reply stands as it was and
goes on from its end. A replay that lost its middle leaves a gap, which a
later replay fills. Below revision 5 there are no positions: a pass counts
its own pieces from the turn's start, the longer text is kept and the chat
never shows less than it did. A stored row carries the whole text, so a
turn's row always wins over what was streamed of it.

**Background runs.** `idle` with `running` means this server process has no
stream for the turn yet (a trigger, a schedule, a turn another replica
owns). The chat says "<Agent> is working in the background" and Stop is not
offered. Below revision 5, and for an automated run at any revision, the
thread is read every 5 s (and on every feed row for a thread or a message)
until its status settles; then the rows are read. From revision 5 a chat's
run that was resumed streams like any other, so there the 5 s timer asks
for the stream again instead of reading the thread (an attach, with the
row read every attach makes), and the chat streams from the first event
one brings. A feed row still reads the thread.

**Stop** posts `stop`. A `409` means nothing streamable runs: the thread is
read again, and what it says is shown. Any other failure is the stop's, not
the reply's: the strip above the composer says the stop failed and that the
reply is still being written, with Try Again for the stop.

### A message sent while the assistant works

A server at `agents` 6 takes a message sent under its id into the chat
turn running on its thread (decision 0318): it writes the row at once,
answers with that turn's stream from its start, and says `messageQueued`
on every stream of the turn, then `messageTaken` when a model call reads
the message. Where the server says that revision, takes ids, and the agent
is a chat's, the composer sends while a reply streams (`Conversation.queues`,
`ConversationQueue.swift`):

- **The send takes over the read.** The stream being read is let go and the
  send's own is read in its place: the same turn, said again from its
  start, folded over what is drawn as a stream joined again is. Nothing is
  drawn twice and nothing drawn goes.
- **Queued, and then not.** The message is drawn from the press, under its
  id, as every message is. Once the server says it wrote it
  (`messageQueued`) it says "Queued for Moss's next step" under the bubble,
  until `messageTaken` names its id. A piece of a reply whose ordinal is
  past the message's, and a reply's stored row past it, say the same: a
  call read it. The composer says how many are queued and what a stop does
  to them.
- **Another device's message** arrives as a `messageQueued` with its id,
  ordinal and text, and is drawn the same way. Several keep the order of
  their ordinals, and a call of the reply being written stays with that
  reply, above them.
- **The run ends on its own** (`ok`, `overbudget`) with a message unread:
  the server starts the turn that reads it. Nothing is queued from that
  word on. Once the rows are read, a thread that says it is running is
  attached to, and that turn's stream opens with `messageTaken`; one that
  does not run has the message in its rows with no reply after it, and the
  card at the end of the chat says the assistant didn't answer, with
  Continue.
- **The run is stopped or fails** with a message unread: nothing starts.
  The card at that turn says so, and that the message was not read ("It
  hadn't read your last message. Continue has it read."), with Continue,
  which writes no message. A message is never shown queued after its run
  ended.
- **Stop with a message queued** stops the turn; the composer said before
  the press that the message stays unread until Continue.
- **A send the server refuses** gives the text back to the composer and the
  strip above it says why; the reply goes on, read again from an attach.
  One whose answer is lost is sent again under its id, and the repeat is
  answered with the turn's stream, or with `idle` once the turn is over.
- **A stream said again says how the turn stood, not how it stands.** An
  attach, a send into a running turn and a send's repeat are answered with
  the turn from its start, and nothing on the wire says where that ends
  and the present begins: a message a call read long ago is first said
  queued. So what such a stream says is queued is kept apart
  (`ConversationState.recalled`) and drawn as the ordinary message its row
  is, until the stream is known to have caught up (`Conversation.catchUp`):
  it says the message this send carried was written, a heartbeat arrives
  (sent only into a stream gone quiet), or the run says how it ended. A
  send that started its own turn is answered with that turn as it
  happens, and believed from its first event. So a chat that joins a run
  anew (a launch, a sign-in again) never draws a message as waiting that a
  call has read, also where its stream is cut between the two words; and
  a message another device queued is drawn as waiting from the next
  heartbeat at the latest, ten seconds.
- **A turn this conversation was not reading.** A message sent into a
  thread may meet a turn already running there. Its rows so far were drawn
  from the rows, under their own ids, so the send's replay of those
  ordinals is dropped (`stale`) and only what the rows do not hold is
  folded.

Below `agents` 6 none of this happens: the composer keeps the text while a
reply is written, and a send goes when the reply ends.

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
| 3 | `TurnFold.fold` in the presenter and `TurnFold.settle` at each of the conversation's own reads: a stored row takes its provisional row's place under the same id. The conversation's ownership still reaches the list with each of its events, and `load` starts no second read of an owned thread | `rule3_rowsReadUnderAStreamTakeTheirTurnsPlaceAndDoubleNothing`, `rule3_rowsThatLandUnderAStreamAreDrawnOnceWithWhatItHasSaid`, and `FoldTests` |
| 4 | `ChatAppState.readEpochs`, carried by every read, cached read and prefetch, and moved in `ChatAppEngine.reported` by a binding, a send or an attach (the conversation takes the thread) and a settle (it hands it back), each of which also cancels the read under `read:<thread>`; a read that fails clears what is outstanding, so the next asks again | `rule4_aReadIsDroppedWhenItsThreadsEpochMoved`, `rule4_aCachedReadIsDroppedWhenItsThreadsEpochMoved` |
| 5 | the runtime's key epochs: a new run restarts `run` and cancels `retry`, `poll`, `pollTimer`, `stop`, `stopWait`, `check` and `repeat`, so a late result is dropped | `rule5_aNewRunDropsWhatAnOlderRunStillDelivers` |
| 6 | the runtime's controller generation: `ChatApp.resetting(of:on:)` starts the list over when the link's repository changes or is gone, closing every conversation; the folder's lease is retired, so nothing still on its way reaches the folder, the link generation in it moves, the rest of it is deleted on Sign Out, and the notifications are removed; the next sign-in's open runs after all of it | `rule6_aSignOutDropsEverythingStillOnItsWay`, `aSaveStillOnItsWayAcrossASignOutWritesNothing` |
| 7 | `ChatAppState.isCurrent` | `rule7_cachedRowsAreCurrentOnlyWhileTheirThreadIsSettledAndUnmoved` |

1. **A conversation is keyed by a client UUID**, never by an agent. A new
   chat has a UUID and no thread; the `thread` event binds it, atomically
   moving its draft, its stream's rows and selection, and from then the store finds it
   by thread id. There is exactly one `Conversation` per thread in the
   process: two windows on one chat share it, its stream and its composer
   state; two New Chat windows have two UUIDs.
2. **One stream per conversation.** The app's own send is never attached to;
   opening a chat the store already streams opens nothing.
3. **A turn is drawn once, whoever read its rows.** Rows may land under a
   stream from any read (the open-time read, the feed's, a prefetch, the
   conversation's own): the fold pairs each with what the stream said of
   the same turn, so nothing is doubled and nothing is lost. This replaces
   "no rows under a live overlay", which kept reads out because rows and a
   stream had no key in common. A conversation that sends or streams still
   owns its thread, so the list starts no second read of it.
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

**The handover.** On `done`, the rows are read under a new epoch. When they
land, each takes its provisional row's place in the fold, in the same step
of one tree, and the conversation lets go of what the rows now hold
(`aSendStreamsThenHandsOverInOneStepWithoutDoubling`). It is no swap of one
model for another, so no height changes and no row is made again
(`HandoverTests`, frame by frame). A read that fails or outlasts 15 s keeps
what the stream drew, releases the composer and offers Try Again.

**A message under its own id** (`agents` at revision 3 or more, decision
0312). The id is minted once, when Send is pressed, and every copy of the
message goes under it. The bubble carries it from the start and the stored
row has the same id, so the handover replaces nothing.

- **Kept before it leaves.** The message, its id, its assistant and its
  thread are written to `unsent.json` before the first request goes: the
  conversation's send waits for the list's write to answer. A kill at any
  moment after leaves the id to send it again under. The emptied composer
  is written in the same step, on any server, so a kill never brings a
  sent message back as a draft, and a message put back into the composer
  (a refusal, Edit Message, a chat that is gone) is written as promptly,
  since it is in no other place. A text moves between the two files both
  ways (a send out of the drafts, Edit Message and a refusal back into
  them), so the unsent messages are written first with none taken away,
  then the drafts, then the unsent messages without the ones that went: a
  kill between any two leaves the text in both and never in neither. The
  next launch gives it one owner: a draft that is the message and no more
  was on its way out, and is the unsent message; a draft that is the
  message with what was typed since behind it was on its way back, and is
  the draft, with nothing left standing beside it. The unsent messages
  keep an order of their own beside the drafts': a write of the drafts
  alone that was asked for later and landed first does not keep a message
  out of the folder. Each order's mark moves only once its file is
  written, so a later write that failed never lets an earlier one take a
  message out of the unsent ones on account of drafts that are not there.
  A message put back into the composer is in no other place, so it is
  said beside the draft (`draftGiven`), and a field that keeps its own
  text over the rewrite (what it held was typed over the text the rewrite
  stands in place of) takes the message all the same, above its own.
- **A folder that cannot be written** (a full disk, or one that could not
  be opened at sign-in) does not hold a message back: it goes, the drafts
  file is left as it was, and the message is kept only while the app
  runs. The person is told, in the
  strip above the composer: "Your message is on its way, but it isn't
  saved on this device yet." with "Keep Substrate Chat open until it shows
  as sent." under it and the device's own words under Details (for a
  message that stands unsent: "Your message isn't saved on this device
  yet."). There is nothing to press: the list writes again after 2, 5 and
  15 seconds and then every half minute, and with every change to the
  unsent messages. The strip goes when a write lands or the server has
  the message. A folder that never opened is not opened again by these
  writes: its strip stays until the message is the server's.
- **An answer that never came** (a 5xx, a gateway's page, a dropped
  connection, a timeout, a stream that ended before `thread`, the app
  suspended mid-request) leaves it unknown whether the message arrived, and
  it does not matter: the same request goes again. The app repeats it on
  its own three times at most, one, three and eight seconds after each
  answer that did not come (the console's waits), wherever the app runs,
  and never later than two minutes after the press, so a phone that slept
  sends nothing late. Coming back to the front sends it again at once
  within the same bounds. While it waits the bubble stays, the composer
  says "Waiting for the answer", and nothing is drawn as a failure.
- **Then it stands.** Past those bounds the message stays at its place in
  the chat, dimmed, with a card under it: "No answer came back for your
  message.", why as the note, and Send Again, which sends the same request
  once more under the same id. The pencil beside the words takes the text
  back into the composer, ahead of anything typed since; what is sent from
  there is a new message, by the person's own word. While a message stands
  the composer sends nothing else ("Send or edit the message above
  first"), and New Chat leads back to a new chat whose first message
  stands.
- **A refusal.** The server's own refusal of the only copy sent, or a
  request that never left the device, proves nothing was written: the text
  goes back into the composer and the strip above it says why, as on a
  server without ids, and the id is done with. A refusal of a later copy
  proves nothing about the earlier ones (a copy still on its way can be
  written after another was refused), so the message keeps its id and
  stands with the refusal's sentence and Send Again. A busy thread is also
  attached to: the turn on it may be this message's own.
- **An id that names another record** (`409` with the reason
  `message_id_taken`) cannot happen for an id the app minted. It is said
  as the app's own fault, with the server's words under Details and Edit
  Message as its button. No id is ever minted and sent on the app's own.
  A server that names no reason for it is read by its words, for the
  field's name, in `FailurePresenter.takenID` alone, to delete with the
  last such server.
- **The rows decide.** Whenever the thread's rows hold a record with the
  pending id, the message is that row and is pending no more. A run that
  left a message standing reads no rows of its own, so the list reads the
  thread once it is handed back. A new chat's first copy may have opened a
  thread whose answer was lost: as soon as the rows of any thread the list
  reads hold the new chat's pending id, the new chat is that thread, and
  where another conversation already has it (the person opened it from the
  list) the new chat gives way to that one, so no thread has two
  conversations and no message two bubbles. The same holds when the new
  chat's own repeat is answered first: where the conversation on that
  thread is in the middle of something (a send out, a message standing, a
  failure unread, a draft the folder keeps for the thread), it stays and
  the new chat gives way; only one that holds nothing is closed for the
  new chat, as rule 1 has it. Where no conversation is on that thread and
  the folder keeps a draft for it, written there before, the new chat's
  composer takes that draft under what was typed in it, as Edit Message
  puts a message above what was typed since: both texts stay, in the
  composer and in the folder, and neither is dropped, also where the
  field holds other text than the conversation last heard.
- **After a kill.** At the next launch the message is back at its place
  (a new chat's with the next New Chat) and is read for by its id. Found,
  it is the stored row: the chat is bound to its thread and read, and
  nothing is sent. Not found, it stands with "Substrate Chat closed while
  it was being sent." and Send Again. It is never sent again without a
  press, and never dropped.
- **Stop** before the server answered stops the sending: the message
  stands, and a turn a copy may have started is stopped where its thread
  is known.
- **A server that goes back.** Where the document said revision 3 and the
  server refuses the field (`400`, `unknown field "messageId"`), nothing
  was written: the message goes once more without an id, and so do the
  next ones until the document is read again.

**Unknown delivery**, on a server that takes no `messageId` (`agents`
below revision 3, or its document unread). The app waits a second and
reads.

- On an existing chat: the thread fresh, and a user row past the newest turn
  held before the send (unknown if the last reload failed) with the same
  text. Found: a running turn is attached to. Not found: the text goes back
  into the composer and the strip above it says "Your message may not have
  arrived", with Send Again. Same text is evidence, not proof.
- On a new chat: the agent's newest `chat` threads (`startedAt:desc`,
  first 5); one started after the send whose first user row is the text is
  adopted. Otherwise the same strip, and the text is back in the composer.
- While it reads, the strip says "Checking whether your message arrived…"
  and the composer waits.
- No message is ever sent again on its own there: without an id a repeat
  could run twice. Coming back to the front and Stop leave a send the
  server has not answered alone, since an attach in its place would end it
  and drop what was typed.

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
| `state.json` | format version, agents, providers, the chat list's first page (the newest hundred, however far the list was scrolled), the pinned chats and the ones that wait on a person, the `records` revision the server last said (which lists a launch reads before it asks again), opening lines, pending cards, the person's marks as the server last said them and the ones this device moved and has not had answered, the device's own `seen` map (what is unread on a server without marks), the notified set; a build before ChatKit kept the feed cursor here too |
| `cache/<thread>.json` | one thread's newest rows (the read's window, never the older ones read in above it), the thread `version` they were read under, and whether the window was full (in `messages/` before ChatKit; moved at the first open, the newer copy kept where both areas hold one, and `messages/` removed only once every file in it is moved) |
| `records/watch.json` | the watch's cursor and the rows it marked to read again (the kit's `RecordCache`); the cursor `state.json` kept before ChatKit moves into it at the first open, which removes it from `state.json`, so it is read once |
| `drafts.json` | each chat's unsent draft |
| `unsent.json` | each chat's message sent under an id of its own that the server has not taken: the id, the text, the assistant, the thread. Written before the request leaves and before the draft is cleared, so a kill between the two leaves the message in both files, and the next launch gives it one owner |
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
  runs): kinds thread, message, agent, provider, interaction and request,
  and the thread mark once a read of the marks was answered (a watch naming
  a kind the server does not declare is refused whole). It
  resumes from the list read's `head` and `generation` (taken before the
  reads, so nothing between them is missed), reads the opening `bookmark`,
  advances its cursor past a row once the engine has taken the row
  (`ChatAppAsk.noteRow`), and persists it: a watch that is replaced has
  what it says dropped, and a cursor moved first would leave a row nobody
  took on no stream again. The watch is opened again, from its cursor, to
  follow one more kind once a read of that kind was answered; the kind is
  then read once more when the watch that follows it is open, since the
  cursor moved on with the other kinds past whatever a record of this one
  did meanwhile (`ChatApp.widen`). A card is kept from the moment its
  record is asked for, so a row that says the record moved while that read
  is out reads it again. A `410` or a
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
  where the rules allow; a mark's row re-reads the list and the marks
  together; a message row re-reads any background-running chat
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
  notifies. A reply that failed says the sentence its card in the chat says,
  never the server's own text. Where the server keeps marks, a reply a mark
  says was read (on any device) is not notified at all, and the rest wait
  five seconds for a mark: scheduled that far ahead, and taken back if the
  mark arrives first, so a reply read live on the Mac does not sound on the
  phone beside it. The refresh job reads the marks with the list, so it
  never posts for a reply read elsewhere.
- **Attention**: a new pending interaction ("Assistant has 2 questions"),
  a proposed request ("Assistant suggested a change") or a pending approval
  ("Secretary wants your approval to run gmail_send") notifies once per
  record and opens the chat that asked.
- **A turn that ends on a card is one notification**, the card's, which is
  the one that needs the person, with what the reply said above it where
  that is 160 characters or fewer (`ChatAppAttention.swift`). The two are
  read apart, the card by the pending lists and the reply by its thread's
  row, so either may come first. A card read while its turn still runs is
  held until the row settles, three seconds at most (`ChatApp.cardHold`),
  and is then posted with the reply; the reply is not notified on its own.
  A row read first schedules the reply's notification as any reply's, and
  the card that follows takes it back and says it. A card stands for the
  turn that asked and no later one: a reply to a message sent while an
  older card still waits notifies as any reply does. A background refresh
  holds nothing, since it is over before a hold would be. On a server
  without marks a reply's notification is shown at once, so there a row
  read first is shown and then replaced.
- **Answered where it stands.** A reply's notification takes a line of
  text (Reply), where the server takes a message under an id of its own
  (`agents` 3 or more). An approval's takes Approve and Decline, and shows
  the tool and every argument whole; a call longer than a notification
  shows (320 characters) offers only Open Chat, and says to read it there.
  A single question that takes one of three options or fewer, each short
  enough to read on a button, takes its options; any other question opens
  the chat. Each action goes through the path the chat uses: the reply is
  its thread's conversation sending a message under a fresh id, kept in
  `unsent.json` before the request leaves and sent again under that id if
  its answer is lost; a card's is the write that resolves it, over the
  version the notification was made from, which rides in its `userInfo`,
  so a record that changed since is refused by the server and never
  decided unseen. The same response heard twice acts once.
- **What an action waits for.** A launch for an action may hold nothing
  yet: an action waits, inside its time, for the folder to be read, a
  reply also for the server's document (which says whether a message
  takes an id) and for its chat's row, which names the agent it is sent
  to. The folder keeps the list's first page alone, and an archived chat
  is in no list, so that row is read by name where the launch holds none.
- **Inside the time a background action gets.** An action has 20 seconds
  (`ChatApp.actionSeconds`). Done, its notification is taken back. Not
  done, a new notification says so in the sentence the chat would say it
  in, and opening it opens the chat: a reply's text stands there as a
  message with Send Again (or is the chat's draft, where the send could
  not start in time or was refused), and a card still waits. Only then is
  the device told the action is over (`Notifications.finished`), since the
  system may suspend the app at that word: the effect that tells it writes
  the drafts to the folder first, so a text that went back into them is
  kept before the app may stop. The kit's delegate answers the system on
  the main thread then, or after 25 seconds whatever the app says. Where
  the folder itself was not read in that time (the action launched the
  app), there is nowhere to write: the notification says the reply whole
  ("Your reply was not sent or saved. You wrote: …"), and the text becomes
  the chat's draft when the folder is read.
- **Which chats are notified for** is asked of one predicate,
  `ChatAppState.notifies(_:row:waits:)`: a chat its person archived is
  silent, but while something in it waits on a person
  ([the list's rules](#chat-list)). A reply's notification asks it, a
  card's (a card is itself what waits), what an action could not do, the
  background refresh, and whatever takes a notification back.
- **What stands is kept by thread** (`announced`): a reply's, a card's and
  a failed action's alike. Each list read reads those threads by name
  with their marks, so a notification is taken back when its chat is
  archived or read on another device. A read answers a reply's
  notification alone. A card's stands while its chat may still wait (the
  row's count, or a card of it among the pending ones) and goes once
  nothing does; what an action could not do stands until its chat is
  opened here, since the text it kept is on this device. A reply's own, in
  a chat its person archived, was said because the chat waited: it goes by
  the row's count alone, the moment that says nothing waits, whatever
  keeps the other two.
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
  pending (each one's id starts with the thread's). So does a mark that
  says the chat was read, wherever it was read: from the feed, a refresh or
  the refresh job.
- **Permission** (alerts, sound and the badge) is asked once the first
  reply has settled, never while it streams, and in the app's words first:
  a card above the composer says what would be notified ("Substrate Chat
  can notify you when an assistant finishes a reply or needs an answer from
  you while you are in another chat or another app."), Turn On
  Notifications brings the system's prompt, and Not Now asks nothing. A
  device the system already asked is not offered. Settings has a switch for
  replies and one for questions and suggestions, and Turn On Notifications
  while the system was never asked; automated runs never notify unless they
  ask something.
- **Unread**: [Read state](#read-state). The badge is the number of unread
  chats by the same rule as the dots.

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

Replies are Markdown. A block parser in ChatKit (paragraphs, headings,
nested ordered and unordered lists, task items, fenced and indented code,
quotes, tables, rules) reads a reply into blocks, and the views draw each
block as SwiftUI and its inline spans through `AttributedString(markdown:)`
with `inlineOnlyPreservingWhitespace`. Text past 200,000 characters is
plain.

- **The presenter parses.** An assistant turn's presentation carries its
  blocks as a keyed list of rows (`AssistantTurn.blocks`), made where the
  presentation is made, on the runtime's executor and never the main
  thread. A row's id is its block's position and a hash of what it holds
  (FNV-1a, the same on every device), so a finished block is the same row
  at every flush after it and only the last, still being written, is
  another. A reply's rows are kept for its text: a piece of reasoning or a
  tool's event leaves them as they were. A stored turn's are made once per
  read.
- **One parse at a time, at most 30 a second.** A reply's pieces share a
  50 ms window (`Conversation.textWindow`), so its text is presented, and
  so parsed, once in each however fast they arrive: 20 times a second. The
  presenter runs one at a time, so the pieces that arrive during a parse
  wait and are parsed together, and no presentation follows one made of
  newer text. Something else can have the tree presented between two
  windows (a tool's event, a search in another window), so the limit is
  also held where the parse is: a reply being written is parsed no sooner
  than a thirtieth of a second after its last parse
  (`Markdown.parsesPerSecond`). Presented sooner, its rows stay as they
  were, and the tree names the instant it must be presented again
  (`ChatPresentation.redrawAt`, the runtime's tick), so nothing written is
  left undrawn. A reply that ends is parsed at once.
- **A reply being written is closed for display**
  (`Markdown.parse(writing:)`), so the page only ever grows: an open
  emphasis, strikethrough or code span is closed where the text ends; a
  marker with nothing in it yet, or that may be half of a longer one, is
  left out until more arrives; a link reads as its label until its address
  is whole; a table is a header row from its first cell, the line under it
  written for it; a task item's box is drawn once its brackets are there;
  a line that is only the start of a block (a bullet's mark, one or two
  dashes, part of a fence's closing line, a line of code that has only
  begun) waits. Inside code nothing is touched, and whether the text ends
  inside code, a list item or a quote is the parser's to say: the text is
  parsed as it stands first; where it ends in code that is the answer;
  otherwise its last line is put right as text where a block's start is at
  stake and parsed again only if that changed it, and what is open inline
  is closed in the one block still being written. A finished reply is
  parsed as it stands, so what a stream drew at its end is what its stored
  row draws. Two things are left as they are: a table is a header row
  early only where its first line stands at the text's start, under a
  blank line or under a heading, so one inside a quote or a list item reads
  as text until its second line arrives; and a marker its writer never
  closed (in a heading, a table's cell or a paragraph that has ended) is
  drawn closed while it is the last thing written and as written after.
- **A long reply is several rows of the transcript.** Past about a screen
  and a half of text (`Markdown.pageWeight`) a reply's next block starts
  another row (`AssistantTurn.pages`), and what stands under its text
  (sources, linked records, tool calls) is a row of its own. A block's
  page depends on the blocks before it alone, so pages do not move as the
  reply is written. The lazy stack then measures rows of about a screen,
  and a flush lays out the last one.
- **Nothing a reply's views read changes at a flush but its own rows.** A
  block is drawn by its position, so the one being written keeps its view
  (and where its code or its table was scrolled to), and a finished one is
  an equal value there, which is not drawn again. What a link opens and
  how a sub-agent's conversation opens are set once above the transcript
  as values that compare (`replyLinks()`, `OpenThreadAction`): an action
  set as a closure at each draw read as changed, and every text under it
  resolved again.

Two texts are still parsed where they are drawn, once each and kept: a
compaction's summary and a sub-agent's answer under its call, which show
only when opened and are never being written.

**Code is coloured where the reply is parsed.** A fenced block's row
carries its colours as values (`MarkdownRow.code`, one `CodeHighlight` for
each code block in the row's block, a list's and a quote's included): runs
of a token each (keyword, string, comment, number, name, meta, and a
diff's added and removed), by UTF-8 offset into the block's text, and the
longest line in columns. `CodeHighlighter` makes them, in ChatKit, on the
runtime's executor with the parse, so a finished block is an equal value
at every flush and its view is not drawn again. It is a small reader of
its own, with no package behind it, for the languages people paste: Swift,
Go, Python, JavaScript and TypeScript, JSON, YAML, a shell, SQL, HTML and
XML, CSS (SCSS and Less), Rust, Kotlin and a diff, by the name on the
fence in any case (`ts`, `yml`, `golang`, `postgresql`…). Any other name,
and none, is plain text.

- **A line at a time.** A line's colours depend on its own bytes and on
  what the lines before it left open (a block comment and how deep, a
  string that runs over lines, a here-document, a YAML block scalar, an
  HTML tag), and on nothing after it. So a finished line is coloured once
  and for good, whatever is written after it. A reply is parsed whole at
  each flush, and its code is coloured from the lines read at the last
  parse (`CodeMemo`, kept beside the reply's rows and forgotten with
  them): a line that is as it was, after the same lines, is not read
  again, and a block being written costs its last line.
- **It would rather leave a thing plain than colour it wrongly.** A string
  that must close on its line and does not is not coloured, and neither is
  the rest of that line, so a string half typed never paints its own words
  as keywords. A word is a keyword only where the language has no other
  use for it: not as a member (`value.default`), not before a mark that
  follows a name (`type: string`, `get(key)`), a shell's only where a
  command stands. A regular expression is one only after a mark no value
  ends with, and never after `<` or `>`, where JSX closes a tag. A Rust
  lifetime is no character. A selector that looks like a declaration
  (`a:hover {`) is none, and neither is `12:30` a YAML key or `1.2.3` a
  number. A here-document's body, a YAML block scalar and the body of a
  `script` or a `style` are another language's text and are left plain.
- **Colours** are named in `CodeColours.swift` beside the chat's others,
  each token's on the code block's fill in light and in dark, and
  `ColourContrast` holds every pair to 4.5:1. None carries meaning alone:
  a keyword is also heavier and a comment slanted, and a diff's lines
  start with their own `+` and `-`.
- **Drawn as one text, made a line at a time** (`CodePaint`): making a
  long block into coloured text costs about 15 microseconds a line on the
  main thread, so the view keeps the text of the block's whole lines and
  makes only what follows them. One text, so a selection on a Mac runs
  across its lines.
- **A long line scrolls sideways unless its reader chose to wrap.** The
  switch that wraps is shown only on a block with a line longer than its
  room: the longest line's columns (the highlighter's count, a wide
  character two) times one column's width in the code's font, which is
  measured once above the transcript, against the block's width
  (`CodeWrap`). The choice is the person's, kept on the device with their
  other choices (`ChatPreferences.wrapCode`, also "Wrap long lines of
  code" in Settings), and is every block's.
- **Copy says it has copied**: its mark is a check and its label "Copied"
  for a moment, VoiceOver is told, and the device taps as it does for a
  send. Its room to touch, and the switch's, is 44 points, whatever the
  bar's height.

**A table is laid out for the room it has** (`TableLayout`, ChatKit's rule
over the widths the view measures; `TableGrid`, the layout that draws
it). Each column's natural width is its widest cell on one line:

- where the columns fit, they are drawn at those widths, the table a card
  as wide as its columns;
- where they do not but every column can have a readable width (88
  points at the usual text size), the narrow ones keep theirs and the
  wide ones share the rest and wrap, so a column of prose wraps and the
  name beside it does not;
- otherwise the table scrolls sideways with its first column held in
  place, each column at its natural width and no wider than 260 points,
  the held one no wider than two fifths of the room;
- at the accessibility text sizes each row is a card, each header beside
  its value, the headers' column as wide as the widest header and no more
  than half.

A cell is inline Markdown, a record's pill included; a column keeps the
alignment its Markdown gave it. A column's width follows from its own
cells alone, so a table being written moves a column only when a wider
cell arrives in it or the room runs out. The held column is moved in the
drawing alone (`visualEffect`), so nothing is laid out again while the
table scrolls.

A link opens only when it is
`http`, `https`, `mailto`, a console path (`[Books](/applets/books)`, which
opens that page of the console on the signed-in server) or a record link
(below), and one that opens outside the app carries the external-link mark
beside its text, which VoiceOver reads as "opens in your browser".

## The interface

**Layout.** `NavigationSplitView`: chats in the sidebar, the conversation in
the detail, both columns shown on an iPad and a Mac. iPhone collapses it to
a stack.

**The first screen.** A window opens to a chat, never to an empty detail
(`ChatApp.land`, decided once the chats are held, from the folder at a
launch):

1. the chat it was left in, where that was less than half an hour ago;
2. else the newest chat that is running;
3. else a new chat with the default assistant, whose composer takes the
   focus.

A route the window is about to take (a notification's tap, a shortcut, the
Action Button) wins over all three. Scene storage keeps which chat a window
showed and when it was last in it (`chat.thread`, `chat.left`: moved when
the chat changes and when the window leaves the front), and that is all
state restoration does: it names the chat for the first rule, and a chat
left longer ago is not gone back to. A window left on the list of a phone
has no chat to go back to.

**Glass is the control layer only** (HIG): the toolbars (already glass on 26),
the composer and the Jump to Latest button, which share one
`GlassEffectContainer` so they merge rather than stack. Content is not glass:
suggestion chips, cards, code blocks and bubbles use plain fills; the user's
bubble is a tinted fill. The transcript softens under the composer with
`scrollEdgeEffectStyle(.soft, for: .bottom)`.

### Chat list

- Grouped Today, Yesterday, Previous 7 Days, Earlier, by last activity (the
  console has no week group; the list here is longer), under a Pinned
  section where any chat is pinned
  ([Pinned and archived chats](#pinned-and-archived-chats)). A chat is in
  one section.
- A row: the title (`subject`, else the opening line, else "New chat") and
  when, then the agent's name in its colour running into the start of the
  last reply ("Working…" while it runs), and a state: a dot when unread, a
  question mark when a card waits on the person (the thread's
  `waitingOnPerson` where the server counts it, else counted from the
  pending cards), and the console's mark in
  words for a last turn that didn't finish: "Didn’t finish" (orange) for a
  restart or a limit, "Stopped" (grey) for a stop, "Failed" (red) for an
  error. A chat at work shows a small spinner where its mark would stand.
  No avatar and no agent pill: each took a column and said no more than
  the name.
- Each row is a part of the presentation of its own (`row/<thread>`), kept
  until its thread, its mark or its instant moves: a page read in at the
  list's end makes that page's rows and none above them.
- A row takes its new place with motion when the order changes (a reply
  settled, a pin, an archive), and with none under Reduce Motion.
- While the first read is out and the folder held nothing, eight rows of
  grey bars stand in for the chats, shimmering unless the device asks for
  less motion.
- The selected row draws every word in the system's primary style and
  keeps no colour of its own: the system makes that white on the
  selection's fill while the list has the focus, and the label colour on
  the grey it is while the focus is elsewhere. An iPad fills the focused
  selection with the list's tint, which is the accent as a fill (the icon's
  purple in both modes: white is 2.8:1 on the dark mode's accent); a Mac's
  source list is left to fill it as the system does. On the selection a
  mark is an outline.
- Automated runs are behind a toggle in the list's filter menu (off by
  default), with a glyph for what started each.
- Search: the chats whose title, preview or rows held on the device match
  are found at once, as typed, the archived ones under "Archived". 0.35 s
  after the typing stops the server is asked for the messages that hold
  the words (the ranked read above), and they come under the chats, by
  chat, in the order of each chat's best message: the chat's title, up to
  three of its messages cut to the words around the first that matched,
  each matching word in the accent, and "and 4 more". An archived chat's
  group says "Archived". A message opens its chat at that message
  ([Where it opens](#conversation)). A search that fails says so in the
  list's strip, with Try Again, and keeps the chats found. On a server that
  takes no search from this sign-in the rows on the device are searched
  alone, and "No chat on this device matches" says so.
- Swipe and context menu: Rename, Mark as Read or Unread
  ([Read state](#read-state)), Copy Thread ID; and, where the server keeps
  them, Pin or Unpin and Archive or Unarchive.
- At the list's end, while the server's list goes on, a row reads the next
  page when it comes on screen ([The list's window](#the-lists-window)).
- Pull to refresh; an offline banner ("Offline. Showing saved chats.").
- Toolbar: New Chat (⌘N); Settings (⌘, on the Mac). On a phone the search
  field sits in the bottom bar with New Chat to its right.

### Conversation

- Title: the chat's title (tap to rename, optimistic); subtitle: "with
  <Agent>", or "Working…" while it runs.
- **User turns**: right-aligned tinted bubbles; Copy in the context menu.
- **Assistant turns**: full width, Markdown, no bubble.
- **Under the last reply** stands a row of small marks (`ReplyActions`,
  `ReplyActionsRow`): Copy; Try Again or Continue where the run can go on
  and its card does not already offer that (below); and Share. Each is 44
  points to touch and labelled for VoiceOver, and Copy says it has copied
  as a code block's does. While the reply is being written the row holds
  Copy alone, of what is there. The row is part of the reply's last row
  of the transcript, never a row of its own: it is there from the reply's
  first words at the height it keeps, so a reply that settles moves
  nothing (Share arrives in the row's own height), and the reply that
  takes it over gains what the one before gives up. The last reply is the
  assistant's last turn that has words, and the row stays under it while
  a run goes on (the message that started it, one queued behind it, the
  next call thinking or running tools) and where something of the
  assistant's follows the person's last message (a call, a card that
  waits). It leaves only where the chat is at rest with a message nothing
  answered, and a card stands under that message then, so the transcript
  never gets shorter for it. Older replies keep the same behind a long
  press: Copy, Select Text and Share, and the last reply's long press
  offers what its row does. Where a run ended before the assistant said
  or did anything, its card stands at the person's message, and a Try
  Again the card names beside its button stands alone under that
  message, on its side.
- **The row and what else offers a way on** are decided in one place
  (`ReplyActions(under:failures:working:)`, from `FailurePresenter`'s card
  at the last turn and the turns), so one thing offers each:

  | The chat | Under the last reply | Elsewhere |
  | --- | --- | --- |
  | a reply being written, a message queued or not | Copy | the queued message says "Queued for <Agent>'s next step" |
  | a card that waits (a question, an approval) | Copy, Share | the card; nothing runs again while it waits |
  | a card answered, folded onto its call | as the run it resumed | the call's row says how it ended |
  | stopped, or failed where its card's button leads elsewhere (a key to replace) | Copy, Share, and Try Again or Continue | the card's own button |
  | failed where the card's button is Try Again | Copy, Share | Try Again on the card |
  | ended with a queued message unread | nothing: the message stands after the reply | Continue on the card under the message |
  | ended before any reply, the card's button leading elsewhere | Try Again, alone under the message | the card's own button |

  Rows that arrive above the ones drawn, and a chat opened at an old
  message, change none of it: the row is part of the last reply's row,
  which neither moves.
- **Select Text** opens the reply in a sheet, in the system's own text
  view, where a selection runs across the whole of it: a reply's rows are
  separate views, and a SwiftUI text on an iPhone selects nothing less
  than all of itself. The sheet holds the reply as it reads, not as it
  was written: a heading's weight, a list's marks, a table a row to a
  line with its columns lined up by tabs, code in its fixed-width font,
  links as links.
- **Thinking**: while the model reasons, "Thinking…" with a shimmer over a
  window of a fixed height (about three lines, scaled with the text size)
  that shows the newest of the thinking, from the start of a word
  (`ReasoningPresentation.tail`). When the answer begins it becomes "Thought
  for 6 s", collapsed; tap to read. The window gives its height to the
  answer: while the answer is the last thing in its turn, its row keeps at
  least the height it had with the window in it, so the transcript does
  not shrink at the moment the answer starts. With "expand thinking" on in
  Settings, and once you have opened the section, it is the whole text and
  no window.
- **Text is drawn at a steady rate** (`TextReveal`, in the presenter): a
  piece of a reply is not drawn the instant it arrives but over the steps
  that follow, 30 a second, so a model that sends in bursts reads evenly.
  What is drawn is never more than 0.4 s behind what arrived and never
  moves back, so a reply is whole on screen at most 0.4 s after its end.
  With Reduce Motion on, each piece is drawn as it arrives
  (`ChatApp.Command.pace`).
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
- **A record is one pill wherever it stands** (SubstrateKit's `RecordPill`
  and `RecordPillText`, both drawn from `RecordPillStyle`): the kind's tile
  and the record's name on a grey capsule, the same as the console draws
  them. Under "Linked records", under a tool row and in a card it is a
  view; a name too long for its row is cut in the middle. A tap opens the
  record read-only in a sheet, its own links pushing within it. A session
  an earlier build kept with scopes reads what they reach; a kind out of
  that token's reach is said so ("This app can't read people", and where to
  allow it), never shown as missing, since the server answers both alike. A
  record a read found gone is dimmed and struck through.
- **Record links in a reply** (`[Q4 budget](substrate://<kind>/<id>)`, or the
  older `ref:<kind>/<id>`) are the same pill inside the line, as text: the
  link's own words are its name, it wraps with the line (a capsule on each
  line it spans), sits on the baseline, takes the size of the text around
  it, and leaves the line's height alone, because the capsule and the tile
  are drawn behind the words (a `TextRenderer`). A tap anywhere on it
  opens the record, and one a read found gone is dimmed and struck through
  like the view. A link the engine found no record for (the reply row's
  `mentions`, marked `missing`) is its text alone, so an invented id never
  reads as a record. Under a settled reply, "Linked records" repeats each
  record it links, by its own name. The `done` event's mentions reach the
  reply at once, so its links read so from the run's end, before its rows
  land. While a reply streams, a link is its Markdown until it closes.
- **A link in a line is read as its own words.** On a Mac every link of a
  text is an element a screen reader can go to, and SwiftUI reads its
  words out of the line from a place it worked out in the line as spoken,
  where a piece with words of its own for a screen reader (the mark after
  a link that leaves the app, "opens in your browser", for one image; a
  pill's tile, its kind for four spaces) is as long as those words. Every
  link after such a piece was read from the wrong place: as other words,
  or, past the line's end, as nothing, AppKit raising where it was asked.
  So on a Mac each such piece is made as long as its words with word
  joiners, which draw nothing and break no line (`SpokenPiece`, in the
  kit). An iPhone and an iPad read a link from where it is, and their
  lines are as they were. A pill's link is read as its name on a Mac: the
  kind is the tile's, and a link's words there are the line's own. Words
  selected in a reply on a Mac and copied carry the joiners, unseen, as
  they carry the mark's own character and a pill's spaces; Copy, Share and
  Select Text take the reply's own text and carry none.
- **Sources**: under a finished reply, the web pages it links as numbered
  pills, once each, in the order it first cites them.
- **Questions** (`ask`): a card with each question's options (one or many,
  as the console), Answer and Dismiss, and "Answer in Your Own Words"
  (below).
- **Suggestions** (`propose`): a card for a create, a patch or a delete:
  the record, each property's before and after, Apply and Dismiss, drawn
  from the `propose` call at once; Apply reads the request's version if the
  card holds none, and refuses if the change is no longer the one shown.
- **Approvals**: a card for a call that waits for the owner: the tool, each
  argument whole, Approve and Decline.
- **A card stands only while it waits.** The call that asked is a row of
  the transcript, and while its card waits the row says so ("Waiting for
  your answer", "Waiting for your approval") with the card under it. Once
  the card is answered, dismissed, applied, approved or declined, here or
  on another device, the card is gone and the same row, under the same id,
  carries the outcome: a check or a cross and how it ended ("You answered",
  "You dismissed the question", "You approved gmail_send, and it ran",
  "You declined gmail_send"). Opened, the row shows what was asked with
  what was answered, the arguments of the call, or the change as applied
  or not taken, and when. What was asked is the card's record, read when
  the row is first opened. The presenter moves the outcome onto the row
  (`CardPresenter.fold`): the transcript's rows and their ids are the same
  before and after, so nothing above or below moves but by the card's own
  height.
- **Whose hand ended it** (`agents` 7, decision 0327, `ChatAppHands.swift`).
  A resolved card links the person who resolved it: `resolvedBy` on a
  question, `decidedBy` on an approval and a suggestion. The app reads no
  actors to tell its own person's from another's. It asks the server which
  resolved cards of a thread are `me`'s, one list read per kind (`thread`
  and `resolvedBy` or `decidedBy` equal to `me`, 100 at most), when a
  thread's rows or one of its cards is read and a card of it ended that
  the last such read did not cover. A card in the answer says "You". One
  with a link that a whole answer does not hold says "Someone else"; the
  app cannot read a name without reading actors, so it says none. One
  with no link (resolved before the revision, or decided by a policy's
  rule) says only how it ended ("Answered", "Dismissed", "gmail_send was
  approved, and it ran"), and so does every card on a server below the
  revision, every card whose record is not read yet and is not the
  person's, and, past a first page of the person's own, every card that
  page does not hold. A card this device resolved by its own write says
  "You" from the write's answer. The lines the thread's rows hold for an
  outcome say it the same way.
- **A card whose write failed** stays, with the failure in it in the
  sentence every failure is said in, and its button: Try Again sends the
  same answer over the version read since. It is cleared when the write
  goes through or the card stops waiting.
- **Answer in your own words.** Beside a question's options. It takes the
  card off the transcript (the call's row says "You are answering in your
  own words"), puts the cursor in the composer, and a strip above the
  field says what is being answered, with Show Options. If nothing is sent
  (Show Options, or the chat is closed), the card is back as it was; a
  send the server refuses leaves the question waiting and the text in the
  field. A message typed without pressing it leaves the card standing.
  What is sent depends on the server:
  - At `agents` 7 or more (`Conversation.linksWords`), the message names
    the questions it answers: `repliesTo`, the interaction's id, beside
    `thread` and `messageId`. The server moves the questions to `replied`
    in the write of the message, writes no row of its own for that and
    resumes nothing, so the turn that reads the message is the only one.
    The app writes no dismissal. Every copy of the message says the same
    (`UnsentMessage.repliesTo`, in `unsent.json`), one sent again after a
    kill too, and a repeat resolves nothing twice. It is offered only on a
    card whose interaction's `thread` is the chat's: a sub-agent's
    questions are another thread's and are answered there. Questions
    resolved on another device while the words are on their way are left
    as that device made them, and the message goes as an ordinary one:
    the strip goes, and the card is read again and says what became of
    it. The call's row says "You answered in your own words", and opened,
    each question with the message's words and Show Message, which puts
    the chat at the message (`repliedIn`).
  - Below 7, the message is an ordinary one, and a typed message answers
    and dismisses nothing there, which would leave the questions pending
    for good: once the server holds the message the app dismisses them
    (`state: dismissed`, over the version it read), and the call's row
    says they were dismissed. The server resumes the thread on every
    resolution, a dismissal included, so the assistant takes one more step
    after the reply to the message. The app does the same at 7 where it
    had not read the server's document when the message left.
- **Compactions**: a divider, "Earlier messages were summarized".
- **Decisions and answers** the substrate wrote into the thread: a line in
  the tool calls' shape ("You applied the suggestion", a check or a cross).
  Where the call that asked is among the rows drawn, its row says the
  outcome and this line is not drawn a second time under it; where the
  call is past the rows read, the line stands, a decision's with the
  change under it as a panel. It is the transcript's: no read, so nothing
  to fail.
- **Triggered openings**: "When <record> changed", not JSON.
- **Failures**: [their own section](#failures) below. A turn that failed,
  was stopped or ran out of a limit is a card at that turn; a failure of
  the chat as a whole is one strip above the composer.
- **A deleted chat**: "This chat was deleted", and the draft stays to copy.
- **A provider without a key**: when no key resolves for it, its own or one
  the operator supplies (the operator read's `llm/<row>` item; before that
  read answers, no row is said to lack one), a callout, "Add an API key for OpenAI",
  in a new chat above its openers and scrolling with them, else above the
  composer, with a sheet that names the provider's endpoint and, for
  a password sign-in, writes the key (an app token links to the provider's
  record in the console). The key lives only in the field until it is sent;
  it is never cached or logged.
- **More than 500 rows**: a chat opens with its newest 500. A row at the
  top, "Earlier messages", reads the 200 before the first one held when it
  comes on screen, or when pressed, for as long as the thread holds more
  (`ChatApp.Command.olderRows`). The rows arrive above the ones drawn, and
  a transcript that does not follow its end stays at the turn that stood
  first (`TranscriptScroll`'s `above`: the scroll view is put at that turn
  again once the rows are laid out, since content that grows above a
  reader moves what they read down). A reader who went on down the
  transcript while the rows were out stays at the first row they have on
  screen: the first they had before the rows were laid out (`RowsBefore`),
  since by the time the scroll view can be asked, the rows it has on
  screen are the ones that arrived. A row is kept by its top. A row of a
  reply is about a screen and a half, a table or a block of code in it
  the taller for it, so a reader part way down one when rows arrive is
  put at the start of that row and no further. The next read of the newest rows
  (the thread moved, a run handed over) keeps the older ones where it
  begins among the rows held, and starts the transcript over where it
  does not: more rows were written since than a read holds. The folder
  keeps the newest window alone.
- **The transcript follows its end while you let it** (`TranscriptFollow`,
  ChatKit's rule; `TranscriptScroll`, the view that keeps it). Following
  is your intent, and only you change it:
  - A chat opens at its end and follows it. A new chat lies from the top
    and has nothing to follow until it outgrows the screen.
  - A touch, a wheel, a scroll bar or a key that takes the transcript up
    lets go. Coming to rest within 80 points of the end takes it up again.
    A scroll bar and a key are told from the app's own moves by where the
    scroll view stood when this view last saw it: moved up, with the
    content and the room as they were. Never by the place the system
    hands over as the one before, which may be one the scroll view passed
    through and never said. When stored rows take a stream's place the
    lazy stack guesses its heights again, the content gets shorter and
    taller again in one breath, and read by the system's "before" that was
    a reader scrolling up at the end of every reply.
  - A send and Jump to Latest go to the end from anywhere.
  - While it follows, every change of the content's height and of the room
    for it takes it to the end: a piece of a reply, rows landing, the
    keyboard, a turn of the device, a split view or a window resized,
    another text size. The view reacts to the height the scroll view
    reports, not to a hash of what the screen holds, so nothing that grows
    is missed.
  - A chat that was at its end before the app left the front is at its end
    when it is back, whatever arrived meanwhile; one you had scrolled up
    stays where it was. Following is never worked out from where the
    scroll view stands after its content moved: a reply that grew by a
    screen while the app was away would read as "not at the end", which is
    how a chat used to be left behind.
  - Away from the end, Jump to Latest says how many messages are below
    what is on screen (`ConversationScreen.messages(below:)`), and
    VoiceOver hears "Jump to Latest, 3 messages below".
  - `ConversationScreen.opens` says where a chat is first shown: its end,
    or the turn its "New" line stands above (below), where it then stays
    until you come down.

  What moves at each of those is the scroll view's own state, and the rows
  are handed to it made, so the transcript's body runs once for a flush.
  On iOS the move is to the content's own end (`scrollTo(y:)`): the
  system's move to the bottom edge overshoots by the composer's inset when
  the insets change. On a Mac the bottom edge is exact and is what is
  used. A Mac's scroll view says nothing of a wheel or a trackpad as a
  touch's phases do, so the events themselves are watched: from the first
  one turned up the transcript is yours, however little it has moved. It
  has come to rest a quarter of a second after the last event, and never
  while fingers lie on the trackpad. The glide after a flick lets go of
  nothing, so a send or Jump to Latest made while it lasts keeps the end.
  `scrollDismissesKeyboard(.interactively)`.
- **Where it opens**: a chat with more than the person had read opens at
  the first turn they had not, under a "New" line; where all of that fits
  on one screen the scroll stops at the end, as a chat with nothing new
  opens. Where the rows the device held were older than the thread, the
  rows read on opening bring the line with them: the chat is put at it
  once they are laid out, and does not follow its end in between. The
  line stays while the chat is open and is gone the next time
  ([Read state](#read-state)). Where the person read through a message
  further back than the rows held (the newest 500), the line is above
  them: the rows before are read toward it, as for a search's hit, five
  reads of 200 at most. While that is out the chat stands at the top of
  what it holds, under the row at the top, which says "Finding where you
  left off…", and no line is drawn where none is known
  (`ConversationScreen.seeks`). Once the rows hold the message, the line
  is drawn and the chat is put at it, unless its reader has touched,
  scrolled, sent or jumped by then. A line more than about 1,500 messages
  back is not reached, and neither is one past a read that failed: the
  chat stays at the top of what was read, a line there says "New from
  above", and the row over it, "Earlier messages", reads further. A chat
  opened at a message, from a search,
  opens at that message's turn the same way, and each time it is opened at
  one again while it is shown (`ConversationScreen.visit`); where the rows
  held do not reach the message, the rows before them are read until they
  do, five reads at most. A transcript still to be put at its row is not
  kept at the turn that stood first when those rows arrive, nor is one
  put there with them. It stands on the scroll view's seam:
  `ConversationScreen.opens` names the turn, by the id the turn is drawn
  under (a row streamed in this conversation keeps the id its stream gave
  it); the line is a row of the transcript above that turn's first, which
  is the row the scroll view is put at, once; and a chat opened there does
  not follow its end until you come down to it. A reader who has already
  touched, scrolled, sent or jumped when the line arrives is left where
  they are.

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

- **It says what is happening** (`ComposerStatus`, one line above the
  field, from the send to the end): "Connecting…" until the stream opens,
  "Thinking…", "Writing…", the running tool in its own words ("Looking
  through your records…", "Asking Librarian…"), "Reconnecting…" while a
  stream that dropped is opened again, and "Stopping…". It stands where
  "Waiting for the answer" stood, which would say it twice. Stop is there
  the whole time, and you can type the next message throughout. Where the
  server takes a message into a running turn (`agents` 6), Send stands
  beside Stop once there is text, and the line under the status says what
  is queued and what a stop would do to it ("1 message is queued. If you
  stop Moss first, it stays unread until you continue."). Below `agents`
  6, and for an agent that is not a chat's, only Send waits: the text
  stays in the field, the bar says "Waiting for the answer", and Stop is
  the one button until the reply ends, as before.
- **Answering in your own words**: after "Answer in Your Own Words" on a
  question's card, a strip above the field says what is being answered
  (the question, where there is one) with Show Options, which puts the
  card back. The cursor is in the field.

- **Keys**: Return makes a new line and ⌘Return sends, on a Mac and on an
  iPad's or an iPhone's hardware keyboard alike; Shift-Return and
  Option-Return are new lines too. The keyboard on the screen has no ⌘, so
  there the button sends. The rule is ChatKit's `ComposerKeys`, which the
  field's `onKeyPress` asks: only ⌘ alone sends, and a Return that confirms
  an input method's marked text is left to it. On a Mac and an iPad the Send
  button's help and its hint name the shortcut. ⌘. stops.
- While a send cannot go, the text stays and the bar says why ("Waiting for
  the answer", "Send or edit the message above first", "Checking whether
  your last message arrived" on a server without message ids, "Offline",
  "Add an API key first").
- Drafts are kept per chat across launches.
- **The text is the field's while it is typed.** A key is no command: it
  runs no engine and no presenter and draws nothing but the field. The
  conversation is told the text after half a second without a key, never
  more than two seconds behind while the typing goes on, at once before a
  send, and when the field stops editing: it lost the focus, the app left
  the front, or its chat left the screen, when the list is told as well,
  since on a phone the conversation may be gone first. The tree carries the
  draft only as the engine wrote it: a send cleared it, a refusal gave it
  back, dictation landed, an opener was picked, or the chat was shown again
  and takes up what was typed into it. Each such rewrite moves the draft's
  revision and says what text it stands in place of, and the field takes
  the new text with whatever it held beyond the replaced one kept at its
  end, so a key pressed while a rewrite is on its way is never lost
  (`DraftTests`: on a send, on leaving the front, on switching chats, on a
  refusal, on dictation). A send tells the field's whole text first,
  whatever was told before, so two windows on one chat each send what their
  own field shows. The drafts are written to the disk at once from the
  app's first word of leaving the front (inactive) until it is in front
  again, whichever of that word and the field's own arrives first, and in
  the order the writes were asked for, so an older one that comes to run
  later is dropped; at any other time the write waits 0.6 s for more.

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
- **Chats**: show automated runs; expand thinking; wrap long lines of
  code; show technical details (the thread id, status and tally under the
  title).
- **Notifications**: the two switches, the system's state with a link to its
  settings, and what the app can and cannot see.
- **Voice**: the language.
- **Storage**: size; Clear Saved Chats.
- **Diagnostics**: Share Diagnostics ([its section](#diagnostics)).
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

- Haptics on send, on Stop, on a copy, on a reply settling on screen, on a
  failure appearing; and a light one for a reply that settles while its
  person is elsewhere in the app (another chat, the list, Settings), which
  is felt and not said.
- Springs for the send/stop morph and the growing field; rows move to the top
  animated.
- The live turn carries `.updatesFrequently`; a settled reply is announced
  once, and felt, where its chat is on screen (the list's `answered`
  signal, acted on once per raise by `PresentationModel.onSignal`); Reduce Motion stills the shimmer and the pulse; code scales with
  Dynamic Type; every control has a label; light and dark.
- **Colour.** Every colour the chat's own views choose has a name in
  `ChatColours` (`apple/Sources/Views/ChatColours.swift`), over the kit's
  `DesignTokens` for what another app would share: quiet words
  (`quietText`, where the system's secondary label is 3.4:1 on white), a
  tone as words (`toneText`: red, orange and green are not text colours on
  a white page), the accent as words and as a fill, an agent's colour as
  its pill's tint and as the ink of its name. `ChatColours.pairs` lists
  the pairings of words and fill the list, the transcript and the cards
  draw from those names, and `ColourContrast` in the app's tests holds
  every one, and every hue an agent's id can hash to, to WCAG's 4.5:1 (3:1
  for large text) in light and in dark. A prominent button of the chat's
  own is filled with the accent as a fill. Still the system's: a Markdown
  block quote, the composer's notes and its key callout, the navigation
  bar's subtitle (the secondary label), and a prominent button in the
  kit's screens (white on the accent's text tone).

## Read state

How far a person has read a chat is the server's record of it, one
`substrate.reamde.dev/llm/threadmark` per thread and person (decision 0313),
so a chat read on the phone is read on the Mac. The app reads the person's
marks with every list of threads, keeps them in `state.json` so the list
draws its dots before it asks anything, and follows the kind on the change
feed. Everything below is `ChatApp/ChatAppMarks.swift`, and
`MarkTests.swift` holds it.

**Which server.** The `records` feature at revision 8 or more has marks.
Below that, or where the server answers that it does not know the kind, the
app behaves as it did before marks: the device's own `seen` map (each
chat's `finishedAt` when a window last showed it settled) says what is
unread, opening a chat reads it at once, Mark as Unread and Mark as Read
change that map alone, a reply notifies at once, and the three pending
lists are read whole. Nothing names the mark kind: no read, no write, no
watch.

**What is unread.** A settled chat is unread when its mark says
`markedUnread`, or when the thread's `lastMessage` is not the mark's
`readThrough`. `lastMessage` is the server's: the newest message the
thread settled with, passing over a message of the person's that no reply
came after (from `agents` 6), so a chat whose last send failed is failed
by its status and not unread. A thread with no `lastMessage` is unread
only when marked so. A thread the person has no mark of was never opened,
and is unread; so is one whose mark holds no position, as a chat pinned or
archived from the list before it was ever opened has: a mark alone does
not say a chat was opened. A
chat that is running is working, not unread. A chat whose mark the server
has not said yet is not called unread, and its reply is not notified, until
it has: a list read whose marks failed shows no dot it would have to take
back, and a server whose marks were never read (the first read failed)
notifies nothing by the device's own map, which knows nothing of the
other devices. A reply that waited is judged against the thread's row
that came with its mark, never an older one, and is notified by the
device's map after all if the server turns out to keep no marks. A thread this device just started has no mark, which the
app knows without asking.

The marks are read after the threads they are compared with. A mark whose
position was taken after its thread's row settled, of a message that is
not the row's newest, may read through a reply that settled between the
two reads, and would look unread against the older row: the threads are
then read once more. This matters to the refresh job, which has no feed
to put it right a moment later.

A mark can also arrive without its thread's row: a read of the marks
alone (after a list read whose marks failed), the answer to this device's
own write, the folder's state at a launch. Where such a mark is ahead of
the row held by the same test, the chat is not called unread for the
older message: it reads as it did, the row is asked for again
(`rowsBehind`), and what that read brings is told by the rule. The row is
read by a read of its own, so the answer that ends the wait is known to
have started after the mark was learned: a list read under way before it
may still land the older row, which changes nothing, and a thread opened
on its own is in no list. A read that fails is made again when the server
next answers a refresh or the app comes to the front. A dot that went
does not come back for the time one read takes to follow another, and a
position another device took from an older row of its own still shows its
dot once that read has answered.

**Reading.** A chat is read once the newest message it settled with has
been on screen for a second, in a window that is in front and active (the
key window of the app in front, on a Mac) with nothing presented over it:
a chat behind a sheet or an alert is in front of nobody, which on iPhone
and iPad the view learns from UIKit dimming its tint (`CoverProbe`), the
one sign a view behind a presentation is given. A menu dims nothing and
covers little, and does not count. The view says whether
the end of the transcript is on screen, which is the scroll view's word
(`TranscriptScroll`'s `looked`): the transcript follows its end, and the
end is within what shows. Both: a chat opened at its "New" line, or one
you scrolled up, does not follow, so nothing that arrives below it is read
until you come down; and one that follows is not at its end in the moment
before a reply that just grew has been scrolled to. The list counts the second, from the
moment the rows drawn hold that message, and stops counting when the chat
scrolls away from its end, runs again, or leaves the front. So a reply
that settles while the person is looking at the end is read without a
scroll, a reply that arrives below a chat scrolled up is not, and a
resumed run that settles a second reply is read like the first. Nobody
is looking while the sign-in has ended: the screen that says so stands
over every chat. The mark moves to the thread's `lastMessage`, never to
the newest row.

The chat reads so on this device at once: what the device moved is laid
over the server's mark until the server answers. A read this device made
of a message that is no longer the thread's newest is not laid over: the
thread has moved on, another device may have read further or marked the
chat unread since, and what the server holds is believed (the write still
goes, and the server, which never moves a position back, drops it). The
write waits two
seconds, so a run of replies is one write, and goes out at once when no
window in front shows the chat any more, when the app goes inactive and
when it goes to the background. One thread's writes go out one at a time,
in the order they were made. A write that fails is nothing the person can
act on and is never shown: it waits, across a launch too, for the next
time the server answers a refresh or the app comes to the front. One the
server refuses for good (the thread is gone, the message is no longer one
of its) is dropped, and the chat reads as the server says.

**Forward only.** A mark held under a later version than the one a read
brought is kept, so an answer that left the server before this device's
write, or before a newer read, never brings a dot back. Mark as Unread on
another device does: it is a newer version of the mark.

**Marks from elsewhere.** A mark's row on the feed re-reads the list and
the marks together (a mark that reads through a reply this device has not
heard of would otherwise look unread). When a mark says a chat was read,
the dot goes, the chat's delivered and pending notifications are taken
back, and the badge is counted again, as when the chat is opened here.
The app keeps the threads it notified a reply of since its launch
(`announced`) and takes each back once the mark it holds reads the reply,
however the two came to agree: the first marks a launch learns take back
what the device's own map notified before them, and a row a stale list
read dropped and a later one brought back beside its mark is taken back
too.

A call that failed before any reply leaves `lastMessage` where it was, so
the chat is not unread and no mark moves for it. Its failure is notified
all the same where the chat is not on screen, unless the mark's position
was taken after the thread settled: then somebody has looked since.

**Mark as Unread and Mark as Read**, on a row's swipe and its context
menu, and Mark as Unread in a chat's menu. Mark as Unread writes
`markedUnread` and never moves the position. A chat marked unread while a
window shows it stays unread until it is left and opened again, or says
more. Mark as Read reads through the thread's `lastMessage`, which clears
the flag with it; on a thread that names none it clears the flag alone.

**The "New" line.** When a chat is opened with messages after the one the
person had read through, the line stands above the first turn after it,
in the rows the device holds, and the chat opens there. Where this device
saw further than the server has heard (a read not written yet), or the
server holds further than this device saw, it is the later of the two. A chat the person
never opened is new from its first turn held, and opens there. A chat
only marked unread has nothing new in it and no line. It is decided once,
on opening, so it stays while the chat is open, read or not, and is gone
the next time.

**What waits on the person.** A server that keeps marks counts each
thread's pending questions, suggestions and approvals on its row
(`waitingOnPerson`). The row's mark reads that, and the pending cards are
read only under the threads that say something waits, which is none of
the three reads in the usual case. A card in a chat still reads its own
record when it is drawn, and a sub-agent's approvals are found under the
thread they wait on.

**The device's own marks are not uploaded.** The server's migration marked
every chat from before as read, for every person, so the `seen` map a
build before this one kept is sent nowhere. On the first launch against a
server with marks the list draws from the disk as it was left, and the
moment the marks land they replace the map in one step: a dot only this
device knew of goes, a dot the server knows of shows, and no list in
between shows every chat as unread. The map is kept for servers without
marks, and a server that stops keeping them (a rollback) leaves each dot
where it was.

## The list's window

The list holds a window of the server's list that grows at its end: its
first page at a launch and at every refresh, and the page after the last
one read each time the list is scrolled to its end, asked for with the
cursor that page gave (`ChatAppLists.swift`; `ListTests.swift` holds it).
A chat is never listed twice: a page's row takes the place of the row held
under the same id.

**A refresh reads the first page alone**, however far the window has
grown, and so do the change feed and a mark that arrives. The page is
merged into the window:

- Where the page and the window share a chat that has not moved since,
  every chat that did move is on the page above it. The chats on the page
  replace their rows, the rows past the page stay, and a row inside the
  page's span that is not on it has left the list (deleted, or archived on
  another device).
- Where they share none, more chats moved than a page holds, and what lies
  between the page and the rows held was never read. The window starts
  over from the page, and grows again from its cursor.
- Pull to refresh, and Try Again on the list's strip, start the window
  over too: the person asked for the list anew. So does a cursor the
  server can no longer go on from (`410`). The archived chats' window
  starts over with them, and when Automated Runs is switched: a cursor
  goes on only under the filter it was cut for.

**What no list read reaches is read by name.** A read of the first page
says nothing of a chat past it, of one that just left its list, or of one
a window shows that no list holds. Each thread held that a row of the
change feed names (its own, or its mark's, whose id starts with the
thread's), each thread a conversation is on and each thread a notification
stands for is read on its own with the person's mark of it wherever the
lists read beside it do not hold it, in a background refresh too. A row
going missing from a list says nothing by itself, since a read may have
left the server before the chat was there: a notification is taken back
by the mark that says the chat was archived, or by the read that finds
the thread gone. One read so that came to wait since the lists were read
is listed with the ones that wait.
The watch resumes from where it stopped, so what moved while a phone was
away is told when it is back.

A page asked for from a place the window has since left is dropped.
Automated runs are their first fifty and do not grow.

## Pinned and archived chats

Where the server keeps a person's `pinned` and `archived` on their mark of
a thread, the mark's two flags (decision 0322, `records` at revision 9 or
more), a chat can be
pinned and archived, from the row's swipe and its context menu on iPhone,
iPad and Mac. Below that revision the list is every chat, neither action
is offered, and neither property nor the filter arm is ever sent. A server
that says revision 9 and then refuses the arm (`400`: a rollback the
folder had not heard of) is read as one without, until its document is
read again.

**Four lists, merged by the thread.** The chats not archived are the list,
and its window. The pinned ones and the ones that wait on a person are
each read whole beside it, since either may be older than any page, and a
chat that waits may be archived. The archived ones are read a page at a
time while a window lists them. A thread in several is one row, at the
newest version any of them holds.

**The rules, which are the client's** (the server holds none of them):

- An archived chat is in no list, not in the unread count or the badge,
  and a reply in it notifies nobody. `ChatAppState.notifies(_:row:waits:)`
  is the one test of that, for whatever posts a notification of a thread
  or takes one back.
- While something in it waits on a person (`waitingOnPerson` above 0) it
  is listed all the same, with the waiting mark, and counted and notified
  for as if it were not archived. Its flag stays, so it leaves the list
  again once answered.
- Pinned chats stand in a section of their own above the rest, however
  old. A chat both pinned and archived is with the archived ones and keeps
  its pin.
- Speaking in an archived chat ends its archive: a message, Send Again,
  Try Again and Continue write `archived: false`. The message never waits
  for that write. A message sent while the assistant works and an answer
  in the person's own words are messages like any other. So is a reply
  typed into a notification, and since the app may have been away when
  the chat was archived on another device, the chat's mark is read beside
  that send (`confirmMark`), and the archive ends when the read says it
  was archived. Only that read's word counts, and only while this device
  has moved no flag of the chat since: a chat its person archives here
  after they spoke stays archived, and the read is let go. Answering a card (an option, Approve, Decline) is no
  message: the chat stays archived, and leaves the list once nothing
  waits in it.
- Until the person's mark of a chat is read, and where that read failed,
  the list the server answered a chat in says which it is (it narrowed
  the lists by the same marks): one that came with the chats is not
  archived, one that came with the pinned ones is pinned.
- The Archived list is offered by the server's revision, never by having
  read an archived chat: with every chat archived the list is empty.
- The thread kind is followed on the change feed whenever the app runs in
  front, and the mark kind once a read of the marks was answered, also
  with no chat listed: an archived run comes back when it comes to wait,
  and a chat unarchived on another device when its mark says so.

**Pin and Archive are the mark's writes.** Each shows on this device at
once and is written on the path every write of a mark takes (`ChatAppMarks.swift`):
laid over the server's mark until the server answers, one write a thread
at a time, with whatever position the thread's mark has waiting. A write
that fails is made again at the next chance, across a launch too, and is
not shown; one the server refuses for good is said in the list's strip
("Couldn't save that change to the chat."), where Try Again writes it
again, and the chat reads as the server says. A list read that left the
server before the write was taken brings the chat back in its page and
moves nothing: what this device moved stays laid over until its own
answer. A pin or an archive still to be written when the server turns out
to keep none (a rollback) is never sent, and the position it waited
beside is; one already on its way is refused whole, and its position goes
again alone.

**Archive asks nothing and offers Undo.** The row leaves at once and a bar
at the list's end says "Archived “Title”" with Undo for six seconds. Undo
writes `archived: false` and puts back the row that was there, at its
place: the chat's row is kept among the archived ones from the moment it
leaves. What the chat had notified is taken back when it is archived, and
when a mark from another device says it was.

**The Archived list** is in the list's Show menu: the window's list
becomes the archived chats, newest activity first, each with Unarchive on
its swipe and its menu, a page at a time, and Chats leads back. An
unarchived chat is listed again where its place is inside the window read
so far, and otherwise once the list is scrolled to it.

## Failures

A failure is a value until it is drawn. Where a request, a stream or a run
fails, the app keeps a `Failure` (`ChatKit/Sources/ChatKit/API/Failure.swift`):
what was attempted (a send, a message that stands unsent under its id, a
continue, the reply itself, a stop, opening a
reply's stream, a reload, a chat's rows, a rename, a card read, an answer, a
decision, a key, the list's refresh, the change feed, the memories and
instructions) and why (a run's `status` and `cause`, a reply's `cutBy`, an
HTTP status with the envelope's `code` and `reason`, no connection, a server
that cannot be reached, a timeout, a stream that ended early, the app closed
or the sending stopped before the answer, a sign-in that ended, an approval
that does not reach, an answer that was not the API's).
It holds no sentence. `FailurePresenter` is the one place it becomes words:
a sentence, at most one note, one button and the details, in switches with
no default, so a new cause or attempt does not compile without its sentence
and its button.

**One place, by what it is about, never an alert**, and one component
(`FailureView`) draws all three:

| About | Where | Which attempts |
| ----- | ----- | -------------- |
| A turn | a card at that turn in the transcript | the reply, Try Again, Continue, a message that stands unsent |
| The chat as a whole | one strip directly above the composer | a send that started no turn, a stop, a stream that will not open, a reload, a chat's rows, a rename |
| The list | one strip at its top | a refresh, a page of the list, the change feed, a rename of a chat no window shows, a search of the messages, a pin or an archive the server refused |

A second failure in a place replaces the first. A question's, a
suggestion's and an approval's card, the key sheet and a row of Settings say
their own failure in place, from the same presenter. A 401 is the kit's
signed-out screen. A stream that dropped and is being opened again, and a
change feed that dropped, are states (`ScreenFailures.reconnecting`,
`ChatListPresentation.reconnecting`), never drawn as failures.

**The sentences.** A run that did not finish reads by the server's `cause`,
which the thread, the `error` event and the `done` result carry:

| `cause` | Sentence | Button |
| ------- | -------- | ------ |
| `provider_key_refused` | Anthropic refused the API key. | Replace Key |
| `provider_rate_limited` | Anthropic is limiting requests. Try again in 20 seconds. | Try Again, counting down |
| `provider_billing` | The Anthropic account is out of credit. | Try Again |
| `provider_overloaded`, `provider_unavailable` | Anthropic is having trouble. (What Moss wrote so far is kept., where the transcript holds it) | Try Again |
| `provider_rejected` | Anthropic turned this request down. | New Chat |
| `model_unavailable` | Anthropic does not offer this model to your key. | Open in Console |
| `context_too_long` | This chat is too long for the model. | New Chat |
| `allowance_spent` | Today's allowance is used up. It resets at midnight UTC. | Add Your Own Key |
| `interrupted`, `shutdown` | The server restarted while Moss was working. | Try Again |
| `write_failed` | The server couldn't save what Moss was doing. | Try Again |
| `internal` | Moss ran into a problem and didn't finish. | Try Again |
| `turn_limit`, `tool_call_limit`, `deadline` | Moss ran out of room before finishing. | Continue |
| `requested` | Moss was stopped before it finished. | Continue |

A cause this build does not know gets the sentence for its status ("Moss
couldn't finish this reply.", "Moss ran out of room before finishing.",
"Moss was stopped before it finished."). A server older than `cause` sends
none: only then is its text read for one (`FailureFallback`, the whole of
that matching, to delete when no such server is left). A reply whose row
says `cutBy` reads "The reply stopped at the model's limit." with Continue.
A refusal before the stream opens reads by the envelope's `reason`:
`thread_busy`, `nothing_to_continue`, `turn_cap`, `message_id_taken`
(a standing message's own card, with Edit Message), `spend_cap` ("Today's
spending limit is reached.", Open in Console), `provider_key_missing`
("Anthropic has no API key yet.", Add Key: a missing key is refused before
any thread opens, so it is never a run's cause), and today's `quota` and
`key_refused`. Anything else says what did not happen, with why as its note:
"You're offline. Your message is in the box below." (Send Again), "Couldn't
stop Moss. The reply is still being written." (Try Again), "Couldn't load
the end of this chat." (Try Again), "Couldn't refresh your chats." (Try
Again), "Live updates stopped." (Try Again), "Couldn't search your
messages. Chats are found by their titles." (Try Again, which searches
again), "Couldn't save that change to the chat." (Try Again, which writes
the pin or the archive again).

**The details.** The server's own words (a thread's `reason`, an envelope's
message) are behind a Details disclosure, under "The server said". What the
device saw of a connection that failed is under "This device reported",
never under the server's name.

**The buttons.**

- Try Again and Continue run the thread again through the continue route,
  which writes no message, where the server's document says it has it. While the request is out the card stays, its
  button working and not pressable; when the server answers the card goes,
  and when it does not the new failure stands in the same place.
- A rate limit that says how long (`retryAfter`) counts down on its button,
  which cannot be pressed before the end. At zero the app tries again by
  itself, once, and only while the chat is on screen; it never does on a
  server without the continue route, where going on needs a message of the
  person's.
- Send Again on a message that stands unsent sends the same request again
  under its id, whatever refused it: that cannot send it twice. On the
  strip above the composer it is the composer's Send. What was typed is
  never lost: it is in a stored row, in the composer, or on a message that
  stands with its card.
- Add Key opens the app's own key sheet where the provider has no key, and
  otherwise the provider's record in the console (`data/…/llm/provider/<id>`),
  as Replace Key and Add Your Own Key do. Open in Console opens the agent's
  page (`agents/<id>`): the nearest page to a model or a spending limit.
  Each opens in the browser, and carries the external-link mark.
- New Chat starts one with the same assistant.
- **One place offers the run again.** Where a card's one button is the
  run again (Try Again, Continue), that is where it is offered. Where the
  button leads elsewhere first (Replace Key, Open in Console, Add Your Own
  Key, Add Key), trying again is still the way on once that is done, and
  Try Again stands under the reply, in its row of actions
  (`FailurePresentation.resume`): the presenter that words the card says
  so, and names it only where the card's button is another, so the two
  never offer the same. Where asking again asks the same thing (a request
  the provider turned down, a chat too long), it is offered nowhere.

**Older turns.** A stored reply that says `cutBy`, and a message no reply
follows, say so at that turn in a card with no button: only the newest turn
can be tried again.

**Access.** The card's words are one element, labelled with the sentence
and the note, and its button is the next. VoiceOver hears a failure when it
appears in the chat on screen, with the error haptic. The colours are
numbers (`FailurePalette`) and a test holds every pair that carries text to
4.5:1 in light and in dark. Nothing in the card has a fixed height, and the
button and Details stack where the text size leaves no room side by side.

| Where | What the person sees |
| ----- | -------------------- |
| Offline | A line on the list; the composer keeps the text and says "Offline" |
| The list's read fails | The cached content stays; the strip at the top of the list, Try Again |
| The change feed ends | The strip at the top of the list, Try Again |
| Send refused, or it never left | The strip above the composer; the text back in the composer; Send Again |
| Send unanswered | Nothing while it is sent again under its id; then the message stands with its card, Send Again and the pencil |
| Send unknown, on a server without message ids | "Checking whether your message arrived…", then nothing, or the strip and the text back |
| The app was killed mid-send | The message at its place at the next launch: the stored row where the server holds it, else standing with Send Again |
| The stream drops | Nothing; the app attaches again with backoff |
| The turn fails, is stopped or runs out of a limit | A card at that turn: the sentence, Try Again or Continue, Details; the list marks the chat |
| Stop fails | The strip above the composer says the stop failed; the reply goes on |
| Reload after a turn fails | The run stays shown; the strip above the composer, Try Again |
| A card's write fails | The failure in the card, with Try Again; a 409 re-reads and asks again |
| A run ends, stopped or failed, with a message sent into it unread | The card at that turn says it was not read; Continue has it read |
| A run ends on its own and no turn starts for a message sent into it | The card at the end of the chat: the assistant didn't answer, Continue |
| An action from a notification fails or outlives its 20 s | A new notification with the sentence; the reply stands in the chat with Send Again, the card still waits |
| The chat was deleted | "This chat was deleted"; the draft stays to copy |
| 401 | The signed-out screen |

## Diagnostics

A file a person can share with whoever is helping them, of what the app
decided and how long things took, and never a word of a message, a title,
a record's name, a server's address or a token
(`ChatKit/Sources/ChatKit/Local/Diagnostics.swift`,
`ChatApp/ChatAppDiagnostics.swift`).

**What it says**, a line each:

| Line | Says |
| ---- | ---- |
| `launch`, `app phase=` | a launch over the folder, and the app going to the front and away |
| `features` | the `agents` and `records` revisions the server's document said, when they move |
| `list read`, `rows read`, `older rows read` | a read asked for, and of how many chats |
| `feed opened`, `feed dropped`, `feed back`, `feed compacted`, `feed ended` | the change feed |
| `send`, `send out`, `send repeat`, `message stands`, `send again` | a message's press, each copy that left, the wait before the next, and when it stood unsent |
| `stream opened`, `run of=attach`, `stream wait`, `stream retry`, `stream done`, `stream idle`, `stream ended` | which stream opened, dropped and why, and how long the app waited before it asked again |
| `failure` | each failure as the value it is: `attempt`, `why`, and the `status`, `code`, `reason` and `cause` where they are of a closed set |
| `mark write`, `mark written`, `mark not written then=` | a mark's write, what it carried (a position, a flag) and what became of it |
| `notification posted`, `notification held`, `notification taken back`, `notification action over` | a notification, of what (`reply`, `card`, `failed`) |
| `follow stopped by=` | why a transcript stopped following its end: `touch`, `wheel`, or `bar` for a scroll bar or a key |
| `timing of=` | what a `flush`, a `parse` and the main thread's half of a publish (`republish`) cost in a run: how many, the mean and the longest, from the kit's `Meter` |

**How it cannot hold a word.** A line is built only from words written in
the source (`StaticString`), numbers, and words of the app's closed sets
(`Diagnostic.with`). A word the server says is written only where it is
one of a set this build names (its run causes, refusal reasons, cuts,
statuses, error codes) and otherwise as `other`: an unknown cause from a
newer server is a sentence as far as this build knows. A failure is
written without what the server said and what the device saw, which are
sentences. A chat is `chat 3`, by the order the launch met it in, never
its id.

**One writer, one order.** A line is a pure reading of one step: the
event the list applied, what the step asked for, and for a conversation
the lines its report carries to the list (`Conversation.notes`). The list
writes them in the one effect that follows (`ChatAppAsk.diagnose`), which
stamps and numbers them as it is asked and appends them off the main
thread.

**A bounded ring on disk.** Two files in the repository's folder:
`diagnostics.log` is appended to, and past 128 KB it becomes
`diagnostics.old.log` in place of the one before. So the file is kept
across launches, holds between one and two files' worth of the newest
lines, and is gone with the folder at a sign-out.

**Sharing it.** Settings → Diagnostics → Share Diagnostics shows the whole
text first, under what it holds and what it never holds, with the build's
version under its heading; Share hands exactly that text to the system's
share sheet. Nothing leaves the device that was not on that screen.

## Feature list

**Must (v1)**

1. Sign in through the app authorization flow (the password and an optional code on an older server); Keychain; Sign Out with revoke and the full teardown.
2. Chat list from cache, refreshed, live through the feed; titles, previews, day groups, run state, unread, attention marks.
3. Conversation from cache, current by version; prefetch of recent chats.
4. Streaming send: reasoning, tools, compaction, result; every event and exit in the tables above.
5. Attach to running chats, re-attach on drops and on returning to the foreground; background runs.
6. Stop; a message sent while the assistant works, queued until a call reads it (`agents` 6).
7. Every message under an id of its own, sent again when its answer is lost and kept across a kill; refused sends; unknown-delivery sends with the reconcile reads on a server without ids; for existing and new chats.
8. One model of a turn from its first token to its stored row: no blink at the handover or on a return to the front; reload failure with Try Again.
9. Markdown replies; code in its language's colours with Copy and a switch to wrap; tables laid out for their room; Copy, Share and Select Text on a reply, and Try Again or Continue under the last one.
10. Agent picker; Settings → Agents for which agents are offered and the default.
11. Voice typing with live transcription.
12. Notifications for settled replies and attention items, each answered where it stands: a reply with text, an approval, a single question's options; continued processing on iOS.
13. Questions, suggestions and approvals answered inline, CAS'd; a card that ended is the mark on the call that asked, and says whose hand ended it where the server links one (`agents` 7); a question answered in the person's own words, by a message that names it at 7.
14. A provider key added in-app (a password sign-in; an app links to the console).
15. Rename.
16. Drafts per chat across launches.
17. Offline banner and blocked-send reasons.
18. Mac menus and shortcuts, Settings window, ⌘Return to send.
19. Glass composer that says what is happening, toolbar, and Jump to Latest with how many messages are below.
20. Action Button / App Shortcuts: New Chat with an assistant, optionally listening.
21. A window opens to the chat left a short while ago, a running one, or a new chat.

**Polish (v1)**

22. Delta coalescing; Markdown parsed in the presenter, closed while it is written, a long reply in several rows; equatable turn views; a presenter made of kept parts, and a tree published in parts.
23. Haptics; the send/stop morph; the thinking shimmer and its window; text drawn at a steady rate.
24. Search over titles, previews and cached transcripts, with the server's ranked messages under them.
25. Technical details.
26. Unread dot and badge.
27. Accessibility for streaming text.
28. A list and a transcript that grow past their first read; pinned and archived chats where the server keeps them.

**Later**

- Handoff of the open chat (`NSUserActivity`).
- A share extension that starts a chat with the shared text.
- Widgets; a Live Activity of its own.

## Testing

Five kinds of test hold the app, and no CI job runs any of them: a change
runs them on a Mac, and ChatKit's on Linux too.

| What | Where | Run by |
| ---- | ----- | ------ |
| The rig: ChatKit on the kit's runtime against a scripted server, on a clock the test moves | `ChatKit/Tests/ChatKitTests` | `mise run apple:test:chat`, `mise run apps:test:linux` |
| The seeded suite: a person and a server that misbehave by a seed, with the rules a chat keeps whatever happens | `SeededRun.swift`, `SeededServer.swift`, `SeededThread.swift` | every `swift test`, and by hand for more seeds |
| The frame tests: the app's own views in a window, frame by frame | `apple/Tests` (`HandoverTests`, `FollowTests`, `OpeningTests`, `OlderRowsTests`, `ReplyPartsTests`) | `mise run apple:test:chat` |
| The gallery: every state drawn to a picture for a person to look at | `apple/Tests/GalleryTests.swift` | `mise run apps:gallery` |
| The measure: what a streamed reply and typing cost, as a table | `apple/Sources/Measure` | `mise run apple:measure:chat` |

**The rig** (`ChatRig`). The list is run as the link runs it, on the kit's
`ControllerHarness`: a `TestClock` the test moves, ids in sequence, a
folder of its own, recorded notifications and scripted background work,
and every kept part of the presentation made again and compared at each
step (`verifiesParts`), which stops the process on a part kept past a
change to what it reads. The server is one of three, each a transport the
client is handed in place of the network:

- `ChatServer`: routes scripted by the test, an answer each (a body, an
  envelope, an ndjson stream held part way until the test lets it go). The
  list's seven rules are held against it, by number (`RuleTests`), and so
  are the failures and the send's every exit.
- `MarkServer`: a server that keeps marks, flags and pages by the real
  one's rules, for any number of devices at once (`MarkTests`,
  `ListTests`).
- `SeededServer`: the truth a correct server would hold, with its turns
  moved only by the test, and faults where a test or a seed says
  (`AnsweringTests`, and the seeded suite).

A test waits for a state, never for a time: `harness.until` asks its
condition each time the runtime moves, and `until(advancing:)` also moves
the clock to each wait the app asked for that ends within the span named.
It fails after a minute in which nothing moved at all, so a loaded machine
slows a test and does not fail it. What a test may not do is lean on an
order nobody promised. Two tests that timed out on a busy machine were
each the app doing that with the change feed, and are now
`RuleTests`' "What the feed does not promise": a row is past the watch's
bookmark only once the engine took it, a kind the watch gains is read
again once the watch that follows it is open, and a card asked for is
kept from that moment. Two helpers of the scripted servers wait on the
real clock, 5 ms at a time, for a test's own word to let a held answer go
(`ChatServer.hold`, a stream's `holdAfter`): they poll for the test, and
no outcome depends on how long they took.

- Unit tests (`mise run apple:test:chat`, on the Mac; ChatKit's also on
  Linux, `mise run apps:test:linux`): the transcript fold
  and the stream's rows against the console's cases (pairing by id, orphans,
  compaction, reasoning), ndjson and watch lines (heartbeats, garbage,
  terminal frames), replay dropping by set membership, the Markdown parser on
  every prefix of a document, previews, titles and day groups, the tool words
  and path encoding.
- The list's rules against a scripted server (`RuleTests`, by the rule's
  number), and one turn's fold (`FoldTests`): text placed by position in
  any order and twice; a stream joined again at every point, with positions
  and without, which never shows less; events then rows and rows then late
  events; and, at every point of a run's stream and every prefix of its
  rows, a transcript with nothing doubled, nothing lost and every id as the
  stream first gave it. Each `cutBy` says so at the turn the stream drew.
  A message sent under an id is the row of that id and no other, whatever
  its words; one sent under none is the row its run came after, whatever
  older row holds the same words and whatever was held when it was sent,
  and is named for good only once its own stream has said so or its run
  has ended.
- The stream (`StreamTests`): a return to the front at revision 5 and
  below it empties nothing and the reply goes on where it was; a turn that
  settles between an attach's read and its stream; `lagged`; the quiet a
  stream may keep (25 s and 75 s); a resumed run asked for its stream at
  revision 5 and read below it; what the composer says from the send to the
  end; and every piece of a reply keeping its identity to its stored row.
- Pacing and following (`RevealTests`): what is drawn is never further
  behind than the lag and never moves back; the presenter asks for each
  step and ends on the whole text; Reduce Motion draws each piece as it
  arrives; the follow rule's every move; Jump to Latest's count.
- The handover as it is drawn (`apple/Tests/HandoverTests.swift`, the
  app's own rows in a window against the fixture's server, one event at a
  time, paced and not): from the first piece to the stored rows the
  transcript's height never shrinks, no row is torn down, each turn's text
  is never taken back, an attach empties nothing, and an opened Thinking
  section and tool row stay open. `FollowTests` holds the real
  conversation view: at its end before the app was away and after, with a
  reply grown meanwhile; scrolled up, it stays, and Jump to Latest says how
  many messages are below and goes there; a wheel turned a little at a
  time, and fingers that pause on a trackpad, are the reader's until they
  rest. `OpeningTests` holds the seam the "New" line and the read mark
  stand on, with rows of a known height in the scroll view itself: a chat
  opens with its line at the top and is not at its end, whatever arrives
  below, until its reader comes down and rests; from there what arrives is
  followed and looked at with no scroll; a line that comes after the rows
  takes the chat to it, once; and where what is new fits on one screen
  the chat is at its end from the start.
- A reply as it is written (`MarkdownClosingTests`, `MarkdownRowsTests`):
  at every prefix of several replies, what is drawn is the start of what
  the whole reply draws, with no marker in it the reply does not show; it
  never shrinks; its code is the whole's to the letter; and a finished
  block never changes kind, nor anything inside it. The same prefixes
  parsed as they stand fail that, which is how the test is known to see,
  and the app's own tests hold the same through the renderer the app draws
  with (`StreamedMarkdownTests`). Marks in any order never stop the parse.
  Only the last row changes between two flushes; the streamed path ends on
  the blocks of a one-shot parse; pages never move; a reply is parsed once
  for its text, once a window, and never more than 30 times a second
  whatever else presents the tree.
- A reply's code (`CodeHighlightTests`, over `CodeFixtures`, a sample of
  each language with what it lets run over lines and what is easy to
  colour wrongly): each sample's colours, token by token, as they were
  read by eye once; what is left plain; what runs over lines closing
  where it should (a brace in a string inside a template's interpolation,
  a here-document under a quoted word, an attribute's value over two
  lines); a diff by its hunk's counts; and,
  at every prefix of every sample, that colouring from the lines kept is
  what reading the prefix whole makes, that every finished line is
  coloured as it stays, and that the runs are in order, inside one line
  and never cut a character. A block of 400 lines written seven
  characters at a time is read a line at a flush. A reply with code in a
  list and a quote is coloured from its last parse as a one-shot parse
  colours it, at every prefix.
- What stands under a reply (`ReplyActionsTests`): for every cause of the
  closed set, the run again is offered on the card, beside it or nowhere,
  against a table that must name every cause, and never in two places;
  where the row stands (not under a reply with a message after it, not
  moving while a call only runs tools, under the last turn alone for a
  run that said nothing, the person's message included); Copy alone
  while a reply is written. The seeded
  runs hold the same after every step (`offered`). A table's layout and
  the wrap rule are numbers (`TableLayoutTests`): what fits, what wraps
  to fit, what scrolls and how wide the held column is, cards at the
  accessibility sizes, and that a growing column moves no other while the
  table fits.
- As drawn (`apple/Tests/ReplyPartsTests.swift`, the app's own views in a
  window, read from the accessibility tree): the switch to wrap on the
  blocks with a long line and on no other, and none in a room wide enough;
  every cell of three tables in the grid and in cards, and a table with
  no row yet as its headers in both; a table narrower
  than, as wide as and wider than its room; the row under a reply through
  a whole run (Copy from the first words, Share once it is whole, one of
  each, the content never shorter, and the settled reply as tall as the
  frame before); Try Again once, on the card or under the reply, for
  every card of the table of causes, and under the person's message
  where the run ended before any reply; code made into coloured text a line
  at a time equal to making it whole; and the text opened for selecting
  holding every word the reply draws and no Markdown marker.
- What draws what (`PartsTests`, `DraftTests`): a delta makes its own
  conversation's screen and live turn again and moves that conversation's
  part alone; a state that moved makes again only the parts that read what
  moved; the list is kept until the instant a row stops being true; a
  tool's row is made once; and no key is lost by a send, a refusal, leaving
  the front or switching chats.
- A message under its own id (`SendIDTests.swift`): the bubble's id is the
  stored row's across the handover; no id below revision 3, with the
  delivery check as before; the folder holds the message before its
  request reaches the server; a lost answer is repeated under the same id
  and arrives once, on a new chat as one chat; three repeats at most, none
  past the window, then the card; a repeat the server already answered
  reads the thread; the only copy refused gives the text back and a
  refused repeat stands, for each reason; an id that names another record
  is said and never answered with a new one; a kill mid-send stands or is
  the stored row at the next launch; Stop and a return to the front never
  drop an unanswered message; a write asked for earlier never lands over
  a later one, and a stop between the two files leaves a text in one of
  them whichever way it moves; a new chat is the thread its lost first
  copy opened once that thread is read, gives way to a conversation
  on it that holds something, and keeps both texts where the folder holds
  a draft for that thread; a write the folder refuses is said in the
  strip, tried again on the list's waits and unsaid once one lands or the
  server has the message; an id taken is read from the server's reason,
  and from its words only where it names none.
- A server that misbehaves by a seed (`SeededServer.swift`,
  `SeededRun.swift`, `SeededTests.swift`). `SeededServer` keeps the truth,
  the threads and rows a correct server would hold (a message under its
  id written once, a repeat answered as an attach is, a continue that
  writes nothing), runs its turns only when the test moves them, and
  draws each request's fault from the seed: a 500, a 503 and a 429 with
  and without `Retry-After`, a gateway's 502 page, a 401, a hang past the
  timeout, a request that never arrives, a dropped connection, a page of
  HTML with a 200, a stream that closes before its `thread` event or is
  cut at any event, an event sent twice, a delay; a write is taken or not
  before its answer is lost. Two seeds in five draw a server at
  `agents` 5: each piece of a reply says where it belongs (`at`), which
  below 5 none does; a reader is cut off as `lagged`; and at any revision
  the server may restart under a running turn, which ends every stream of
  it with no word and leaves the run with no stream (`idle`, `running`),
  given one a moment later from 5 and never below. `SeededRun` is a person by the same seed:
  sends, Stop, Try Again and Continue, Send Again and Edit Message,
  opening and closing chats, leaving and returning to the front, kills
  and relaunches over the same folder, with the server's turns and the
  rig's clock moved between. After every step it holds the app to: the
  transcript drawn equals the fold of the server's rows wherever the app
  holds the thread at the server's version; no conversation waits with no
  request at the server about it and no wait on the clock of its own (read
  from the effects it asked for, and counted, so another conversation's
  retry does not keep a dead one alive; believed once it has stood so
  through 100 s of the clock, and at the end of every run with no
  allowance at all); no message is written
  twice; no row is drawn twice; nothing the person sent is lost (it is in
  a stored row, in a composer or on a message with its card); no failure
  is drawn in two places; a pending message has its send or its card;
  without an id nothing is sent again on the app's own; the list holds
  each chat once. And one fold's rules, on what each conversation draws
  against what it drew a step ago: the text of a turn is never shorter
  (but where a server that says no positions sent a piece twice, which no
  reader can tell from a word said twice); a row keeps the id its turn was
  first drawn by, a reply's by its turn's ordinal and a message sent under
  an id by that id and no other; nothing drawn of a run goes while the run
  goes on, an attach least of all; and no reply's words are drawn twice.
  At the end the server behaves, time passes, and every
  conversation must have settled on the server's rows. A broken rule
  prints the seed and the steps, and
  `CHATKIT_SEED=<seed> CHATKIT_STEPS=<n> swift test --filter SeededChat`
  replays it. Every `swift test` runs 24 seeds of 60 steps;
  `CHATKIT_SEEDS`, `CHATKIT_FIRST_SEED`, `CHATKIT_STEPS` and
  `CHATKIT_WIDTH` run more, and `CHATKIT_VERBOSE` says what each seed
  reached. The seed fixes everything, the order the app's concurrent
  work runs in included: a run is made on a thread of its own
  (`SeededThread.swift`), which takes every job the run enqueues, through
  the Swift runtime's enqueue hook, and runs them one at a time in the
  order they were enqueued, and between steps the run waits for that
  thread to have no job left. That is exact and takes no time on any
  clock, so a seed takes the same steps and ends on the same rows on a
  Mac and on Linux, alone and on a starved host, and
  `aSeedRunsTheSameWayWhateverRunsBesideIt` holds it. It holds for work
  woken from the run's thread, so nothing in a seeded run may wait on real
  time, a dispatch queue or the main actor. A run says where it is at
  every place it can look (a request reaching the server and its answer
  going back, a wait on the clock beginning and ending, an id minted, a
  stream's cleanup, each step of the driver), and one met off its thread
  there breaks a rule of its own. That is a net and no proof: a job that
  left the thread and passed none of those places is not seen. The suite
  leans on that hook, `swift_task_enqueueGlobal_hook`, which is the Swift
  runtime's own and no public API. It was run on Swift 6.4.2, Apple's on
  macOS 27 and swift.org's on Linux (the `swift:6.4` image
  `apps:test:linux` pins). On a toolchain that has dropped the symbol, or
  kept it and stopped calling it, every seeded test fails with a sentence
  that says so (`SeededThread.Unordered`): none skips, and none runs on
  the pool instead. `everyJobStartedOnItRunsOnItInTheOrderItWasEnqueued`
  holds the hook to what the suite needs of it, for the kinds of work it
  starts; a runtime that honoured the hook for some enqueues and not for
  others could pass it, and would be caught only where the run is met.
  A rule the app is known to break is listed in `SeededRun.known`, noted
  and not failed, and an entry that no longer fails must not stay. It is
  empty. Its first four were the two models of a turn, gone with them;
  its last three each have a plain test beside the seed that found them:
  a message sent under no id paired with an older row of the same words
  (seed 1171 at 101 steps on a server at `agents` 7; `FoldTests`), a
  message a call had read drawn queued again by a chat that joined its
  run anew (seed 9435 at 39 steps; `AnsweringTests`), and a reply's
  notification standing for an archived chat with nothing waiting (seeds
  30167 at 45 steps and 20442 at 101; `AnsweringTests`). A kill may take
  a draft typed inside its save's pause and a send without an id still on
  its way, and nothing else.
- The diagnostics file by a seed (`SeededRun.discreet`). The seeded
  server's every sentence is a canary (`SeededServer.said`: an envelope's
  message, a thread's reason, a failed turn's error), and so is every
  message the person typed, every reply the server wrote, every id of a
  thread, a row and a card, the server's address, the repository's name,
  the agent's name and the token. At each kill and at the end of every
  run, every line the launch wrote is read from the folder: none may hold
  a canary, and each must have the shape of a line (when, a chat by its
  number, lower-case words, then `name=value` with a number or one word),
  which no sentence has. At the end Share Diagnostics is opened, and what
  it would share must be the folder's every line. `DiagnosticsTests` holds
  the builder against words outside its sets, the ring's bound and its
  life across a launch and a sign-out, and one run's whole story.
- Read state (`MarkTests.swift`, over `MarkServer`, a server that keeps
  marks by the real one's rules for any number of devices at once): the
  unread rule case by case; the mark moves after a second with the end on
  screen, and not when scrolled away or in a window that is not in front;
  a read that could not be written against one made elsewhere since; a
  mark that outran the list it was read with;
  the two seconds before the write and what sends it early; two devices
  where a read on one clears the other's dot, takes back its notification
  and lowers its badge; the five seconds a reply's notification waits; an
  older mark arriving late; Mark as Unread and Mark as Read; a write that
  fails and one refused for good; the waiting mark from the thread; the
  "New" line; a server without marks, one that does not know the kind and
  one that stops keeping them; the first launch after the server gained
  marks; the offer to notify; and what the seeded runs found, each as
  the engine alone (a chat under the screen that says the sign-in ended,
  the first marks taking back what the device's map notified, a mark
  ahead of the row held, with a list read from before it landing after
  it and a thread that is in no list, a row dropped and brought back
  read, a failed call that is not unread).
- Answering (`AnsweringTests.swift`), on the rig against the seeded
  server with no faults: a message sent while a reply is written is
  queued until a call reads it, from this device and from another, several
  in order, through a stop, a failure, a refusal, a lost answer and a
  server below `agents` 6; a card stands while it waits and is its call's
  mark once it ended, here or on another device, and a write that failed
  stays in the card with Try Again; an answer in the person's own words is
  an ordinary message and the question is dismissed once the server holds
  it, at `agents` 3, 5 and 6; and each action of a notification, through
  the recorded notification service: a reply sent under an id and sent
  again when its answer is lost, an approval over the version shown, one
  that changed since refused and said, a question's option, an action
  heard twice, one heard before the folder was read, one that outlives its
  time, and none offered below `agents` 3.
- Answering by a seed (`SeededRun`, `SeededServer`): two seeds in five
  draw a server at `agents` 6, which takes a message into a running turn,
  says `messageQueued` and `messageTaken` on every stream of it, and ends
  a run with one unread each way (on its own, starting the next turn;
  stopped; failed). Its turns call tools that ask a question or wait for
  an approval, and another device answers, dismisses, approves, declines
  and sends at any step. The person also sends while a reply runs, answers
  in their own words, taps an option, approves, declines, tries a card
  again, and answers from a notification. After every step: a message is
  drawn queued only while the server holds it unread in a run that goes
  on, and never twice; a card the server holds resolved is never drawn
  waiting once the app holds the record at that version; a card that
  waits is never hidden, but aside while it is answered in words; no card
  is resolved twice on the server; and an action from a notification is
  over, with the device told, within its time.
- Answering at `agents` 7 (`AnsweringTests.swift`): a reply in words names
  its questions, nothing is dismissed and one turn follows; questions
  resolved elsewhere meanwhile stay as they were; a reply whose answer is
  lost is sent again and resolves once; a card another person ended says
  "Someone else", one below 7 says only how it ended, and whose hand it
  was is concluded from the server's answer alone. The seeded server
  takes `repliesTo`, links who resolved a card (`me`, or another person,
  which its other-device step now draws), and answers the read of the
  person's own. Two rules beside the others: at 7 a question is dismissed
  only where Dismiss was pressed (or its words left before the app had
  read the server's document) and a question replied to resumes nothing;
  and a card says "You" only where the server links it to the person the
  app is signed in as. `CHATKIT_REVISION=7` runs every seed at that
  revision, which a tenth of the seeds draw on their own.
- Answering beside the list (`AnsweringTests.swift`, "Answering in a list
  that archives" and "A turn that ends on a card is one notification"):
  an archived chat that comes to wait on a card is listed, counted and
  notified for with no list refresh, and once its card is answered from
  the notification it is in no list, count or notification again; a
  message sent into an archived chat's running turn, an answer in the
  person's own words, and a reply from a notification into a chat archived
  on another device each end the archive, and none is held for it; the one
  notification of a turn that ends on a card, whichever of the card and
  the row is read first, held no longer than it may be, and not standing
  for a later reply; a card's notification kept through a read and taken
  back once nothing waits; and the predicate. The seeded server counts
  its cards on the thread's row (`waitingOnPerson`) and tells the feed
  when the count moves, so the list's steps and rules and these run over
  one server.
- Read state by a seed (`SeededRun`, `SeededServer`): the seeded server
  keeps marks as #1405's does where its `records` revision is 8 (a sixth
  of the seeds draw 7, a server without them), names `lastMessage` as
  #1419 does, and tells each mark that moved on its change feed. The
  person also scrolls to a chat's end and away from it, presses Mark as
  Unread and Mark as Read, and has another device that reads a chat or
  marks it unread at any step. After every step: a dot that went for a
  message the server holds read, with the app holding it read, is drawn
  again only once somebody marked the chat unread; every write of a mark
  names a message whose chat stood open at its end, in front, settled,
  with its rows drawn and handed back by its conversation, for a second
  of the run's clock, or one Mark as Read was pressed on, and
  nothing is written to a server without marks; the badge is the number
  of chats drawn unread, and each is drawn as its mark and its newest
  message say; no reply's notification stands, waiting or shown, for a
  chat the app holds read. A kill takes what the list drew read, since
  the launch after it draws from the folder first.
- The list (`ListTests.swift`, over `MarkServer` at `records` 9, which
  keeps the two flags, narrows a list by the marks that point at its rows
  and cuts every list into pages): the window grows by the cursor and
  lists no chat twice; a chat that moves keeps a grown window whole from a
  read of its first page; a page asked for before the order moved loses
  none; more chats moved than a page holds start the window over; a row is
  made once and a page makes its own rows alone. A transcript's older rows
  arrive above the ones held, a page a read, and stay through the next
  read of its newest. The four lists merge by the thread; Pin and Unpin
  show at once and reach the mark through a write that failed; Archive
  takes the row and Undo puts back the very sections; Undo stands six
  seconds; an archived chat that comes to wait is listed and leaves again
  once answered; a reply in an archived chat notifies nobody and counts
  nowhere; archiving takes back what a chat notified; speaking in an
  archived chat ends its archive; the archived list is read when shown
  and grows; a chat archived or unarchived on another device, on the
  first page and past it; every chat archived and one coming back; a
  write that races the list reads; a flag refused for good; a flag still
  to be written when the server stops keeping them; what a chat notified
  taken back by its mark read by name, and never by its row going
  missing; the archived list going on under Automated Runs; the lists as
  what says which chat is which until a mark is read. Below revision 9
  nothing of it is offered or sent, and a server that stops keeping the
  flags is read as one that never did. A search's titles at once and its
  messages after the pause, by chat with the words that matched; one that
  fails, in the strip with the titles kept; a server that takes none; a
  hit opening its chat at that message, the rows before the ones held
  read to reach it. A window's first screen, each of its rules.
- The list by a seed (`SeededRun`, `SeededServer`): three in four of the
  seeded servers that keep marks keep the flags too (`records` 9), with a
  thread's `waitingOnPerson` moved by the run and told on the feed, and
  the list is read two, three or four chats a page, so a handful of chats
  is several pages. The person also pins and unpins, archives, presses
  Undo, opens the archived list and unarchives there, scrolls a list to
  its end and searches; another device of theirs moves a flag; a chat
  comes to wait on them, and is answered. After every step: an archived
  chat is drawn, counted or notified for only while it waits; a pinned
  chat the app holds is in the pinned section and nothing else is; a chat
  is in one section; a flag is written only where the person set it, or
  spoke in a chat they had archived, and never to a server that keeps
  none; Undo puts the chat back, and pressed at once the very row at the
  very place; a search draws each message under the chat that holds it.
  At the end the list is pulled and scrolled to its end, and must draw
  every chat the server lists, once, the pinned ones in their section,
  with each chat's flags as the server holds them, and no notification
  may stand for a chat the server holds archived with nothing waiting.
- Failures (`FailureTests.swift`, `FailureFallbackTests.swift`): every
  cause, refusal reason, cut and attempt has its sentence and its button
  asserted against a table that must name every case; each attempt lands in
  its place; a failed stop never reads as a failed reply; Continue falls
  back below the revision that has the route and never probes it; the
  `error` event's codes; the countdown, and one automatic retry at
  zero only while the chat is on screen; nothing typed is lost; the colours
  meet 4.5:1. `apple/Tests/GalleryTests.swift` draws each place over
  `FailureFixtures` at an iPhone's and an iPad's size, in light and in
  dark, for `mise run apps:gallery`.
- A run against the dev substrate (`mise run dev`) with a model stub: sign
  in, the list, Markdown, streaming, a tool call, a question answered and the
  agent resumed, Stop, a new chat, Settings.
- Pictures (`mise run apps:gallery`, into `build/gallery/apple/chat`): a
  conversation's transcript drawn by the app's own rows through its own
  runtime against the fixture's server (`GalleryTests`), in light and in
  dark: a settled reply with every block at its whole height, a reply that
  links records (each its pill inside the line, in a paragraph, a list and
  a table), and a reply caught as it is written, part way through an
  emphasis, a table and a fenced block. And what stands in and under a
  reply (`GalleryTests.parts`), each at an iPhone's width and at the
  width an iPad's transcript keeps: the row under the last reply settled,
  being written and with Try Again beside a card; every language's sample,
  two to a picture; a block with long lines scrolled and wrapped; and the
  tables as a grid and as cards (a Mac has no Dynamic Type, so the cards
  there are the layout at the usual letters; the largest size is a
  simulator's to show). The tests run inside the app, which
  the Mac sandboxes, so each picture is attached to the test's result and
  taken from there.

### Measuring

`mise run apple:measure:chat [mac|sim|all]` builds the app with the
optimizer on and runs it with `SUBSTRATE_CHAT_MEASURE` set, which a debug
build reads at launch (`apple/Sources/Measure`; a release carries none of
it). The app then signs in to nothing: its session is in memory, its folder
a scratch one, and its server is `FixtureServer`, which answers in the
process from ChatKit's `ChatFixtures` (a list of 100 chats, a chat of 200
turns, a chat of 500 rows, the change feed held open) and writes a 16 KB
reply 400 characters a second. The scenario is the same every run: the
reply streamed into the chat of 200 turns, then into the chat of 500 rows,
then 200 characters typed into the composer through the field's own text
system, 80 ms apart. It prints one table and ends. Above the table it says
what opening each chat cost: how many turns' bodies ran (a lazy transcript
runs the ones on screen, not 500) and the main thread's time.

The figures come from the kit's `Meter` (see the kit's README), each a
signpost as well, so the same run under Instruments draws them. A step is a
flush of the stream, or a key typed:

| Row | What it is |
| --- | ---------- |
| main thread | every pass of the main run loop from waking to waiting again, summed, per step; and the longest pass |
| presenter | each controller's presenter, by its type: time per step, the longest run, and runs per step |
| flush, publish | the tree presented and sent, on the runtime's executor; the half of a publish the model does on the main thread |
| body evaluations | how often each view's `body` ran, per step: the list, a list row, the transcript, a turn, a row of a reply's text and what stands under it, its Markdown, a Markdown block, the composer |
| Markdown | parses per step and what one took, and what making a reply being written into blocks took in all, both on the runtime's executor; lines of inline Markdown made into styled text on the main thread |
| code | code blocks read for their colours per step and what that took, on the runtime's executor (every block of the reply at each parse, each finished line from the lines kept); and what making a block's coloured text took on the main thread |
| key to drawn | from the key to the end of the run loop's pass it was pressed in, which has drawn it |

The numbers are a machine's: the table's first line says which, and a
simulator is not a device. The counts (runs, evaluations, parses per step)
are the same on any machine.

### Looking at a state

A debug build started with `SUBSTRATE_CHAT_DEMO=<state>` signs in to
nothing, as a measured one does, and shows one chat of the fixture's server
held at that state: `connecting`, `thinking`, `writing`, `tool`,
`answering`, `reconnecting`, `done`, `long` (the fixture's chat of 200
turns, to scroll up in for Jump to Latest), `deep` (a chat of 1,100 rows:
the fixture's server answers the window a read asks for, so its older
rows are read in above the newest 500 when its top is reached, with no
server), `play` (the whole run, at
the fixture's pace) or `reply:<name>` (a chat that holds one settled
reply of the fixtures: `week`, `short`, `lines`, `tables`, `table`, or a
language's name for its code sample). And how a person answers:
`question` and `approval` (a card that waits), `words` (the question
being answered in the person's own words), `folded` and `approved` (each
once it ended), `queued` (a message a run has not read, with more typed)
and `taken` (the same once a call read it), and `notify` (the app's three
notifications that take an answer, posted through the device's own
service; `notify-reply`, `notify-question` and `notify-approval` post
one, for its banner to be pulled open). The state can be given as a
launch argument too (`-SUBSTRATE_CHAT_DEMO <state>`), which is what a
tool that launches the app on a simulator can pass. It is how the
screenshots of a state that lasts a moment are taken; a release carries
none of it. The run is
`ChatFixtures.run(thread:)`, the lines a server at `agents` revision 5
sends and the rows it holds after each, which `FixtureServer.script` serves
one step at a time for the tests above.

A debug build started with `SUBSTRATE_CHAT_SESSION` set to a server's
address, a repository's name and a token, with spaces between, is signed in
to that server from memory, with a scratch folder. It is for a run against
a dev substrate on a simulator, where the browser's sign-in cannot be
driven; a release carries none of it either. With
`SUBSTRATE_CHAT_SESSION_NOTIFY` set it notifies through the device's own
service, so an answer given from a notification reaches that server.

## What the reviews changed

- The full event and exit tables (`truncated`, `error` after the 200, a
  settled `idle`, refusal statuses, permanent attach failures, drains).
- The store's rules: UUID-keyed conversations bound by `thread`, one per
  thread, read, run and session epochs, no rows under an overlay (which
  one fold of a turn has since replaced, rule 3).
- Freshness by thread `version`, never `updatedAt`; message rows on the feed.
- The feed's handoff from the list's `head` and `generation`, and its
  terminal frames.
- Unknown delivery on a new chat (adopt by the agent's newest chat), and same
  text called evidence.
- iOS notifications through a continued processing task, with the refresh
  task as a backstop and honest copy; Reply from a notification moved to
  Later (built since, on message ids); notification and unread keys on `finishedAt`, not `updatedAt`.
- The chat list reads `mode eq chat`; automated runs behind a toggle;
  prefetch cut to 8 chats of 100 rows.
- No actor header: `console` would be a false attribution.
- Glass only on the control layer; keyboard specifics; voice pitfalls;
  accessibility for streaming text; file protection and backup exclusion;
  the pending-card reads; the provider key's handling.

## What the code reviews changed

Both reviewers read the first implementation against this design, the
console and the server.

- A conversation owns its thread while a handover read it is waiting on has
  failed, and the next move of the thread retries it.
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
