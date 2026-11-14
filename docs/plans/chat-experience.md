# Plan: a chat that keeps its place, streams evenly and fails in plain words

Status: proposed 2026-11-14. A plan, not a contract, and nothing in it is
built. It covers the agent API as a chat client uses it, and Substrate Chat,
the iPhone, iPad and Mac app ([its design](../../apps/chat/DESIGN.md)). It
follows [detached chat turns](chat-turns.md), which landed, and sits beside
[what Moss needs next](moss-capabilities.md), which owns attachments,
schedules and documents and is not repeated here.

It was written from a read of the server and the app as they are on `main`
at v0.123.1, and from a survey of how messaging protocols and assistant
products solve the same problems. The public sources are listed at the end.

## The short version

Four things the owner notices today, each with its cause further down:

1. Reading a chat on the phone does not mark it read on the Mac, and the
   app knows only that a chat changed, never how far the owner read.
2. A reply arrives in bursts, and the half-written text disappears and
   comes back whenever the app returns to the front.
3. A failure is the server's own sentence on a red card with no button.
4. The interface lacks the small things a chat is judged by: actions under
   a reply, code and tables that read well, a list that says what is
   running, motion that explains what changed.

Ten changes answer them. Sizes are S, M and L as in the other plans.

| # | Change | Side | Size | What the owner gets |
| --- | --- | --- | --- | --- |
| 1 | How far the owner has read is a record | server, app | M | Read on one device is read on all; dots, badges and notifications agree; a line where the new part starts |
| 2 | A stream a client can join twice without a blink | server, app | M | No blink on return; an answer after an approval streams; text is kept when a provider drops the call |
| 3 | A failure with a cause from a closed set | server, app | M | One sentence, one button, on every failure |
| 4 | A send that carries its own id, and a continue that writes no message | server, app | S | No "it may not have arrived"; no words put in the owner's mouth |
| 5 | Small facts a chat list reads from the thread | server | S | A list that says who is waiting on whom, from one read |
| 6 | A message sent while the assistant works | server, app | M | The owner corrects course without stopping the run |
| 7 | Push to a closed device, through the app publisher's relay | server, app, relay | L | A reply or a question reaches a phone in a pocket |
| 8 | Even streaming, plain failures and a fuller interface in the app | app | M | The app reads as carefully as the answers do |
| 9 | Tests that hold all of it | app | S | The polish does not wear off |
| 10 | The app's structure: draw only what changed, one model of a turn, failures kept as values | app, kit | M | Typing and streaming stay smooth in a long chat; the fixes above stop being patches |

[The order](#the-order) puts the app-only work first, because it needs no
server change and removes the blink, the bursts and the button-less
failures within one release.

## What holds

The proposal stays inside the substrate's premise. These are the rules it
was checked against:

- **Records are the truth.** How far the owner has read is the owner's
  data, so it is a record in the changelog, not a file on one device. This
  is the rule the console's preferences already follow: what describes the
  person follows the person, what describes one window stays in that window
  ([0132](../decisions/0132-console-preferences-follow-the-person-and-a-window-fact-stays-in-the-browser.md)).
- **One owner.** There are no other participants, so nothing here adds
  presence, typing indicators, delivery receipts or a per-person read
  state. A read position is one value per thread.
- **A link is a reference.** Every new field that names a record is a
  reference to one kind
  ([0044](../decisions/0044-a-reference-is-the-only-link-between-records.md)).
- **The turn is the server's.** A chat turn runs detached in the serving
  process with an in-memory hub
  ([0169](../decisions/0169-a-chat-turn-runs-detached-from-its-client-and-its-thread-is-attached-by-id.md)).
  That stays. No stored event stream and no second transport is proposed:
  the stream stays ndjson and every change to it is an added field.
- **The app speaks only the API**, and the console's chat is its
  reference. Every server change here serves the console too, and the
  console should take changes 1 to 4 in the same pull requests.
- **The client owns the words.** The server says which failure happened,
  from a closed set. The sentence the owner reads is the client's.

## Where chat stands

### Reading and notifications

Moss answers while the phone is in a pocket. The owner reads the answer on
the Mac. An hour later the phone still shows the dot and a badge of 1, and
at its next background refresh it posts a notification for the reply the
owner already read.

- The app's read marks are a map from thread id to the thread's
  `finishedAt`, kept in the device's `state.json`
  (`apps/chat/ChatKit/Sources/ChatKit/Local/CachedState.swift:23`). A chat
  is unread when the two differ (`ChatApp/ChatAppReading.swift:142`). The
  set of notifications already posted sits beside it, also per device.
- No kind holds read state. The chat-turns plan put it out of scope as "a
  client's own state".
- The mark is per thread. The app knows a thread settled since it was last
  seen, not which message the owner stopped at, so it cannot open a long
  chat at the first new message.
- The server has no push channel. The app posts local notifications from
  what it observes: the change feed while it runs (paused in the background
  on iOS), a conversation's own stream, and a background refresh that iOS
  runs when it chooses.
- A failed reply's notification body is the thread's `reason`, the
  server's sentence, unedited (`ChatApp/ChatNotifications.swift:22`).
- Permission to notify is asked right after the first message is sent
  (`ChatApp/ChatAppEngine.swift:755`), which is while the first reply
  streams.

### Streaming

The owner sends a question and switches apps for five seconds. On return
the half-written reply disappears, then reappears whole. When the reply
finishes, the open "Thinking" section snaps shut and selected text is
dropped. A table shows as lines of pipes until its second line arrives.
After the owner approves a tool call, the answer that follows does not
stream at all: the chat says the assistant is working in the background and
the text appears when it is done.

- An attach replays the turn from its start, and no event says where its
  text belongs. So the client empties what it shows before the replay
  lands (`Conversation/ConversationEngine.swift:40`), on every return to
  the front (`ChatApp/ChatAppEngine.swift:380`). The console does the same
  (`web/console/src/components/agent/conversation.tsx`, `openAttach`).
- A live row's id is `live-a<n>` and the stored row's id is the record's
  (`Model/LiveOverlay.swift:25`), so every view is replaced when the stored
  rows arrive.
- The whole reply is parsed again on every flush, in the view's body, and
  blocks are identified by their index
  (`apps/chat/apple/Sources/Markdown/MarkdownView.swift:35`). Text is drawn
  as it arrives, in pieces the size the network delivered.
- Following the end of the transcript fires on a hash of the screen
  (`apps/chat/apple/Sources/Views/ConversationView.swift:152`), so a height
  change outside that hash is not followed, and a send made while scrolled
  up does not go to the new message.
- A continuation after an answer, a decision, an approval or a background
  job runs with no hub (`internal/engine/agentdecision.go`,
  `continueThread`). An attach answers `idle` with `running`, and the app
  reads the thread every 5 seconds until it settles.
- A model call that fails after streaming text settles the thread with an
  empty reply (`internal/engine/agentloop.go:1039`). Only a stop keeps what
  streamed (`:991`).
- The provider's stop reason is not kept. A reply cut at the model's
  output limit settles `ok` and reads as finished.
- The server writes a heartbeat every 30 seconds and the app calls a
  stream dead after 75 seconds of silence, showing nothing meanwhile.

### Failures

Anthropic rate-limits a request. The owner reads a red card, "Anthropic is
turning requests down", then "The server said" over a line that begins
`substrate: validation failed: agent …: llm: …`. There is no button. The
owner types the question again.

- A failure after the stream opened is `{"kind":"error","error":"<text>"}`
  (`internal/api/agents.go:187`). The same shape carries a failed turn, a
  client that read too slowly, a sign-in that ended and a panic.
- The thread keeps `status: error` and a free-text `reason`.
- The API's refusals have a closed set of codes and four reasons
  (`internal/substrate/errors.go`). None names a provider's rate limit, an
  overloaded provider or a missing key in chat. A busy thread and the
  four-turn cap are both `409 conflict`. A spend cap is `403 guard` with no
  reason.
- So the app searches the vendor's prose for words such as "rate limit"
  and "invalid api key" (`Presentation/TurnFailureWords.swift:35`).
- The failed-run card has no action. A Stop that fails shows "couldn't
  answer" while the turn still runs (`ConversationEngine.swift:101`).
- A send whose answer was lost cannot be repeated safely, because nothing
  identifies it. The app compares message text to guess whether it arrived
  and otherwise says "It may not have arrived".
- Continue sends the words "Continue where you left off." as the owner's
  own message, which then stays in the transcript.

### The interface

- A reply's actions are behind a long press (Copy, Share). There is no
  retry.
- A code block has its language and Copy, no highlighting and no wrap
  switch. A table has no column sizing and does not scroll sideways.
- The list has no pin and no archive, and its search reads only the
  transcripts the app has in memory. It loads the first 100 chats and a
  chat's newest 500 rows, with no way to go further.
- Rows appear and the handover happens with no motion. Loading is a bare
  spinner.
- About two dozen fixed sizes do not scale with the text size.
- A problem with the change feed is kept in state and never drawn.

## The server

### 1. How far the owner has read is a record (M)

One record per thread says what the owner has done with it, apart from
what the engine writes about the run. Its id is the thread's id, so every
client finds it without a search.

```yaml
kind: substrate.reamde.dev/llm/threadmark
metadata:
  id: 8f2c41d07a                # the thread's id
data:
  properties:
    thread:
      ref: substrate.reamde.dev/llm/thread/8f2c41d07a
    readThrough:
      ref: substrate.reamde.dev/llm/message/5b90e3c1f2
    readAt: "2026-11-14T09:41:07Z"
    markedUnread: false
```

| Property | Holds | Points at |
| --- | --- | --- |
| `thread` | the thread these marks are about | one `llm/thread` |
| `readThrough` | the newest message the owner has seen | one `llm/message` of that thread |
| `readAt` | when that was | a time |
| `markedUnread` | the owner chose Mark as Unread | a bool |

The thread gains one property, `lastMessage`, a reference to the newest
`llm/message` the run wrote, set in the settle patch the engine already
makes. With it, a list decides unread from two records and no message
read: a settled thread is unread when `markedUnread` is set, or when its
`lastMessage` is not the mark's `readThrough`.

The rules:

- **Forward only.** A write whose `readThrough` is an older message than
  the stored one lands nothing and is answered with the stored record.
  Older means a lower `turn`, then an earlier row. The tree already
  refuses a write that goes backwards on observed rows (the
  `stale_revision` reason), and IRC's read marker and Telegram's inbox
  marker hold the same rule: two devices racing can only ever agree on the
  later position.
- **Mark as Unread never moves the position.** It sets `markedUnread`, and
  the next write of `readThrough` clears it. Matrix and Telegram both keep
  the flag apart from the position for this reason.
- **Unread is derived, never stored.** No count is written anywhere. The
  badge is the number of loaded chats that are unread by the rule above.
- **An ordinary write.** A client puts the record at
  `…/llm/threadmark/<thread id>` like any other. The forward-only rule is
  the engine's, at admission, so the console, the app and `substratectl`
  cannot disagree.
- **Devices learn through the change feed.** The kind joins the kinds the
  app watches. A suspended phone learns at its next refresh, or at once by
  [push](#7-push-to-a-closed-device-l).
- **History is read.** One repository migration
  ([0099](../decisions/0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md))
  gives every existing chat thread its `lastMessage` and a mark that reads
  through it. After it, a thread with no mark has never been opened, and
  no client keeps a second rule for old threads.

What was considered and set aside:

- **A property on the thread.** Every read would move the thread's
  `version` and `updatedAt`. The version is the app's test for whether its
  cached rows are current, so each read would make every device read the
  transcript again. `updatedAt` is the list's order, so reading an old
  chat would lift it to the top. The thread is also the engine's row
  ([0195](../decisions/0195-an-app-talks-to-agents-decides-their-requests-and-tails-the-feed-it-reads.md)
  kept writes of it to `subject` alone).
- **One preference record holding a map of threads.** It grows with every
  thread, and two devices reading different chats write the same record.
- **`readAt` on a notification record**, as
  [the Moss plan](moss-capabilities.md) sketches for notes Moss sends
  unprompted. That would be a second read mechanism. A note from Moss is a
  message on a thread, and the mark covers it.

What it costs: a changelog entry per read, debounced to about one per
reply, which is the cost 0132 accepted for preferences. One more kind in
`llm`, one more list read for a page of threads, one more kind on the
watch.

The same record is where `pinned` and `archived` go later. Archive is the
answer to "delete a chat": a thread is a run and its cost counts, so it is
hidden, not removed.

### 2. A stream a client can join twice without a blink (M)

Five additive changes to a turn's stream. The hub stays in memory and the
replay stays bounded, as 0169 decided.

**Text carries its position.** A `delta` and a `reasoning` event gain
`at`: how many Unicode code points of this turn's text came before this
piece.

```json
{"kind":"delta","turn":3,"at":0,"text":"The balcony plants "}
{"kind":"delta","turn":3,"at":19,"text":"need water twice a week."}
```

A client that already shows 30 code points of turn 3 and attaches again
receives the replay from `at: 0` and writes each piece where it belongs.
Text it already shows is written over with the same text, and nothing is
emptied. Tool events are already placed by their call `id`. No resume
cursor is needed: placing by position makes a replay safe to apply twice,
which is what a cursor would have bought.

**Every run of a chat thread streams.** A continuation after an answer, a
decision, an approval or a background job publishes into a hub exactly as
a sent turn does. Attach streams it and Stop stops it. It takes one of the
repository's four chat-turn slots; when all four are taken it waits for
the sweep that already retries resumes.

**What streamed is kept when a call fails.** The text a failed model call
had streamed is written as the assistant row before the thread settles, as
a stop already does. A provider that drops the connection after three
paragraphs costs the owner the fourth, not all of them.

**A reply says how it ended.** An assistant `llm/message` gains `cutBy`,
absent when the model finished the reply: `length` (the model's output
limit), `filter` (the provider withheld the rest), `stop` (the owner or a
shutdown) and `failure` (the call failed). A client offers Continue on a
row that carries it.

**A quiet stream is noticed sooner.** A turn's stream writes its heartbeat
every 10 seconds, not 30, so a client can say "Reconnecting" after 25
seconds of silence. A client that read too slowly gets its own event,
`lagged`, and attaches again; that is not a failure of the turn and should
not travel as one.

Not proposed: a stored event stream. 0169 rejected a queue in Postgres
because a repository has one writer, and that reasoning stands. A crash
still loses the text of the call in flight. Writing the partial row every
few seconds would close that gap at a changelog entry per few seconds, for
text that settles a moment later.

### 3. A failure with a cause from a closed set (M)

The thread gains `cause`, an enum beside `status` and `reason`. The
`error` event and the `done` result carry the same value, with
`retryAfter` in seconds where the provider named one. `reason` keeps the
server's words, which a client shows under Details and nowhere else.

| `status` | `cause` | When |
| --- | --- | --- |
| `error` | `provider_key_missing` | the provider row has no key |
| `error` | `provider_key_refused` | the vendor refused the key |
| `error` | `provider_rate_limited` | the vendor is limiting requests; carries `retryAfter` |
| `error` | `provider_billing` | the vendor account is out of credit or over its quota |
| `error` | `provider_overloaded` | the vendor is shedding load |
| `error` | `provider_unavailable` | a vendor fault, a timeout or no connection |
| `error` | `model_unavailable` | the vendor does not offer the model to this key |
| `error` | `context_too_long` | too long even after the one compaction retry |
| `error` | `allowance_spent` | the operator's daily allowance is used up |
| `error` | `interrupted` | the server stopped during the run |
| `error` | `write_failed` | a write of the run failed |
| `error` | `internal` | anything else |
| `overbudget` | `turn_limit`, `tool_call_limit`, `deadline` | which budget ended the run |
| `stopped` | `owner`, `shutdown` | who stopped it |

Each wire in `internal/llm` already tells one case apart
(`ErrContextTooLong`). The same place sorts a vendor's status and error
code into the other classes, so no client reads vendor prose again.

A refusal before the stream opens keeps its status and gains a `reason`
in the envelope's closed set: `thread_busy` and `turn_cap` on `conflict`,
`spend_cap` on `guard`, `provider_key_missing` on `validation`. A
provider failure on the call route stops answering as `validation`, which
says the caller's input was wrong, and answers `unavailable` with the
cause as its reason.

What it costs: the set is closed, so a new cause is a wire change. A
client that meets a cause it does not know falls back to the sentence for
the `status`, which is also what a thread from before this change shows.

### 4. A send that carries its own id, and a continue that writes no message (S)

`ChatInput` gains `messageId`, an id the client chooses for the owner's
message. The engine writes the user row under it.

```json
{"thread": "8f2c41d07a", "messageId": "c41a7e9b02", "message": "And the tomatoes?"}
```

A request that repeats a `messageId` writes nothing and runs nothing. Its
stream opens with the same `thread` event, then attaches to the turn if it
still runs, or answers the one `idle` line an attach answers. So a send
whose answer was lost is simply sent again, on a new chat as well as an
old one, and the app's bubble and the stored row are one row from the
start. The "It may not have arrived" card and the comparison of message
text go away.

`POST …/llm/thread/{id}/continue` runs a continuation with no user
message: the stored rows are the whole context, the path a resolution
already takes. It is what Continue does on a stopped, over-budget or
cut-off turn, and what Try Again does on a failed one. It is the owner's
word, so 0169's rule holds: nothing resumes a stopped thread on its own.
A thread that is running refuses it with `thread_busy`.

### 5. Small facts a chat list reads from the thread (S)

- `lastMessage`, from change 1.
- `waitingOnOwner`, a count of the thread's questions, suggestions and
  approvals that are not yet resolved, kept by the engine in the writes
  that create and resolve them. Today the app reads three more lists to
  put a mark on a row (`ChatAppReading.swift:149`), and a push sender
  would need the same three.

### 6. A message sent while the assistant works (M)

Today a second message on a running thread is `409 conflict`. Proposed:
it is admitted. The user row is written at once, under its `messageId`.
The loop reads the rows it has not seen before each model call, so the
message steers the run at the next tool boundary. If the run ends first,
the message starts the next turn.

The app shows such a message as queued until an event carries a `turn`
past it. Stop stays a separate act that ends the run and keeps its work.
This is what the coding assistants do, and it removes the most common
reason to stop a run: to say "not that one".

It changes 0169, which admits the next message once the turn settles, so
it needs a record of its own.

### 7. Push to a closed device (L)

A self-hosted server cannot send to a published app directly: the key
that signs an Apple push belongs to the app's publisher. Matrix,
Mattermost, ntfy and Home Assistant all meet this the same way, with a
small relay the publisher runs. The same shape fits here.

Each install of an app registers itself as a record.

```yaml
kind: substrate.reamde.dev/core/device
metadata:
  id: 3d7e0a91c5                # chosen by the app, kept in its Keychain
data:
  properties:
    name: George's iPhone
    platform: ios
    token:
      ref: substrate.reamde.dev/core/token/71ac09be44
    pushKey: "<what Apple's push service calls this install>"
    notifies:
      - replies
      - requests
    seenAt: "2026-11-14T09:41:07Z"
```

| Property | Holds | Points at |
| --- | --- | --- |
| `name` | what the owner calls the device | text |
| `platform` | `ios`, `macos`, `android` or `web` | an enum |
| `token` | the sign-in this install holds; revoking it ends its pushes | one `core/token` |
| `pushKey` | the address the platform's push service gave the install | text, of no use without the publisher's key |
| `notifies` | which events the owner wants on this device | a set of `replies`, `requests` |
| `seenAt` | when the install last registered | a time |

The rules:

- **The relay is named by the app's publisher.** The server posts only to
  the relay that the authority document of the token's verified app names
  ([0209](../decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md)).
  No address is in this tree, an unverified or local app gets no push, and
  a fork of the app names its own relay.
- **The relay never sees content.** A post carries the push key, the
  platform, the event (`reply`, `request` or `read`), the thread's id and
  the unread count. No title and no text. The app fetches what to show
  from the owner's own server with its own token, in the small process
  Apple gives an app to rewrite a push before it is shown, and falls back
  to "Moss replied" when it cannot.
- **A reply the owner is already reading is not pushed.** When a chat
  thread settles, the server waits five seconds. If the mark from change 1
  reads through the thread's `lastMessage` by then, nothing is sent.
- **A read is pushed too.** When a mark catches up, the server sends
  `read` so other devices take back the notification and lower the badge.
  Apple does not promise to deliver such a push, so the app also
  reconciles from the marks whenever it opens.
- **Best effort.** A relay that does not answer is not retried. The
  records are the truth and the app reads them on open.
- **The operator can turn it off.**

What it costs: the relay learns when the owner's assistant finished
something and which device to tell, never what was said. And it is the
first time the server calls an outside service on its own initiative,
which the Moss plan already marked as needing a decision. The console can
use Web Push with no relay, since a browser's push service needs no
publisher key; that is a follow-up.

## The app

### 8a. Reading and notifications

- The mark moves when the newest settled message has been on screen in a
  front window for a second. Writes are debounced by two seconds and
  flushed on leaving the chat.
- A chat with unread messages that do not fit on one screen opens at the
  first of them, under a "New" line. Otherwise it opens at its end, as
  today.
- When a mark arrives from the feed, a push or a refresh, the app takes
  back that thread's delivered notifications and recounts the badge. It
  already does this when the chat is opened on the same device.
- A notification for a reply waits five seconds for a mark, so a reply
  read live on the Mac does not sound on the phone beside it.
- Permission is asked after the first reply settles, with a sentence
  saying what will be notified, not while it streams.
- Notification actions: Reply with text, Approve and Deny on an approval,
  and the options of a single question with three options or fewer. The
  design deferred reply because a background send whose answer is lost
  needed the unknown-delivery path. Change 4 removes that path.
- A failed reply notifies in the sentence for its `cause`.
- A Live Activity for a running turn comes after push.

### 8b. Streaming that reads evenly

Each rule removes one thing the owner sees today. The numbers are starting
values to tune on a device.

1. **One row from the first token to the stored message.** A row's
   identity is its thread and `turn`, live or stored. The stored row takes
   over the same view, so an open Thinking section, an expanded tool and a
   text selection survive the handover.
2. **An attach keeps what is shown.** With `at`, replayed text is written
   in place. Until the server sends it, the app holds the old text until
   the replay is longer.
3. **Parsing leaves the main thread.** One parse runs and the newest text
   waits. A result never replaces a newer one. A parse starts at most 30
   times a second. [The structure section](#10-the-apps-structure-m) says
   where the parse should live.
4. **Finished blocks hold still.** A reply is split into blocks. A block's
   identity is its position and a hash of its text, so only the last, open
   block is drawn again. A reply several screens long is split into
   several rows at block boundaries, which keeps a lazy stack's height
   estimates honest.
5. **Unfinished Markdown is closed before it is parsed.** An open
   emphasis, code span, link or fence is closed for display. A table shows
   as a header row as soon as its first line is in. The page only ever
   grows.
6. **Text is paced.** Pieces are revealed at a steady rate that catches
   up, so the screen is never more than about 0.4 seconds behind the
   model. The front never moves back. Reduce Motion turns pacing off.
7. **Scrolling follows the reader.** A new chat lays out from the top and
   starts following only once it outgrows the screen. A touch releases
   following; coming to rest within about 80 points of the end takes it up
   again. Following reacts to the content's height, not to a hash of the
   screen. A send always goes to the end. Jump to Latest says how many
   messages are below.
8. **Thinking has a window of fixed height.** It shows the newest lines
   while the model thinks, then becomes "Thought for 12 s". It gives its
   height to the answer as the answer starts, so the page does not jump.
9. **The composer says what is happening.** Connecting, Thinking, Writing,
   the tool that is running, Reconnecting. Stop is there the whole time,
   and the field can be typed in throughout.
10. **A flush costs the same at the end of a run as at its start.** A live
    turn's presentation is kept between flushes, a tool's arguments are
    formatted once, and the list's rows are not rebuilt for a delta.

### 8c. Failures in plain words

One sentence that says what happened, one button that does the next thing,
and at most one note. The card sits at the turn that failed, not in an
alert, and what the owner typed is never lost.

| Cause | The owner reads | Button |
| --- | --- | --- |
| `provider_key_missing` | Anthropic has no API key yet. | Add Key |
| `provider_key_refused` | Anthropic refused the API key. | Replace Key |
| `provider_rate_limited` | Anthropic is limiting requests. Try again in 20 seconds. | Try Again, counting down |
| `provider_billing` | The Anthropic account is out of credit. | Try Again |
| `provider_overloaded`, `provider_unavailable` | Anthropic is having trouble. What Moss wrote so far is kept. | Try Again |
| `model_unavailable` | Anthropic does not offer this model to your key. | Open in Console |
| `context_too_long` | This chat is too long for the model. | New Chat |
| `allowance_spent` | Today's allowance is used up. It resets at midnight UTC. | Add Your Own Key |
| `spend_cap` | Today's spending limit is reached. | Open in Console |
| `turn_limit`, `tool_call_limit`, `deadline` | Moss ran out of room before finishing. | Continue |
| `interrupted` | The server restarted while Moss was working. | Try Again |
| `cutBy: length` | The reply stopped at the model's limit. | Continue |
| no connection | You're offline. Your message is in the box below. | Send Again |

- The switch over `cause` is exhaustive in ChatKit, so a cause without a
  sentence and a button does not compile. An unknown cause from a newer
  server gets the sentence for its `status`.
- Try Again and Continue call the continue route. Send Again repeats the
  send under its `messageId`. Neither can run a turn twice.
- The server's own text is under Details, under a label that says whose
  words they are. The app's own transport sentences are not labelled "The
  server said".
- Reconnecting is a state the composer shows, not a failure.
- A Stop that fails says the stop failed. The list's "Couldn't refresh"
  gets Try Again, and a problem with the change feed is drawn.
- A chat is archived at once with an Undo for a few seconds, not behind a
  confirmation. Undo is faster for the common case and safer for the rare
  one.

Before the server sends `cause`, the app can ship the card, its buttons
and the table with today's matching behind it, and swap the matching out
later.

### 8d. The interface

- **Under a reply:** Copy, Try Again or Continue where they apply, and
  Share, as a row of small targets under the last reply. A long press
  keeps them on older ones. Select Text opens the reply in a sheet where
  words can be selected.
- **Code:** highlighting, a wrap switch shown only when a line overflows,
  and Copy that confirms itself and says so to VoiceOver.
- **Tables:** natural width where it fits; otherwise sideways scrolling
  with the first column held in place. At the largest text sizes, one card
  per row.
- **The list:** a spinner on a running chat's row, the waiting mark from
  `waitingOnOwner`, swipe to pin and to archive once the mark record
  carries them, rows that take their place with motion, and placeholder
  rows while the first read is out.
- **Search:** the server's ranked read over `llm/message`, merged under
  the instant title matches after a short pause, with the matching words
  shown.
- **Paging:** older chats past the first 100 and older rows past the
  newest 500, loaded on reaching the end.
- **The first screen:** the app opens to the chat that is running or that
  was left less than half an hour ago, and otherwise to a new chat with
  the composer focused.
- **Motion and touch:** one place decides every animation and honours
  Reduce Motion. Stop and copy get the light haptic a send already has,
  and a reply that lands while the owner is elsewhere in the app gets one.
- **Text size:** every fixed size scales with the text size, every target
  is at least 44 points, and layouts that cannot fit fall back to a taller
  form.
- **Reading width:** on an iPad and a Mac the transcript keeps a readable
  measure, about 680 points, however wide the window.

### 8e. Interaction

- **Answer in your own words.** A question card offers it beside its
  options. It dismisses the card and puts the cursor in the composer. The
  interaction kind says prose belongs in chat, and this keeps that.
- **A resolved card folds into its tool row.** A card stands in the
  transcript only while it waits on the owner. Once answered it is a check
  or a cross on the call that asked, opened on a tap.
- **Queued messages**, from change 6.
- **Later:** suggested next messages under a reply (a small model call per
  turn, so the owner's switch); a spoken conversation on the device's own
  speech recognition and voices (listen, send, speak sentence by sentence,
  stop speaking when the owner talks); sharing into a chat from other
  apps; widgets.

### 9. Tests that hold it

- **A server that misbehaves by a seed.** The chat rig gains a scripted
  server that cuts streams, repeats events, answers 5xx, 429 and 401,
  hangs, and returns a page of HTML, all chosen from one seed. A test
  runs random sends, stops, attaches and returns to the front against it
  and checks after every step: the transcript equals the fold of the
  server's rows, no conversation is left streaming, and no two rows share
  a `messageId`. A failure is a seed that replays.
- **The handover, frame by frame.** A test records every frame from the
  first token to the stored rows and asserts that the content's height
  never shrinks, that no row is torn down, and that an attach never
  empties the screen.
- **Budgets, measured on a device and written in the pull request.** A
  flush under 8 ms on the main thread in a 200-message chat; a 16 KB reply
  parsed under 8 ms; a cached chat drawn in its first frame.
- **Rules read from the source.** A test fails on an animation that
  bypasses the one policy, and on a colour pair under 4.5:1.
- **A review list for screens.** Clarity, a response within 100 ms,
  forgiveness, consistency, accessibility and craft, each scored on its
  own and never averaged, against screenshots in light, dark and the
  largest text size attached to the pull request.
- **A diagnostics file the owner can share.** It holds decisions and
  timings (which stream dropped, how long a parse took, why following
  stopped) and never a word of a message.

## 10. The app's structure (M)

Changes 8a to 8e say what the owner should see. This section is about the
code under them: whether the app is built so that those behaviours come
out of its structure, or have to be patched on. It was read, not profiled,
so its first step is to measure.

### What is sound and stays

- **One loop.** A command becomes events, a pure engine applies them, a
  presenter draws the state as a value, and effects start only after that,
  all on the runtime's own executor ([the kit](../../apps/kit/README.md)).
  SwiftUI draws values and sends commands. The same ChatKit runs on Linux
  and is what an Android app would draw from.
- **Late results cannot land.** Every read and run carries an epoch or a
  generation, and a result from an older one is dropped. The seven rules
  in the app's design are tests, on a harness with its own clock.
- **The cache is honest.** What the device keeps is the server's, checked
  by the thread's version, and never treated as the truth.
- **Two cheap guards exist.** Stored turns are presented once per read
  (`Presentation/TurnMemo.swift`), and a turn's view is `Equatable`.

None of the changes below replaces the loop. They change what goes round
it, and how much.

### What the experience pays for

**A. Every change redraws the whole tree.** The runtime publishes one
value for the whole app, and SwiftUI holds it as one observable property
(`apps/kit/Sources/SubstrateUI/PresentationModel.swift`). Every view reads
it through `host.chats`. So one streamed delta, 20 times a second, does
all of this:

- The root presenter runs again (`ChatApp/ChatAppPresenter.swift:12`). It
  rebuilds every list row with its date stamp and preview, runs each
  window's search over the transcripts in memory, and rebuilds every open
  conversation, Settings and the key sheets.
- The whole tree is compared for equality three times: in the host, in
  the loop and in the model.
- Every view that read the property evaluates its body again, and the
  transcript compares each of up to 500 turns to find the one that
  changed.

Typing pays the same. Each keystroke is a command (`ComposerBar.swift:86`)
whose event presents at once (`Conversation/Conversation.swift:205`), and
the draft's text is part of the tree.

What to change:

- **The root presenter is made of parts, each kept until what it reads
  changes**: the list, each window, each conversation, Settings. A delta
  in one conversation then rebuilds that conversation's live turn and
  nothing else. `TurnMemo` already does this for one part.
- **The model publishes by path.** The host already knows which child's
  presentation changed. `PresentationModel` should keep one observable
  slot per controller path, so the conversation view observes its
  conversation and the list observes the list. This is a kit change, and
  Tasks and Workouts gain from it too.
- **A draft stays out of the tree while it is typed.** The field owns its
  text and tells the engine after a pause, as it already tells the disk
  after 0.6 seconds. The tree carries the draft only when the engine
  changes it (a refused send returning its text, a dictation landing).

**B. Markdown is parsed by the view.** `TurnPresentation` carries a
reply's text, and the SwiftUI view parses it in its body, on the main
thread, on every flush. The parser itself is already in ChatKit
(`Model/Markdown.swift`).

- The presenter should parse, and the turn should carry its blocks as a
  keyed list whose ids are a block's position and a hash of its text. The
  parse then runs on the runtime's executor, off the main thread, with no
  new machinery.
- A finished block is an unchanged row of that list, so SwiftUI leaves it
  alone, and the kit's keyed list already sends only changed rows over the
  Android bridge.
- Closing unfinished Markdown (8b rule 5) becomes one function in ChatKit
  with tests on every prefix of a reply, where the existing Markdown tests
  already work that way.

**C. A turn has two models.** A streaming turn is a `LiveOverlay` turn
with an id of its own. A stored turn comes from the transcript fold. The
presenter joins them, a `handingOver` phase swaps one for the other, and
rule 3 ("no rows under a live overlay") and the emptied overlay on attach
exist to stop the two from showing the same text twice.

- One fold should take both stored rows and stream events and produce one
  list of turns keyed by `turn`. An event is a provisional row; the stored
  row with the same key replaces it.
- The handover then stops being a phase: it is the fold receiving rows. An
  attach is the fold receiving events it may already hold. The blink, the
  reset of open sections and the doubled-text guard all go with the second
  model.
- With `at` on the wire (change 2) the fold is exact. Before it, the fold
  can keep the longer of the text it holds and the text replayed.

**D. A failure becomes a sentence too early.** `Failure` is built where
the request fails and carries its `words` (`API/Failure.swift:8`). The
conversation's state then keeps `error: String?`
(`Conversation/ConversationState.swift:92`). By the time the presenter
draws it, nothing says what was being attempted, which is how a failed
Stop reads as "couldn't answer".

- State should keep a value: what was attempted (a send, a stop, a
  reload, a rename, a card's answer) and why it failed (a `cause` from
  change 3, a status, no connection). The presenter chooses the sentence
  and the button, in one switch.
- This is the structure 8c's table needs. Without it the table is a
  second layer of matching over strings.

**E. The transcript is a lazy stack of rows of any height.** A lazy stack
estimates the height of rows it has not drawn, and a reply several
screens tall makes those estimates poor: scrolling to the end lands on an
estimate, then corrects.

- Splitting a reply into block rows (8b rule 4, and B above) bounds a
  row's height and may be enough.
- If jumps remain after that, the transcript should move to the
  platform's collection view with self-sizing cells that host the same
  SwiftUI rows, which measures exactly. That is a larger change with a
  separate path for the Mac, so it is decided by measurement, not here.

**F. Lists are whole arrays with a fixed window.** The chat list is the
first 100 threads and a transcript is the newest 500 rows, each loaded
and presented whole. Paging (8d) needs the state to hold a window that
grows at its ends, and the presenter to draw only what is near the
screen.

**G. The views carry their own numbers.** The chat's views hold about 230
literal paddings, fonts, spacings and radii, and use none of the kit's
design tokens, though the kit has roles for text, spacing and radius
(`apps/kit/Sources/SubstrateUI/DesignTokens.swift`). They animate in 14
places and check Reduce Motion in 3. So "the interface needs more love"
has no one place to be answered.

- The chat's type scale, spacing, radii and motion become tokens, in the
  kit where another app would share them. A change of feel is then one
  edit.
- Every animation goes through one function that honours Reduce Motion,
  which is what the source-reading test in change 9 checks.

**H. Nothing measures the view layer.** Every test is in ChatKit. The
app's own tests cover the sign-in receiver, the Keychain item and inline
links. Nothing records a frame, counts a body evaluation or times a
parse, and the app has no signposts.

- Signposts and counters on the four costs above: a presenter run, a
  publish, a body evaluation per view, a parse. The runtime already keeps
  counters and a trace for tests; the same numbers go into the
  diagnostics file of change 9.
- These land first, so A to E are sized by numbers from a device with a
  500-row chat, not by reading.

### What to leave alone

- **The loop and its rules.** They are why the app does not lose or
  duplicate messages, and they are well tested.
- **The device's storage.** JSON files written whole are fine for 100
  chats and 500 rows. A database on the device is worth it only if the
  app must search or page without the server, and the server already
  searches.
- **The size of the engines.** They are long and split by concern; that
  costs the reader, not the owner.

One small repair while there: `SystemNotificationDelegate` is declared
three times in `apps/kit/Sources/SubstrateDeviceApple/SystemNotifications.swift`,
two of them nested inside functions.

## The order

| Phase | Pull requests | What the owner gets |
| --- | --- | --- |
| 1, app only | Measurement from 10 H first; then 10 A to D, which carry 8b rules 1 to 5 and 10 and 8c's card with today's matching; 8b rules 7 to 9; permission timing and the notification hold from 8a; reply actions, code and tables from 8d on the tokens of 10 G; the seeded server from 9 | No blink, steady text, smooth typing in a long chat, a button on every failure |
| 2, server and both clients | Change 1 with its migration; change 2; change 3; change 4 | Read state on every device; resumed answers stream; failures named; safe resend |
| 3 | Change 5; change 6; notification actions; pin and archive on the mark; search, and paging on 10 F; 10 E if the numbers ask for it | A list that says more, steering, answers from the lock screen |
| 4 | Change 7; a Live Activity; Web Push for the console | A closed phone hears about it |

Phase 2's four changes are independent of each other and each is one pull
request: the kind or wire change, the engine, the console, the app. Each
declaration change ships its version bump, and the wire golden file moves
with every new field.

## Decisions this needs

One record each, written with its pull request:

- The thread mark and its forward-only rule. It amends 0195 so that the
  `agents:` verb reaches the mark of a thread it reaches.
- Positions on stream events, and a resumed chat run as a detached turn.
  It amends 0169.
- The closed set of causes, and the reasons added to the error envelope.
- `messageId` and the continue route.
- A message admitted during a run. It amends 0169.
- Push: the device kind, the relay named by the authority document, and
  the server calling out on its own initiative.

## Positions this questions

- The chat-turns plan called unread marks "a client's own state". They
  are the person's, by 0132's own rule.
- The app's design lists push and reply from a notification as non-goals
  for its first version, and holds that nothing is ever sent again on its
  own. With change 4 a repeat cannot run twice, so the rule becomes:
  nothing is sent again under a new id on its own.
- The Moss plan gives a notification record its own `readAt`. One read
  mechanism is enough.
- 0195 rejected a full-access token for the chat app, and the app asks
  for `full` today because no scope reaches Moss. Each new kind here gets
  a scoped reach, so the app can go back to scopes when that gap closes.

## Not proposed

- Presence, typing indicators, delivery receipts or shared threads: there
  is one owner.
- Chats that leave no record. Records are the truth, and a chat the
  changelog never saw is outside it.
- WebSockets, Server-Sent Events or any second transport.
- A stored event stream for turns (see change 2).
- Editing or regenerating a message in place. Both rewrite history. When
  wanted, the shape that fits is a new thread that names the message it
  forks from.
- Deleting a thread. Archive hides it and its cost still counts.
- Sentences written by the server for the owner to read.
- Drafts synced across devices. A draft is a fact of one window, and
  syncing it is a changelog entry per pause in typing.

## Open questions, with the default taken

- The kind's name, `threadmark`, follows the rule that a kind belonging
  to another starts with its name. The kind review may prefer another.
- The unit of `at` is Unicode code points, because Go, Swift and
  JavaScript can all count them the same way.
- A resumed run takes a chat-turn slot.
- The hold before a notification is five seconds.
- `cause` is on the thread, not on each message: a thread has one run at
  a time and the list reads threads.

## Public practice this draws on

- Read state: Matrix's
  [receipts, fully-read marker and unread marker](https://spec.matrix.org/latest/client-server-api/#receipts),
  IRCv3's [read marker](https://ircv3.net/specs/extensions/read-marker),
  Telegram's [dialog](https://core.telegram.org/constructor/dialog) and
  Slack's
  [conversations.mark](https://docs.slack.dev/reference/methods/conversations.mark).
- Push for self-hosted servers: Matrix's
  [push gateway](https://spec.matrix.org/latest/push-gateway-api/), ntfy's
  [iOS relay](https://docs.ntfy.sh/config/#ios-instant-notifications),
  Mattermost's
  [push proxy](https://docs.mattermost.com/deployment-guide/mobile/mobile-faq.html)
  and Home Assistant's
  [notification relay](https://contributors.home-assistant.io/companion/mobile-app-api/notifications/);
  Apple's
  [remote notification payload](https://developer.apple.com/documentation/usernotifications/generating-a-remote-notification)
  and
  [actionable notifications](https://developer.apple.com/documentation/usernotifications/declaring-your-actionable-notification-types).
- Streams a client can rejoin: the HTML standard's
  [server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html),
  the AI SDK's
  [stream resumption](https://ai-sdk.dev/docs/ai-sdk-ui/chatbot-resume-streams)
  and LangGraph's
  [join stream](https://docs.langchain.com/langsmith/agent-server-api/thread-runs/join-run-stream).
- Streamed text: the AI SDK's
  [smoothStream](https://ai-sdk.dev/docs/reference/ai-sdk-core/smooth-stream),
  Streamdown's [block memoization](https://streamdown.ai/docs/memoization),
  Chrome's
  [guidance on rendering model output](https://developer.chrome.com/docs/ai/render-llm-responses)
  and SwiftUI's
  [ScrollPosition](https://developer.apple.com/documentation/swiftui/scrollposition).
- Failures: [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457.html),
  Google's [AIP-193](https://google.aip.dev/193), Stripe's
  [error object](https://docs.stripe.com/api/errors), Apple's
  [guidance on alerts](https://developer.apple.com/design/human-interface-guidelines/alerts),
  and the error classes of
  [Anthropic](https://platform.claude.com/docs/en/api/errors) and
  [OpenAI](https://developers.openai.com/api/docs/guides/error-codes).
- Interaction: Claude Code's
  [queued messages and interrupt](https://code.claude.com/docs/en/interactive-mode)
  and MCP's
  [elicitation](https://modelcontextprotocol.io/specification/2025-06-18/client/elicitation).
