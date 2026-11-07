# Plan: what Moss needs next

Status: proposed 2026-11-07. A plan, not a contract. It follows
[Moss](moss.md) and its [recorded sessions](moss-sessions.md), and asks
what the substrate must add so its assistant stands beside the general
assistants people already use, while doing the one thing they cannot: keep
the owner's own typed records.

## Where Moss stands

What the sessions showed works today, on the tree as it is:

- General answers with no tool call, writing and code included, and an
  honest line where the model's cutoff ends.
- Web search with inline citations, once the Firecrawl provider is
  installed and keyed: its `web_search` and `web_scrape` are offered to
  agents, and Moss takes every offered function.
- Memory the owner approves, pinned into every thread, shaping world
  answers too.
- Reading, writing and proposing over every kind; deletes proposed, never
  written.
- New collections and fields through the architect, planned before they
  apply, a narrowing deprecated instead of dropped.
- Bulk work as a background job that reports back into the thread.

What it could not do, in the order the owner would notice:

- Read a photo, a PDF or a file the owner sends: a chat message carries
  text alone.
- Remind the owner of anything, or tell them anything unprompted: the
  server has no push channel (`apps/chat/DESIGN.md`), and Moss has no way to
  start a conversation.
- Make something to keep or show: a plan, a page, a chart, a document. Its
  output is chat text, and the substrate has no publishing at all ("no
  sharing").
- Compute over the owner's data beyond what `query`'s aggregates answer:
  no code to run.
- Use a service no provider ships.

## What the others do

A survey of the general assistants, late 2026. Items marked (unv.) were
found only in secondary sources.

| Capability | Claude | ChatGPT | Perplexity | Gemini |
| --- | --- | --- | --- | --- |
| Web search with citations | Yes | Yes | The product; numbered inline sources | Yes |
| Long background jobs | Research; Cowork tasks run in the cloud | Deep Research; agents that work for hours | Computer: a sandbox per job | Deep Research |
| Documents and pages | Artifacts, private by default, shared by link, invite or org, with view, comment and edit roles; unpublishing deletes the storage | Canvas; Sites (unv.) | Pages | Canvas |
| Apps with state | Artifacts with personal and shared storage that call Claude | Sites (unv.) | Computer builds apps | Canvas apps |
| Code and data analysis | Code execution, file analysis | Code Interpreter | In Computer | Yes |
| Files in, files out | Yes; writes Office and Google files | Yes | Decks, sheets, reports | Workspace |
| Memory | Per project; chat search; import | Saved facts plus chat history, with a Sources view of what shaped an answer | Preview (unv.) | Personal context |
| Scheduled work | Cowork scheduled tasks | Tasks, push or email | Automations on a schedule or an event; silent when nothing is new; pause after repeated failures | Scheduled actions |
| Approvals | Allow, ask or off per tool; questions inline | Admins pre-authorize | Asks rather than guesses | Asks before send, buy or submit |
| Connectors | MCP connectors; plugins bundling skills, connectors and sub-agents | Plugins (apps renamed) | Gmail, Slack, Notion and others | Workspace |

Sources: Claude's
[artifact publishing](https://support.claude.com/en/articles/9547008-publishing-and-sharing-artifacts),
[Cowork](https://support.claude.com/articles/13345190) and
[memory](https://support.claude.com/en/articles/11817273);
ChatGPT's [memory](https://help.openai.com/en/articles/11146739-how-does-reference-saved-memories-work);
Perplexity's
[Automations](https://www.perplexity.ai/help-center/en/articles/11521526-perplexity-tasks)
and [Computer](https://www.perplexity.ai/help-center/en/articles/13837784-what-is-computer);
Letta's [memory architecture](https://docs.letta.com/guides/agents/architectures/memgpt).

The substrate's advantage over all four is the store itself: typed records
the owner defines, a changelog that says who changed what and why, and a
policy door. Most of what follows is the others' features built on that
store rather than beside it.

## The proposal

Ranked by what the owner gains per unit of work. Each item names its
mechanism in the substrate's own terms and the decision record it needs.

### 1. Files in: attachments on a chat message (M)

The owner sends a receipt, a lab report or a photo of a plant label, and
Moss reads it and files it as records.

- `llm/message` gains `attachments`, a repeated `blobref`. The chat route
  takes blob digests the client uploaded through the existing blob route.
- Each wire renders them as its own blocks: Anthropic `image` and
  `document`, OpenAI `image_url` and `file`. A model without vision is
  refused by name.
- A replayed thread sends the attachment again, so it stays inside the
  cached prefix.
- The Chat app and the console get an attach button.

Decision: what an attachment is on the wire, and its size bound.

### 2. Reminders, schedules and notifications (M, then L)

"Remind me Sunday to water the balcony", "every Monday, tell me what is
due". This is where an assistant stops waiting to be asked.

- **Schedules exist.** A `core/trigger` with a `schedule` source can name
  Moss as its callable. A `schedule` tool takes plain words ("every Monday
  at 8") and writes that trigger, the plain-language setup Perplexity's
  Automations has. Its writes are Moss's own, through the policy door.
- **A notification is a record.** A new seeded kind,
  `substrate.reamde.dev/llm/notification` (`title`, `body`, `thread`,
  `record`, `readAt`), written by a `notify` host function. The apps
  already show local notifications. They follow the changefeed and raise
  one per unread record, so nothing new is needed on the server to reach a
  phone that is open.
- **A run with nothing new stays silent.** A scheduled run that calls no
  `notify` leaves no notification.
- **Push, later.** APNs, Web Push and email delivery for a phone that is
  closed. A `core/device` record holds each registration, and the operator
  configures the senders.

Decisions: the notification kind and `notify`; then the push channel,
which is the first time the server talks to an outside service on its own
initiative.

### 3. Documents: what Moss makes, kept as records (M)

Plans, briefs, comparisons, meeting notes, a research report: the owner's
to keep, edit and find again, not text lost in a thread.

- A seeded kind, `substrate.reamde.dev/llm/document`: `name`, `body`
  (`markdown`), `format` (`markdown`, `html` or `chart`), `files`
  (repeated `blobref`), `thread`. Its versions are the changelog, so
  "show me last week's version" is a read Moss already has.
- Moss writes one with `write`. No new tool, and the owner's policies
  apply.
- The console renders markdown already (`message-text.tsx`). `html` renders
  in a sandboxed frame with no network and no script reaching the console.
  A `chart` is a Vega-Lite specification the console draws.
- **A live document is a view.** A dashboard over the owner's data is a
  saved view or a record layout, both already records
  ([0166](../decisions/0166-a-record-page-layout-is-a-record-in-a-seeded-console-package.md),
  [0229](../decisions/0229-a-collections-shipped-saved-views-are-records-of-a-seeded-console-kind.md)).
  The architect gains `console/*` in its writes, so "make me a page of my
  plants by next watering" is a view, computed every time it opens.

Decision: the document kind, and what an `html` body may do.

### 4. Publishing: a page anyone with the link can read (M)

Claude's model is the right one: private by default, shared deliberately,
revocable. The substrate's rule is "no sharing", and this keeps it. Nobody
else reaches the repository; the owner publishes a copy of one document.

- A `publication` record names one document, a snapshot version or
  `live`, an unguessable slug, an optional expiry and `revoked`.
- Like `recordpatchpolicy`, it is the owner's hand alone: an agent may
  propose one, never write it.
- The server serves `/p/<slug>` as rendered, read-only HTML under a strict
  CSP, with no session and no API behind it. Revoking or expiring answers
  `410`.
- The console lists every live publication on one page, the way the
  Providers page lists outside services.

Decision: required, because it narrows "no sharing" to "no shared access",
and because a public route is new surface.

### 5. Running code over the owner's data (M)

"Does my sleep track my training load?", "chart my spending by month".

- An offered function, `analyze`, in a first-party catalogue: Python in
  the existing sandbox with no network. Its input is code plus the result
  of `query` calls Moss makes first, passed as data, so it reads nothing
  past Moss's grant. pandas, numpy and matplotlib come in as inline
  dependencies.
- It returns stdout and files: a chart lands as a blob on a document.
- The same catalogue writes files: docx, xlsx and pdf from a document.

Decision: none beyond the catalogue's review. It runs as outside code
([0220](../decisions/0220-outside-code-is-confined-and-reviewed.md)) does.
This needs the Linux sandbox; a macOS development server shows the gap.

### 6. Deep research as a background job (S, after 3)

A `mossResearcher` sub-agent with the web tools and a write grant on
`llm/document`, called with `background: true`. It searches, reads,
cross-checks, writes a cited document and reports. Moss answers at once
("I've started on that"), and the `toolCompleted` resume links the
document. Everything it needs exists once documents do.

### 7. Sources on every answer (S)

The engine already stamps the records a reply links as `mentions`. Two
more source types would give Moss what ChatGPT's Sources view offers:

- the web pages a tool returned, with title and URL;
- the pinned memories the turn used.

The console and the Chat app render them as one row under the reply.

### 8. Memory that consolidates (M)

- A weekly `mossGardener` run reads the week's threads and proposes
  memory changes: new facts, merges, retirements. Letta's sleep-time
  agent does this; every change still passes through the owner.
- A memory carries its source thread, so "why do you think that" has an
  answer.
- Past conversations are records already. Moss's prompt says to search
  them (`llm/message`, `q`) when the owner refers to something said
  before.

### 9. Approvals the owner sets once (S, UI)

The policy door already answers allow, gate or refuse per kind, op and
agent ([agents](../agents.md#the-policy-door)). What is missing is the
page:

- Moss's permissions, as a list of kinds with one switch each;
- the proposal and question cards in the Chat app, so a decision does not
  wait for a desktop.

### 10. Connectors beyond the shipped providers (L)

An MCP client provider: the owner adds an MCP server's URL, the substrate
holds its OAuth token as a secret, and its tools are offered to agents
under the policy door. It is outside code, so it falls under
[0220](../decisions/0220-outside-code-is-confined-and-reviewed.md).

Decision: required, because a third party's tools would act with the
owner's grant.

### Smaller, any time

- **Think harder on request.** A chat request's `effort` overrides the
  agent's for one turn. On Opus 5.5 this is a mid-conversation system
  message, so it does not reset the cache. The console shows a "think
  harder" toggle.
- **Narrow `*`.** A grant exclusion (`!substrate.reamde.dev/core/*`) so
  Moss's own `write` cannot reach declarations, triggers or agents; only
  the architect, and the `schedule` tool, write those.
- **Semantic search by default.** An embeddings provider through operator
  defaults, so `q` ranks by meaning on a fresh substrate.
- **Voice** in the apps, on-device speech to text into the same chat
  route.
- **Image generation** as an offered function on a provider row. Low
  priority for this product, and so is a browser agent.

## Order

| Phase | Items | What the owner gets |
| --- | --- | --- |
| 1 | 1, 7, smaller items | Moss reads what they send, and shows where answers came from |
| 2 | 2 (in-app), 3, 6 | Reminders, briefings, documents and research that last |
| 3 | 4, 5, 8, 9 | Pages they can share, analysis, memory that keeps itself, approvals on the phone |
| 4 | 2 (push), 10 | Notifications on a closed phone; any service with an MCP server |

Each phase lands one decision record per new kind or door. The sample's
agents change with each, so the sessions are recorded again, and an eval
built from them is the gate for the prompts.
