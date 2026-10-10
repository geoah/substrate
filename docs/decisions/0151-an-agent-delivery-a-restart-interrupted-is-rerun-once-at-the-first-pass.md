---
status: proposed
date: 2026-10-10
decision-makers: George Antoniadis
---

# 0151. An agent delivery a restart interrupted is rerun once, at the first pass after the restart

## Context and problem statement

When a server stops while an agent delivery's loop runs, the next open parks
the delivery as interrupted, and
[0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md),
[0068](0068-an-accepted-webhook-is-a-pending-entry-in-the-delivery-ledger.md),
[0121](0121-a-function-body-runs-an-agent-under-permissions-agents.md)
and
[0142](0142-a-schedule-fire-that-settles-retires-its-triggers-older-parked-fires.md)
leave it to a person: "nothing redelivers by itself". On 2026-10-06 each of
four rolls of one deployment left one or two parked deliveries of the same
agent trigger that needed `substratectl trigger retry`, or the work was lost
([#884](https://github.com/geoah/substrate/issues/884)).

## Considered options

- Rerun each delivery parked as interrupted at attempt 1 once, from the first
  dispatcher pass after the open, after rewriting its row to attempt 2
- Rerun an interrupted delivery at every pass until it settles
- Rerun it from the open itself, inside the sweep that parks it
- Keep the retry by hand (the 0064 position)

## Decision outcome

Chosen: one rerun from the first pass, because it removes the hand step for
a roll that lands mid-run and bounds what a repeated run costs.

One rerun, because each run may spend tokens and write records, and a run
the server stopped twice may be why it stops: a loop longer than every
rollout's drain, a provider call that hangs. A rerun at every pass repeats
that cost at every restart. One rerun covers the observed case.

From the pass and not the open, for 0068's reason: only `substrated`
dispatches, and an operator's process (`repository rebuild`, `user reset`)
opens the repository and must run nothing. A read-only process appends
nothing. `ProcessTriggers` starts the walk beside `resumeWebhooks`, once per
open, as one detached task that reruns one delivery at a time, so a slow loop
does not hold the pass. Once per open is enough, because only the open-time
sweep parks a delivery as interrupted at attempt 1. A walk that could not
read its rows starts again at the next pass.

The order of the steps keeps it to one run per interruption. For each row
the walk:

1. takes the in-process claim (`runningClaims`, compare-and-swap), so a
   hand already retrying or forgetting the row keeps it and a hand arriving
   later answers `409`;
2. under that claim and the row lock, checks again that the row is still
   interrupted at attempt 1;
3. rewrites it to attempt 2 with `rerunAgentError`, on a delivery entry of
   its own, durable before anything runs
   ([0062](0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md));
4. runs the delivery through the retry's own body (`retryHeldFailure`,
   which `RetryTriggerFailure` also calls);
5. releases the claim.

A stop during the rerun leaves attempt 2 and a text the open-time sweep does
not rewrite and the walk does not select, so the row waits for a person. A
rerun that settles retires the row; one that fails parks at attempt 2 with
its error, as a retry by hand would. A row whose trigger is disabled or whose
callable does not resolve is skipped and stays parked. So is a row whose
agent is at a spend cap, checked before the rewrite as a retry by hand is
checked before its hold: rerun, it would be refused inside its loop and
park at attempt 2 having run nothing. A function body that runs an agent
meets the cap inside the body, as a dispatched delivery does. One log line per
delivery names the trigger, the failure id, the callable the rerun ran and
the outcome as a fixed word, never the error, which may quote the request or
the model.

The rerun delivers what a retry by hand delivers: the parked change or fire,
to the trigger as the trigger stands at the rerun, callable and arguments
included. A trigger edited to another callable since the claim reruns the
parked delivery on the new callable. The parked row records the delivery and
not the callable, and a retry by hand has always run the current definition,
so one rule holds for both.

A function body that runs an agent claims its delivery when the agent's
thread opens (0121), and a stop after that leaves the same interrupted
claim, which the walk reruns the same way. The rerun runs the body again from
its start, not only its agent: whatever the body did outside the substrate
before its agent call happens again, an outbound request included, and the
agent runs again under a fresh thread. A body interrupted twice waits for a
person, like an agent.

The interrupted row's text said "nothing reruns it by itself", which is no
longer true, so it changes. The open-time sweep rewrites a row an earlier
binary parked in the old words, so that row is rerun too, whatever its age.

Rerunning at every pass was rejected for the unbounded cost above. Rerunning
from the open was rejected because operator processes open repositories.
The retry by hand is the status quo whose cost #884 reports.

This amends five records. 0064's "nothing redelivers by itself" and its
consequence that an interrupted agent delivery waits for a hand now hold
only after the one rerun. 0068's named exception still stands for the webhook
resume, which does not run an agent claim, but the interrupted agent fire is
now rerun once like every agent delivery.
[0091](0091-a-parked-delivery-is-retried-or-forgotten.md)'s two verbs stay
the two a person has: the rerun is a retry the server runs once and ends the
row the same ways, and the rejection of age-based expiry stands, since the
rerun reacts to a restart and expires nothing. 0121's rule that a body that
opened a thread is not run again on its own, and its consequence that
nothing redelivers a killed one, now hold only after the one rerun. 0142's
limit for agent claims stands, and the rerun's text joins the texts a settled
schedule fire never retires.

### Consequences

- Good, because a roll that lands mid-run costs no hand step and loses no
  work.
- Good, because a run interrupted twice stays parked with an attempt count
  and an error that say so, and no restart loop reruns it.
- Bad, because an interrupted run's cost repeats once without a person
  reading its thread first: the model's turns and the records the loop wrote
  under fresh ids. A tool's idempotency key derives from the delivery, not
  the attempt, so an effectful tool that honors keys still fires once.
- Bad, because a function body's rerun runs the whole body again, not only
  its agent.
- Bad, because a trigger edited between the claim and the rerun runs the
  parked delivery on its new callable, which may not expect it.
- Bad, because the first pass after a start reruns every interrupted loop,
  one at a time, so a repository with many spends their cost right after the
  boot.
- Bad, because a claim a live process lost (a canceled request, a panic) that
  nobody retried or forgot is rerun at the next start, however long after the
  loss: the open cannot tell it from a stop.
- Bad, because an earlier binary knows neither new text: after a rollback,
  its settled schedule fire retires such a row of the same trigger, and its
  open reruns nothing.

### Confirmation

`internal/engine/agentinterrupt_db_test.go`:
`TestTheFirstPassAfterARestartRerunsAnInterruptedAgentRun` reopens over an
interrupted delivery and shows one pass rerun it (a second thread settled
ok, the row gone, the attempt 2 rewrite on a delivery entry below the rerun's
first write, one log line).
`TestAnAgentRunInterruptedDuringItsRerunStaysParked` stops the server again
mid-rerun and shows the row at attempt 2, refused to a hand while it ran, and
left alone by the next open's pass.
`TestAHandRetryRacingTheRerunRunsTheDeliveryOnce` holds a hand's retry while
the pass runs and shows one loop. `TestARerunRunsTheCallableTheTriggerNamesNow`
points the trigger at another agent between the claim and the restart and
shows the rerun run that agent and the log line name it.
`TestTheFirstPassAfterARestartRerunsAFunctionBodysInterruptedAgent` kills the
process while a function body's agent runs and shows the body run again.
`TestARerunWaitsWhileItsAgentIsAtASpendCap` holds the rerun at a spend cap
and shows the row untouched, then rerun at the next open once the cap admits
it. `TestARestartSettlesTheAgentRunItInterrupted` shows the open alone reruns
nothing and rewrites the old text.

## More information

Reopen if a second writer process shares one repository, when
`runningClaims` stops serializing a hand and the rerun, or if one rerun proves
too few for a deployment whose drains routinely cut a run twice; the answer
then is a count, never a clock.
