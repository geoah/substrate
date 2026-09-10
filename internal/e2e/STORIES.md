# The user stories

The story-level cases of `mise run test:e2e`: whole scenarios a real
repository lives through, replayed against the live server and left in place
for a human to browse. Where a slice case pins one endpoint, a story pins a
pipeline: data arriving the way importers deliver it, functions and agents
acting on it, and a graph at the end that a reviewer can walk. What each story
asserts is in its own case (`caseStory01` … `caseStory06`, registered with its
title and its claim in `cases_test.go`) and in the report the run writes under
`.dev/e2e/`.

## Ground rules

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
- **Every assertion must be one that FAILS against a lazy implementation**
  (the wrong-event match, the resource-address person, the sourceless task,
  the noisy quiet window), or it tests nothing. STORY-05 is the precision
  control of the set: silence is its pass condition, it has no happy path to
  hide behind, and it must keep passing forever.

## Authoring rules

- The story-local declarations (the STORY-02 resolver function, the
  `scorecandidates` tool and the three agents `transcriptMatcher`,
  `actionItemExtractor` and `changeRequestReviewer`) live in one story-local
  package applied through `POST /api/v1/vocabulary/apply` as test fixtures.
  They are not `kinds/` additions.
- The fake LLM is one stub serving `POST /chat/completions`, answered by
  per-model RESPONDERS: deterministic functions of the message history,
  because request ids are server-assigned. The three agents use different
  model ids, so the distinct-actor assertions mean something.
- Triggers are woken explicitly (`…/trigger/{id}/wake`), but the server's own
  dispatch tick races every wake, so the stories assert settled state
  (records, run rows, decisions) and never who delivered first.
- `actionItemExtractor` declares `resume: never`: a decision resuming the
  proposing thread would re-enter the scripted model with a trimmed history,
  and the stories assert exact proposal counts.
- The ecosystem is shared state within a run: STORY-01 writes it and every
  later story builds on what it left.

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
