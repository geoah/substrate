# Plan: the playground — rebuild it, then walk the console

Status: a runbook, not a design. Part 1 tells an AGENT (or a patient human)
how to rebuild the sample environment from nothing; part 2 tells the OWNER
how to see every piece of the interaction model
([thread-interactions.md](thread-interactions.md), decisions 0003 through
0008) working in the console. The seed files live beside this page in
[playground/](playground/), and everything here runs against a throwaway
local dev substrate: `admin` / `adminadminadmin`, invite code `let-me-in`,
no second factor.

## Part 1: rebuild the environment (for an agent)

Prerequisites: this repo, `mise install` done, Docker running, and
`ANTHROPIC_API_KEY` in the environment (a dev box gets it from
`.mise.local.toml`). Every step is a command; expected outcomes are stated
so a failure is visible immediately.

1. **Kill every local substrate server**, whatever port it took:

   ```bash
   pkill -f "bin/substrate$" || true
   ```

   Afterwards `ss -tln | grep :8080` prints nothing.

2. **Fresh server on :8080.** The dev script keeps its state under `.dev/`;
   the wipe throws the database away, which is the only way to re-register:

   ```bash
   mise run dev:wipe
   mise run dev:up
   ```

   `mise run dev:status` says the database runs and the substrate serves
   `http://localhost:8080` with the second factor OFF.

3. **The admin account** (registration is one-shot per user):

   ```bash
   printf 'adminadminadmin\n' | bin/substratectl register \
     --server http://localhost:8080 --username admin \
     --invite-code let-me-in --password-stdin --context playground
   ```

   This stores a `playground` context in `~/.config/substratectl/config.yaml`
   whose token the curl steps below read:

   ```bash
   TOKEN=$(grep -A5 "name: playground" ~/.config/substratectl/config.yaml \
     | grep "token:" | head -1 | awk '{print $2}')
   ```

4. **Install the llm examples and key the provider.** The bundle ships the
   assistant, the editor, the arbiter and the judge; the key makes them run:

   ```bash
   curl -s -X POST -H "Authorization: Bearer $TOKEN" \
     "http://localhost:8080/api/v1/core.substrate.reamde.dev/catalog/llm.examples.substrate.reamde.dev%2Fllm/install"
   jq -n --arg k "$ANTHROPIC_API_KEY" '{properties:{apiKey:$k}}' \
     | curl -s -X PATCH -H "Authorization: Bearer $TOKEN" \
       -H "Content-Type: application/json" -d @- \
       "http://localhost:8080/api/v1/core.substrate.reamde.dev/llmproviders/anthropic"
   ```

   The install answers `"installed": true`; the patch echoes the provider
   with the key redacted.

5. **Seed the playground**, three files, in order:

   ```bash
   bin/substratectl --context playground apply -f docs/plans/playground/10-vocabulary.yaml
   bin/substratectl --context playground apply -f docs/plans/playground/20-policies.yaml
   bin/substratectl --context playground apply -f docs/plans/playground/30-records.yaml
   ```

   That lands: the `playground.substrate.reamde.dev` authority with `task`
   (name, notes, a todo/doing/done machine) and `note` (name, content); the
   `completetask` function and the `shredder` function (whose declaration
   says `confirmation: always`, the floor no policy loosens); three agents —
   `planner` (graphql + propose + ask, writes nothing directly), `taskbot`
   (mutate + completetask over tasks) and `notekeeper` (mutate + shredder
   over notes); three policies — `gate-tasks` (taskbot's task writes gate to
   the judge, enforce, autoAccept 0.85, thread context), `refuse-note-deletes`
   (op delete on notes refuses outright) and `advise-note-edits` (notekeeper's
   note writes gate with the judge only ADVISING); four tasks and three notes.

6. **One sanity call** proving the whole chain without the console:

   ```bash
   curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
     -d '{"input":"Create a task named \"Book the dentist\" with a note that mornings work best."}' \
     "http://localhost:8080/api/v1/core.substrate.reamde.dev/agents/playground.substrate.reamde.dev%2Ftaskbot/call"
   ```

   Expected, in order: the reply says the write was HELD for review; within
   about ten seconds the judge accepts (list
   `/api/v1/core.substrate.reamde.dev/recordpatchrequests` — the one request
   is `decision: accepted` with a `policy/verdict` annotation naming
   `substrateJudge`, a confidence, and `outcome: accepted`); the task exists
   in `/api/v1/playground.substrate.reamde.dev/tasks`; and the taskbot's
   thread grew a `system` row (`event: proposalDecision`) followed by the
   agent's acknowledgement. If all four hold, the playground is live.

## Part 2: walk the console (for the owner)

Open `http://localhost:8080`, sign in as `admin` / `adminadminadmin`. Each
numbered stop shows one shipped piece; do them in order the first time,
since later stops read what earlier ones created.

1. **Records and vocabulary.** Data → tasks: the four seeded tasks plus
   "Book the dentist" from the sanity call, each with its status chip. Open
   one: properties, the state machine, the changelog history at the bottom.
   Registry shows `playground.substrate.reamde.dev` beside the installed
   llm examples.

2. **A transcript that explains itself.** Agents →
   `playground.substrate.reamde.dev/taskbot`: open the sanity thread. Read
   it bottom-up: the mutate tool card is AMBER "held" (not a red failure),
   carrying the proposal card inline; the dashed event line says the
   proposal was accepted with the new version; the agent's last turn
   acknowledges it. Expand the tool card: the engine-stamped changes name
   the request record.

3. **The judge's audit.** On that proposal card, the "Judge:" line shows
   the verdict, its confidence and the outcome. Click through to the full
   change request: the `policy/verdict` annotation carries the policy at
   its revision, the judge's thread, and the request version it read. Open
   the judge's own thread under Agents →
   `llm.examples.substrate.reamde.dev/substrateJudge`: the envelope it was
   handed, its one-line verdict, its cost.

4. **A gate YOU decide.** Chat with taskbot: *"Rename every task to
   'stuff'"*. The judge's criteria call bulk edits escalation-worthy, so
   expect the request to stay pending (if the judge surprises you, its
   confidence was high; read the verdict line either way). A pending card
   shows Accept, Reject, and "Accept + always allow" — the remedy. Click
   the remedy once to READ it: the exact one-agent, one-kind, one-op rule
   it would mint, behind its own confirmation. Reject instead, and watch
   the thread resume with the agent taking the rejection on the chin.

5. **The soft ask.** Chat with
   `playground.substrate.reamde.dev/planner`: *"Plan my weekend chores,
   ask me first."* The ask tool card renders as a FORM in the thread:
   radios and checkboxes per question, Dismiss beside Answer. Answer it;
   the event line records your selections; the thread resumes and the plan
   uses your answers. Then propose something through it and decide the
   proposal from the same transcript.

6. **Advise mode.** Chat with `notekeeper`: *"Rewrite the pasta note to be
   friendlier."* The write gates, the judge only ADVISES
   (`advise-note-edits`), so the card stays pending with the judge's
   recommendation printed for you. Decide it yourself.

7. **Refusal and the floor.** Still with notekeeper: *"Delete the gift
   ideas note."* Two different walls, both honest in the transcript: a
   mutate delete refuses outright (`refuse-note-deletes` names itself),
   and the `shredder` tool comes back HELD instead — its declaration says
   `confirmation: always`, so the effect became a request no policy can
   wave through. Accept that request and the note tombstones.

8. **The policy door is the owner's.** Data → recordpatchpolicies: the
   three rules as ordinary records you can edit — flip `gate-tasks` to
   `mode: advise`, chat with taskbot again, and the same write now waits
   for you with a recommendation. No agent can do what you just did: the
   kind refuses installed code by name.

9. **Everything is rows.** Changelog: the whole session as deltas — the
   gated request's put, the judge's audit write under the `substrate`
   actor, the decision under `policy:gate-tasks`, the resumes. Nothing in
   this walkthrough happened off the record, which is the point of the
   design: any GraphQL consumer could have rendered every step you just
   watched.

Cleanup, when done: `mise run dev:wipe` throws the whole playground away;
the next `dev:up` is a blank substrate.
