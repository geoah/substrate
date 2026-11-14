/** One sentence and one button for every cause a run can settle with, the
 * server's words kept apart for Details, and a fallback by status for a
 * cause this build does not know and for a thread from before causes. */

import { describe, expect, it } from "vitest"

import golden from "@/lib/api/agentcauses.golden.json"
import { turnOutcome, waitLeft, type SettledRun } from "./turn-outcome"

const failed = (cause: string, more: Partial<SettledRun> = {}): SettledRun => ({
  status: "error",
  cause,
  provider: "anthropic",
  reason: "POST …/v1/messages: 429 Too Many Requests {…}",
  ...more,
})

describe("turnOutcome", () => {
  it("has a sentence and a button for every cause the server can send", () => {
    for (const cause of golden) {
      const status = ["turn_limit", "tool_call_limit", "deadline"].includes(
        cause
      )
        ? "overbudget"
        : ["requested", "shutdown"].includes(cause)
          ? "stopped"
          : "error"
      const o = turnOutcome({ status, cause, provider: "anthropic" }, "Moss")
      expect(o, cause).toBeDefined()
      expect(o!.title.trim().endsWith("."), cause).toBe(true)
      expect(o!.action.label, cause).toBeTruthy()
      // The sentence is the console's, never a status code or a vendor's
      // error type.
      expect(o!.title, cause).not.toMatch(/\d{3}|_error|rate_limit/)
    }
  })

  it("says a rate limit with the wait the provider named, and tries again", () => {
    expect(
      turnOutcome(failed("provider_rate_limited", { retryAfter: 20 }), "Moss")
    ).toEqual({
      tone: "warn",
      title: "Anthropic is limiting requests.",
      detail: "Try again in 20 seconds.",
      action: { kind: "continue", label: "Try again" },
      said: "POST …/v1/messages: 429 Too Many Requests {…}",
    })
    expect(
      turnOutcome(failed("provider_rate_limited", { retryAfter: 600 }), "Moss")
        ?.detail
    ).toBe("Try again in about 10 minutes.")
    expect(
      turnOutcome(failed("provider_rate_limited"), "Moss")?.detail
    ).toBeUndefined()
  })

  it("counts the provider's wait from when the run settled, and drops it once it is over", () => {
    const settled = Date.parse("2026-11-14T10:00:00Z")
    const run = failed("provider_rate_limited", {
      retryAfter: 20,
      finishedAt: "2026-11-14T10:00:00Z",
    })
    expect(turnOutcome(run, "Moss", settled)?.detail).toBe(
      "Try again in 20 seconds."
    )
    expect(turnOutcome(run, "Moss", settled + 5_000)?.detail).toBe(
      "Try again in 15 seconds."
    )
    expect(turnOutcome(run, "Moss", settled + 19_500)?.detail).toBe(
      "Try again in 1 second."
    )
    // Reopened an hour later, the chat no longer says to wait.
    expect(
      turnOutcome(run, "Moss", settled + 3_600_000)?.detail
    ).toBeUndefined()
    expect(turnOutcome(run, "Moss", settled + 3_600_000)?.action).toEqual({
      kind: "continue",
      label: "Try again",
    })
    // A clock behind the server's never shows more than was asked.
    expect(waitLeft(run, settled - 60_000)).toBe(20)
    expect(waitLeft({ status: "error" }, settled)).toBeUndefined()
  })

  it("sends a key problem and a model problem to the provider's card", () => {
    expect(turnOutcome(failed("provider_key_refused"), "Moss")).toMatchObject({
      title: "Anthropic refused the API key.",
      action: {
        kind: "provider",
        label: "Replace the key",
        provider: "anthropic",
      },
    })
    expect(turnOutcome(failed("model_unavailable"), "Moss")).toMatchObject({
      title: "Anthropic does not offer this model to your key.",
      action: { kind: "provider", provider: "anthropic" },
    })
    expect(turnOutcome(failed("allowance_spent"), "Moss")).toMatchObject({
      title: "Today’s allowance is used up.",
      detail: "It resets at midnight UTC.",
      action: { kind: "provider", label: "Add your own key" },
    })
  })

  it("does not offer to try again what trying again cannot fix", () => {
    expect(turnOutcome(failed("context_too_long"), "Moss")?.action).toEqual({
      kind: "new-chat",
      label: "New chat",
    })
    expect(turnOutcome(failed("provider_rejected"), "Moss")?.action).toEqual({
      kind: "agent",
      label: "Open the agent",
    })
  })

  it("continues a run a budget or a stop ended", () => {
    expect(
      turnOutcome(
        {
          status: "overbudget",
          cause: "turn_limit",
          reason: "max turns reached (8)",
        },
        "Moss"
      )
    ).toEqual({
      tone: "warn",
      title: "Moss ran out of room before finishing.",
      detail: "It used all 8 of its turns.",
      action: { kind: "continue", label: "Continue" },
    })
    expect(
      turnOutcome({ status: "stopped", cause: "requested" }, "Moss")
    ).toEqual({
      tone: "quiet",
      title: "Moss was stopped before it finished.",
      action: { kind: "continue", label: "Continue" },
    })
    expect(
      turnOutcome({ status: "stopped", cause: "shutdown" }, "Moss")?.title
    ).toBe("The server restarted while Moss was working.")
  })

  it("falls back to the status for a cause it does not know, and for a thread from before causes", () => {
    expect(
      turnOutcome(
        { status: "error", cause: "a_cause_from_next_year", reason: "words" },
        "Moss"
      )
    ).toEqual({
      tone: "bad",
      title: "Moss ran into a problem and didn’t finish.",
      action: { kind: "continue", label: "Try again" },
      said: "words",
    })
    expect(
      turnOutcome(
        { status: "overbudget", reason: "deadline exceeded after a last turn" },
        "Moss"
      )?.detail
    ).toBe("It ran out of time.")
    expect(
      turnOutcome(
        {
          status: "stopped",
          reason: "stopped: the server shut down during the turn",
        },
        "Moss"
      )?.title
    ).toBe("The server restarted while Moss was working.")
    // A prototype's name is no cause.
    expect(
      turnOutcome({ status: "error", cause: "toString" }, "Moss")?.title
    ).toBe("Moss ran into a problem and didn’t finish.")
  })

  it("says nothing about a run that finished or is still going", () => {
    expect(turnOutcome({ status: "ok" }, "Moss")).toBeUndefined()
    expect(turnOutcome({ status: "running" }, "Moss")).toBeUndefined()
  })

  it("names the provider generically where the run names none", () => {
    expect(
      turnOutcome({ status: "error", cause: "provider_overloaded" }, "Moss")
        ?.title
    ).toBe("The provider is having trouble.")
    // And with no card to open, the button tries again.
    expect(
      turnOutcome({ status: "error", cause: "provider_key_refused" }, "Moss")
        ?.action
    ).toEqual({ kind: "continue", label: "Try again" })
  })
})
