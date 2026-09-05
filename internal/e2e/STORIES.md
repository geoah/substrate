# The user stories

These are the story-level cases for `mise run test:e2e`: whole scenarios a
real repository lives through, replayed against the live server and left in
place for a human to browse. Where the slice cases pin one endpoint at a
time, a story pins a pipeline: data arriving the way importers deliver it,
functions and agents acting on it, and a graph at the end that a reviewer
can walk.

Three ground rules:

- **Deterministic work stays deterministic, and agents decide.** Resolving
  an email to a person is a lookup, so a function does it end to end.
  Matching a transcript to a meeting and deciding what work it implies are
  judgments, so agents make them, with the deterministic parts (candidate
  scoring) available to the agent as function tools rather than replaced by
  it.
- **The substrate is never mocked; the model always is.** Agent stories run
  against a scripted OpenAI-wire stub the test hosts (an `llmprovider`
  record points at its loopback URL; the dev server needs
  `SUBSTRATE_EGRESS_ALLOW=127.0.0.0/8,::1/128`, which the `test:e2e` task
  sets). Every "LLM decision" is scripted, so every assertion is exact.
- **Fictional data, realistic shapes.** Invented names and `.example`
  addresses throughout, arranged the way real importers arrange them.

## The ecosystem

One owner's world, small enough to read whole. The owner registers the
repository; everything else arrives the way it arrives in practice:
contacts first, then calendars, then events, then a notetaker's
transcripts.

| Thing | Records |
| --- | --- |
| the owner | a `person` for the owner, the `me` of the graph |
| organizations | `acme` (the owner's, domain `acme.example`) and `northwind` (a client, domain `northwind.example`) |
| teams | Product and Engineering, Platform under Engineering (`parent`), `members`/`leads` references |
| people | five coworkers `@acme.example` (each with `emails` and a `memberOf` reference carrying `role` as link data), one client contact `@northwind.example` |
| projects | Onboarding revamp (active), Billing migration (active) |
| tasks | a few per project with `project`/`assignee` references, one with `dueAt` |
| calendar | one `calendar`; a kickoff `calendarevent` and a billing-sync event 30 minutes apart |
| transcripts | the kickoff's transcript from a fictional notetaker, later a chitchat transcript and an orphan one |

## STORY-01: the graph exists

*As the owner, I describe my world once and every relationship is navigable
from every end.*

Hand-written over the API, no automation. Installs `people`, `scheduling`,
`tasks`, `calendar`; writes the ecosystem above; asserts the graph from both
ends (references read back canonical on a single GET and inline on a plain
list, `/incoming` on a person, a filtered and ordered list) and the refusals
(an `assignee` naming a team, an `assignee` at a person who does not exist,
a link property `memberOf` does not declare, and a body still writing
`edges`). The GraphQL read over the whole graph is STORY-06's, where it
doubles as the rebuild comparison. Every later story builds on what this one
leaves.

## STORY-02: attendee emails become people, deterministically

*As the owner, when an event lands with raw attendee emails, the people I
know are named, the stranger is minted, and the meeting-room address never
becomes a person.*

The wiring is a story-local **python function** (applied via
`vocabulary/apply`) behind a trigger on the story's `eventimport` kind: the
record an importer actually delivers, raw emails and all
(`sam@acme.example, nour@acme.example, priya@northwind.example,
jordan@northwind.example, c_room7@resource.calendar.example`), nothing
anywhere. The function folds it into the real `calendarevent`.

1. each email resolves against `person.emails`; three resolve;
2. `jordan@northwind.example` resolves nowhere, so a person is minted, named
   from the email's local part, one email, a `memberOf` reference to
   Northwind by `domain`;
3. the `c_room7@resource.calendar.example` automated address is filtered and
   minted as NOTHING: room and group addresses are how a contact list fills
   with furniture, so the negative assertion is the point;
4. the `attendees` and `organizer` references land;
5. run twice: the second delivery converges on the same graph, no duplicate
   person, no duplicate pointers. The function writes by chosen record ids,
   so re-delivery is idempotent by construction.

Asserted: the four attendees and the organizer; the minted `jordan` record's
exact shape; no record whose email is the resource address; idempotent
re-delivery; changelog attribution to `function:<authority>:<package>:<name>`.

## STORY-03: a transcript finds its meeting, or honestly does not

*As the owner, an uploaded transcript attaches to the meeting that actually
happened, says why, and a transcript that matches nothing attaches to
nothing.*

The `transcriptMatcher` agent, behind a trigger on `transcript` creates. It holds a
scoped `mutate` grant and carries one function tool: a story-local
`scorecandidates` that scores every `calendarevent` within 90 minutes of the
transcript's time on two weighted signals (start-time proximity 0.6, title
token overlap 0.4) against a 0.4 floor. The arithmetic is the tool's; the
decision is the agent's.

1. The kickoff transcript (title copied from the meeting, as recorders do;
   ten minutes of clock skew): the scripted agent calls `scorecandidates`,
   BOTH events come back scored, the kickoff wins on time + title and the
   billing sync loses despite being inside the window. The agent patches
   `meeting` and `speakers` onto the transcript through its `mutate` tool
   (there are no link verbs to reach for) and writes its audit as a
   `matchverdict` record (verdict, score, reason, and a reference to each
   end), so a wrong match is debuggable from the repository itself.
2. The orphan transcript (no event within 90 minutes, alien title): the tool
   answers with no candidate above the floor, and the agent declines: no
   `meeting` at all, an `unmatched` verdict saying why. Attaching to the
   least-wrong meeting is worse than attaching to none.
3. The agent names as `speakers` only the people who actually spoke; the
   attendee who said nothing is in no `speakers` list.

The agent's write is what fires STORY-04: the two agents form a chain, and
the chain itself (a trigger firing on an agent's write, with both actors
distinct in the changelog) is one of the assertions.

## STORY-04: reflection: what was said becomes work, with provenance

*As the owner, a meeting's action items become tasks assigned to the right
people, judgment calls wait for a decision, and no new record enters my
world without a source.*

The second agent in the chain: a trigger on the transcript's `meeting`
property landing (STORY-03's agent made that write) fires the
`actionItemExtractor` agent, which reads via `graphql` and writes ONLY via
`propose`. The kickoff transcript's text encodes one case per rule:

1. "Nour will draft the new welcome flow by Friday": a proposed task,
   `project` to Onboarding revamp, `assignee` Nour, `dueAt`, and `source`
   naming the transcript. High confidence; the reviewer accepts; it lands
   `open`.
2. "Kai to profile the signup path": same shape, second assignee.
3. "Someone should sync with Northwind about the pilot": nobody is named, so
   the task lands in state `proposed` with no assignee. Proposed work waits
   for the owner; an agent that guesses an assignee is inventing a fact.
4. The transcript says "Speaker 3" for an unidentified voice: the agent
   creates NO person for it. A speaker label is not an identity.
5. A scripted turn proposes a task with no `source` at all: the request is
   filed, the `changeRequestReviewer` REJECTS it, and the task never folds; the rejected
   request stays behind as the audit of the refusal. A task nobody can
   trace back to evidence is a bare claim, and the sharp assertion is the
   rejection leaving no task, not the presence of sources elsewhere.
6. "The invite flow redesign is now urgent": a prioritization is a judgment
   call, so it travels as a proposed patch on the existing task; the arbiter
   agent, a DIFFERENT scripted model id than the writer (a writer grading
   itself is not a review), accepts it, and accepting is what applies the
   diff.

Asserted along the whole chain (source, run, proposal, judgment, decision,
action): the transcript's `/incoming` shows the tasks pointing back through
`source`; the patchrequest's decision is recorded; writer and judge are
distinct actors in the changelog; the rejected proposal left no record
behind.

## STORY-05: the quiet window

*As the owner, when nothing happened, nothing is proposed.*

The chitchat transcript (weather, lunch, no decisions, no commitments) goes
through the same trigger chain. The scripted agent reads it and proposes
nothing. Asserted: the delivery settled OK, the model was actually consulted
(a skipped run must not pass as silence), and the repository gained no work:
not one proposal, not one task. The run's own bookkeeping (thread,
transcript rows, the run record) is the only trace. Silence is the pass
condition, and absence is asserted, not assumed. This is the precision
control of the set: it has no happy path to hide behind, and it must keep
passing forever.

## STORY-06: the world holds together

*As the operator, after all of that I can prove nothing drifted.*

1. Attribution audit over the whole changelog: every row's actor is exactly
   the user's token actor, a bundle actor, the story functions
   (`function:<authority>:<package>:<name>`) or the story agents
   (`agent:<authority>:<package>:<name>`); writer and judge distinct; nothing wrote as
   `substrate` except registration's own records.
2. `substratectl repository verify`: every hash, every signature.
3. `substratectl repository rebuild`: the refolded records answer STORY-01's
   GraphQL join identically.
4. The report appendix renders the ecosystem and the full changelog.

## Vocabulary gaps these stories surface

Writing the stories against the shipped kinds names what they cannot yet
say; each is a candidate for vocabulary work, not a test to force:

- **Durable memories**: transcript reflection here produces only tasks;
  there is no kind for a remembered decision or commitment with its own
  time scope and rollup.
- **Attention**: nothing marks a record as urgently needing the owner, with
  a note, a snooze and an escalation path.
- **Task lifecycle kinds**: a task whose completion is detectable from
  synced evidence (a reply sent, a review submitted) cannot declare that,
  so nothing can close it deterministically; an agent opens obligations,
  and nothing should depend on an LLM noticing they were met.
- **Match audit as declared properties**: STORY-03 mints a story-local
  `matchverdict` kind because the transcript kind declares no confidence or
  signals properties of its own.
- **No case covers a proposed pointer change**: a reference is a property,
  so a proposed patch can change one and "add this person to that team" has
  a decision-loop path. STORY-04's judgment call is a plain property patch,
  and nothing exercises the pointer version.
- **Proposal legibility**: a `recordpatchrequest` reviewer must join their
  own context; the proposal record could resolve what the change is about
  and where it came from at propose time.

## Implementation notes (for when these are built)

- The story-local declarations (the STORY-02 resolver function, the
  `scorecandidates` tool, and the three agents (`transcriptMatcher`,
  `actionItemExtractor`, `changeRequestReviewer`)) live in one
  story-local package, applied via `POST /api/v1/vocabulary/apply` as test
  fixtures; they are not `kinds/` additions.
- The fake LLM (stories 03, 04, 05) is one stub serving
  `POST /chat/completions`, answered by per-model RESPONDERS: deterministic
  functions of the message history, because request ids are server-assigned.
  The matcher's pick comes out of the scoring tool's actual answer; the
  JUDGMENTS (who spoke, what the action items are, silence on the quiet
  meeting) are scripted per delivery, because the model is the mock and the
  machinery around it is what is under test. The three agents use different model ids so the distinct-actor assertions mean
  something.
- Triggers are woken explicitly (`…/trigger/{id}/wake`), but the server's
  own dispatch tick races every wake, so the stories assert settled state
  (records, run rows, decisions) and never who delivered first.
- The `actionItemExtractor` agent declares `resume: never`: a decision resuming the
  proposing thread would re-enter the scripted model with a trimmed history,
  and the stories assert exact proposal counts.
- Each story is one report case (STORY-01 … STORY-06) recorded like the
  slice cases; the ecosystem is shared state within a run.
- Every assertion must be one that FAILS against a lazy implementation (the
  wrong-event match, the resource-address person, the sourceless task, the
  noisy quiet window), or it tests nothing.
