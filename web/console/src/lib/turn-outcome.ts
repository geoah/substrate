/** How a chat turn that did not finish reads: one sentence, at most one
 * note, one button, and the server's own words kept for Details. This is the
 * one place the console turns a run's `cause` (the closed set the server
 * settles a thread with, decision 0315) into words, so no other code reads
 * a provider's prose to decide what to say.
 *
 * The switch is the `Record` below: a cause without an entry does not
 * compile, and `agentcauses.golden.test.ts` holds the union to the server's
 * set. A cause this build does not know (a newer server's), and a thread
 * from before causes, fall back to what the `status` alone says. */

import { providerName } from "@/lib/agent-chat"
import type { TurnCause } from "@/lib/api/agents"

export interface TurnOutcome {
  /** `bad` for a failure, `warn` for a turn something else cut short,
   * `quiet` for a stop somebody asked for. */
  tone: "quiet" | "warn" | "bad"
  /** What happened, in one sentence. */
  title: string
  /** One note under it, when there is something to add. */
  detail?: string
  /** The one thing to do next. */
  action: TurnAction
  /** The server's own words (the thread's `reason`), for Details. */
  said?: string
}

export type TurnAction =
  /** Go on with the thread, sending no message: Continue for a turn that
   * was cut short, Try again for one that failed. */
  | { kind: "continue"; label: "Continue" | "Try again" }
  /** Open the provider's card, where its key is written. */
  | { kind: "provider"; label: string; provider: string }
  /** Open the agent's page, where its model and its limits are. */
  | { kind: "agent"; label: string }
  /** Start a new chat with the same agent. */
  | { kind: "new-chat"; label: string }

/** What a run's record says about how it ended. */
export interface SettledRun {
  status: string
  cause?: string
  /** The server's words, whole. */
  reason?: string
  /** Seconds the provider asked to wait, counted from `finishedAt`. */
  retryAfter?: number
  /** When the run settled: the thread's `finishedAt`, or for a failure a
   * stream just reported, when it arrived. */
  finishedAt?: string | number
  /** The llm/provider row the run was on. */
  provider?: string
}

interface Words {
  /** The clock the provider's wait is read against. */
  now: number
  /** The agent's name. */
  name: string
  /** The provider's name, or "The provider" where the run names none. */
  vendor: string
  run: SettledRun
}

const CONTINUE: TurnAction = { kind: "continue", label: "Continue" }
const TRY_AGAIN: TurnAction = { kind: "continue", label: "Try again" }

/** Opens the provider's card when the run names one, else tries again. */
function card(run: SettledRun, label: string): TurnAction {
  return run.provider
    ? { kind: "provider", label, provider: run.provider }
    : TRY_AGAIN
}

/** The seconds of the provider's wait still to go at `now`, or undefined
 * once it is over or where the provider named none. The wait counts from
 * when the run settled; a run that does not say when is taken to have
 * settled just now. */
export function waitLeft(run: SettledRun, now: number): number | undefined {
  if (!run.retryAfter || run.retryAfter <= 0) return undefined
  const settled =
    typeof run.finishedAt === "number"
      ? run.finishedAt
      : run.finishedAt
        ? Date.parse(run.finishedAt)
        : now
  if (!Number.isFinite(settled)) return run.retryAfter
  const left = Math.ceil(run.retryAfter - (now - settled) / 1000)
  // A clock a little behind the server's must not show more than was asked.
  return left > 0 ? Math.min(left, run.retryAfter) : undefined
}

function waitWords(seconds: number | undefined): string | undefined {
  if (!seconds || seconds <= 0) return undefined
  if (seconds < 90) {
    return `Try again in ${seconds} ${seconds === 1 ? "second" : "seconds"}.`
  }
  const minutes = Math.ceil(seconds / 60)
  return `Try again in about ${minutes} minutes.`
}

/** An overbudget run's reason, in words (`deadlineReason` and the two
 * budget reasons in internal/engine/agentloop.go). */
function limitWords(reason: string): string | undefined {
  const turns = /^max turns reached \((\d+)\)/.exec(reason)
  if (turns) return `It used all ${turns[1]} of its turns.`
  const calls = /^max tool calls reached \((\d+)\)/.exec(reason)
  if (calls) return `It used all ${calls[1]} of its tool calls.`
  return undefined
}

type Entry = (w: Words) => Omit<TurnOutcome, "said">

const BY_CAUSE: Record<TurnCause, Entry> = {
  provider_key_refused: ({ vendor, run }) => ({
    tone: "bad",
    title: `${vendor} refused the API key.`,
    action: card(run, "Replace the key"),
  }),
  provider_rate_limited: ({ vendor, run, now }) => ({
    tone: "warn",
    title: `${vendor} is limiting requests.`,
    detail: waitWords(waitLeft(run, now)),
    action: TRY_AGAIN,
  }),
  provider_billing: ({ vendor }) => ({
    tone: "bad",
    title: `The ${vendor} account is out of credit.`,
    detail: "Add credit with the provider, then try again.",
    action: TRY_AGAIN,
  }),
  provider_overloaded: ({ vendor }) => ({
    tone: "warn",
    title: `${vendor} is having trouble.`,
    detail: "It is overloaded right now.",
    action: TRY_AGAIN,
  }),
  provider_unavailable: ({ vendor }) => ({
    tone: "warn",
    title: `${vendor} is having trouble.`,
    detail: "It could not be reached, or it failed while answering.",
    action: TRY_AGAIN,
  }),
  provider_rejected: ({ vendor }) => ({
    tone: "bad",
    title: `${vendor} refused the request.`,
    detail:
      "Trying again will not help: the agent’s model or settings need a change.",
    action: { kind: "agent", label: "Open the agent" },
  }),
  model_unavailable: ({ vendor, run }) => ({
    tone: "bad",
    title: `${vendor} does not offer this model to your key.`,
    action: card(run, "Open the provider"),
  }),
  context_too_long: () => ({
    tone: "bad",
    title: "This chat is too long for the model.",
    action: { kind: "new-chat", label: "New chat" },
  }),
  allowance_spent: ({ run }) => ({
    tone: "warn",
    title: "Today’s allowance is used up.",
    detail: "It resets at midnight UTC.",
    action: card(run, "Add your own key"),
  }),
  interrupted: ({ name }) => ({
    tone: "warn",
    title: `The server restarted while ${name} was working.`,
    action: TRY_AGAIN,
  }),
  write_failed: ({ name }) => ({
    tone: "bad",
    title: `${name} ran into a problem and didn’t finish.`,
    detail: "The server could not save part of the reply.",
    action: TRY_AGAIN,
  }),
  internal: ({ name }) => ({
    tone: "bad",
    title: `${name} ran into a problem and didn’t finish.`,
    action: TRY_AGAIN,
  }),
  turn_limit: ({ name, run }) => ({
    tone: "warn",
    title: `${name} ran out of room before finishing.`,
    detail: limitWords(run.reason ?? "") ?? "It used all of its turns.",
    action: CONTINUE,
  }),
  tool_call_limit: ({ name, run }) => ({
    tone: "warn",
    title: `${name} ran out of room before finishing.`,
    detail: limitWords(run.reason ?? "") ?? "It used all of its tool calls.",
    action: CONTINUE,
  }),
  deadline: ({ name }) => ({
    tone: "warn",
    title: `${name} ran out of room before finishing.`,
    detail: "It ran out of time.",
    action: CONTINUE,
  }),
  requested: ({ name }) => ({
    tone: "quiet",
    title: `${name} was stopped before it finished.`,
    action: CONTINUE,
  }),
  shutdown: ({ name }) => ({
    tone: "warn",
    title: `The server restarted while ${name} was working.`,
    action: CONTINUE,
  }),
}

/** The reason the loop settles a turn the server's shutdown ended with
 * (`reasonShutdown` in internal/engine/agentloop.go), read for a thread
 * from before causes. */
const SHUTDOWN_REASON = "stopped: the server shut down during the turn"

/** What a thread from before causes, or one with a cause this build does
 * not know, says by its status alone. */
function byStatus({
  name,
  run,
  now,
}: Words): Omit<TurnOutcome, "said"> | undefined {
  const reason = (run.reason ?? "").split("\n")[0].trim()
  switch (run.status) {
    case "stopped":
      return reason === SHUTDOWN_REASON
        ? BY_CAUSE.shutdown({ name, vendor: "", run, now })
        : BY_CAUSE.requested({ name, vendor: "", run, now })
    case "overbudget":
      return {
        tone: "warn",
        title: `${name} ran out of room before finishing.`,
        detail: reason.startsWith("deadline exceeded")
          ? "It ran out of time."
          : (limitWords(reason) ?? "It reached a limit."),
        action: CONTINUE,
      }
    case "error":
      return BY_CAUSE.internal({ name, vendor: "", run, now })
    default:
      return undefined
  }
}

/** How a reply the model did not finish reads, when the run itself settled
 * fine: the row's `cutBy` is `length` (the model's output limit) or `filter`
 * (the provider withheld the rest). A reply a stop or a failure cut is said
 * by its thread's own outcome. The stored rows are in loop order; the
 * engine's own rows (a summary, a resolution) are not turns of the
 * conversation and are passed over. */
export function cutReplyOutcome(
  rows: { properties: Record<string, unknown> }[],
  provider?: string
): TurnOutcome | undefined {
  for (let i = rows.length - 1; i >= 0; i--) {
    const { role, cutBy } = rows[i].properties
    if (role === "system" || role === "summary") continue
    if (role !== "assistant") return undefined
    if (cutBy === "length") {
      return {
        tone: "warn",
        title: "The reply stopped at the model’s limit.",
        action: CONTINUE,
      }
    }
    if (cutBy === "filter") {
      const vendor = provider ? providerName(provider) : "The provider"
      return {
        tone: "warn",
        title: `${vendor} withheld the rest of this reply.`,
        action: CONTINUE,
      }
    }
    return undefined
  }
  return undefined
}

function isCause(cause: string | undefined): cause is TurnCause {
  return cause !== undefined && Object.hasOwn(BY_CAUSE, cause)
}

/** How a settled run that did not finish reads; undefined for one that
 * finished or is still running. */
export function turnOutcome(
  run: SettledRun,
  name: string,
  now: number = Date.now()
): TurnOutcome | undefined {
  const unfinished =
    run.status === "error" ||
    run.status === "overbudget" ||
    run.status === "stopped"
  if (!unfinished) return undefined
  const words: Words = {
    now,
    name,
    vendor: run.provider ? providerName(run.provider) : "The provider",
    run,
  }
  const outcome = isCause(run.cause)
    ? BY_CAUSE[run.cause](words)
    : byStatus(words)
  if (!outcome) return undefined
  // A stop's and a budget's reason is the engine's own stock phrase, which
  // the sentence already says; only a failure's words add anything.
  const said = run.status === "error" ? run.reason?.trim() : undefined
  return { ...outcome, ...(said ? { said } : {}) }
}
