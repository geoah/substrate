import { describe, expect, it } from "vitest"

import {
  agentLimits,
  askedByAgent,
  costWords,
  durationWords,
  failureGroups,
  isConversation,
  periodTotals,
  policySentence,
  runOutlastsInvocation,
  runDurationMs,
  runStartWords,
  runStats,
  runUnpriced,
  tokenWords,
} from "./agent-runs"
import type { SubstrateRecord } from "@/lib/api/types"

function rec(properties: Record<string, unknown>): SubstrateRecord {
  return {
    id: "x",
    kind: "",
    properties,
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  }
}

describe("agent runs", () => {
  it("sums what the runs spent and counts the ones that didn't finish", () => {
    expect(
      runStats([
        rec({ status: "ok", totalTokens: 1000, costUSD: 0.01 }),
        rec({ status: "error", totalTokens: 200 }),
        rec({ status: "overbudget", costUSD: 0.02 }),
        rec({ status: "running" }),
      ])
    ).toEqual({
      runs: 4,
      failed: 2,
      tokens: 1200,
      cost: 0.03,
      // The error run spent 200 tokens and recorded no cost.
      unpriced: 1,
    })
    // The loop writes 0 for a model its provider row has no price for.
    expect(runUnpriced(rec({ totalTokens: 50, costUSD: 0 }))).toBe(true)
    expect(runUnpriced(rec({ totalTokens: 0, costUSD: 0 }))).toBe(false)
    expect(runUnpriced(rec({ totalTokens: 50, costUSD: 0.001 }))).toBe(false)
  })

  it("says what started a run, and which ones are conversations", () => {
    expect(runStartWords(rec({ mode: "schedule" }))).toBe("On a schedule")
    expect(runStartWords(rec({}))).toBe("A run")
    expect(isConversation(rec({ mode: "chat" }))).toBe(true)
    expect(isConversation(rec({ mode: "judge" }))).toBe(false)
  })

  it("reads tokens and dollars the way a person does", () => {
    expect(tokenWords(840)).toBe("840")
    expect(tokenWords(12_345)).toBe("12.3k")
    expect(tokenWords(250_000)).toBe("250k")
    expect(tokenWords(1_200_000)).toBe("1.2M")
    expect(costWords(0)).toBe("$0")
    expect(costWords(0.0004)).toBe("under $0.001")
    expect(costWords(0.0213)).toBe("$0.021")
    expect(costWords(3.456)).toBe("$3.46")
  })

  it("says how long a run took, and nothing while it runs", () => {
    const start = "2026-09-29T10:00:00Z"
    expect(
      runDurationMs(
        rec({
          status: "ok",
          startedAt: start,
          finishedAt: "2026-09-29T10:00:04Z",
        })
      )
    ).toBe(4_000)
    expect(
      runDurationMs(
        rec({
          status: "running",
          startedAt: start,
          finishedAt: "2026-09-29T10:00:04Z",
        })
      )
    ).toBeUndefined()
    expect(
      runDurationMs(rec({ status: "ok", startedAt: start }))
    ).toBeUndefined()
    expect(durationWords(4_000)).toBe("4.0s")
    expect(durationWords(3 * 60_000)).toBe("3 min")
    expect(durationWords(90 * 60_000)).toBe("1.5 h")
    expect(durationWords(3 * 24 * 3_600_000)).toBe("3 days")
  })

  it("tells a span no one invocation can work from one it can", () => {
    const start = "2026-09-29T10:00:00Z"
    const settled = (end: string) =>
      rec({ status: "ok", startedAt: start, finishedAt: end })
    expect(runOutlastsInvocation(settled("2026-09-29T10:09:59Z"))).toBe(false)
    // An invocation's wall clock is capped at 600 seconds.
    expect(runOutlastsInvocation(settled("2026-09-29T11:00:00Z"))).toBe(true)
  })

  it("tells a run another agent asked for from one nobody asked for", () => {
    expect(askedByAgent(rec({ mode: "subagent" }))).toBe(true)
    expect(
      askedByAgent(
        rec({
          mode: "chat",
          parent: { ref: "substrate.reamde.dev/llm/thread/p" },
        })
      )
    ).toBe(true)
    expect(askedByAgent(rec({ mode: "chat" }))).toBe(false)
    expect(askedByAgent(rec({ mode: "judge" }))).toBe(false)
  })

  describe("period totals", () => {
    const now = Date.parse("2026-09-29T12:00:00Z")
    const at = (hours: number) =>
      new Date(now - hours * 3_600_000).toISOString()
    const threads = [
      rec({ status: "ok", startedAt: at(1), costUSD: 0.01, totalTokens: 100 }),
      rec({
        status: "error",
        startedAt: at(30),
        costUSD: 0.02,
        totalTokens: 200,
      }),
      rec({ status: "ok", startedAt: at(24 * 10), costUSD: 0.04 }),
      rec({ status: "ok", startedAt: at(24 * 40), costUSD: 1 }),
      rec({ mode: "subagent", status: "ok", startedAt: at(2), costUSD: 0.5 }),
    ]

    it("sums each period's runs by when they started, and keeps asked-for runs apart", () => {
      const [day, week, month] = periodTotals(threads, { now })
      expect(day.started).toMatchObject({ runs: 1, cost: 0.01, tokens: 100 })
      expect(week.started).toMatchObject({ runs: 2, failed: 1, tokens: 300 })
      expect(week.started.cost).toBeCloseTo(0.03)
      expect(month.started.runs).toBe(3)
      expect(month.started.cost).toBeCloseTo(0.07)
      // Never added to the runs it started: those already count it.
      expect(day.asked).toMatchObject({ runs: 1, cost: 0.5 })
      for (const p of [day, week, month]) {
        expect(p.started).toMatchObject({ complete: true, spendComplete: true })
      }
    })

    it("marks a period the read did not reach back through as incomplete", () => {
      // The read stopped at its bound; its oldest run is 30 hours old.
      const [day, week, month] = periodTotals(threads.slice(0, 2), {
        now,
        truncated: true,
      })
      expect(day.started.complete).toBe(true)
      expect(week.started).toMatchObject({
        complete: false,
        spendComplete: false,
      })
      expect(month.started.complete).toBe(false)
    })

    // A chat opened 40 days ago and continued an hour ago spent some of its
    // total today, and its thread cannot say how much.
    it("marks the spend of a period an older run went on into as a floor", () => {
      const continued = rec({
        mode: "chat",
        status: "ok",
        startedAt: at(24 * 40),
        finishedAt: at(1),
        costUSD: 2,
        totalTokens: 9_000,
      })
      const [day, week, month] = periodTotals([threads[0], continued], {
        now,
      })
      for (const p of [day, week, month]) {
        // Its runs are every run that started in the period...
        expect(p.started).toMatchObject({ runs: 1, complete: true })
        // ...but not all the period's spend.
        expect(p.started.spendComplete).toBe(false)
        expect(p.started.cost).toBeCloseTo(0.01)
      }
      // A run still going that started before the period is in it too: its
      // row moves on every turn.
      const [running] = periodTotals(
        [
          {
            ...rec({ status: "running", startedAt: at(30) }),
            updatedAt: at(0.1),
          },
        ],
        { now }
      )
      expect(running.started.spendComplete).toBe(false)
    })
  })

  it("groups the runs that didn't finish by status and reason, most frequent first", () => {
    const groups = failureGroups([
      rec({
        status: "error",
        reason: "401\ntrace",
        startedAt: "2026-09-29T10:00:00Z",
      }),
      rec({ status: "ok" }),
      rec({
        status: "overbudget",
        reason: "maxTurns",
        startedAt: "2026-09-29T11:00:00Z",
      }),
      rec({
        status: "error",
        reason: "401",
        startedAt: "2026-09-29T12:00:00Z",
      }),
      rec({ status: "error", startedAt: "2026-09-28T12:00:00Z" }),
    ])
    expect(groups.map((g) => [g.status, g.reason, g.count])).toEqual([
      ["error", "401", 2],
      ["overbudget", "maxTurns", 1],
      ["error", undefined, 1],
    ])
    expect(groups[0].latest.properties.startedAt).toBe("2026-09-29T12:00:00Z")

    // The newest failure is the one that settled last, whenever it started:
    // an old chat that failed today is newer than a run that failed at noon.
    const [group] = failureGroups([
      rec({
        status: "error",
        reason: "401",
        startedAt: "2026-09-29T12:00:00Z",
        finishedAt: "2026-09-29T12:00:05Z",
      }),
      rec({
        status: "error",
        reason: "401",
        startedAt: "2026-09-20T09:00:00Z",
        finishedAt: "2026-09-29T15:00:00Z",
      }),
    ])
    expect(group.latest.properties.startedAt).toBe("2026-09-20T09:00:00Z")
  })

  it("reads the limits one run runs under, the loader's default where the agent is silent", () => {
    const limits = agentLimits(
      rec({
        budgets: { maxTurns: 16 },
        subagents: [{ ref: "substrate.reamde.dev/core/agent/a" }],
        permissions: { reads: { kinds: ["*"], budgets: { rows: 50 } } },
      })
    )
    expect(limits.map((l) => [l.path.join("."), l.value, l.declared])).toEqual([
      ["budgets.maxTurns", 16, true],
      ["budgets.maxToolCalls", 32, false],
      ["budgets.deadlineSeconds", 120, false],
      ["budgets.depth", 3, false],
      ["permissions.reads.budgets.calls", 16, false],
      ["permissions.reads.budgets.rows", 50, true],
    ])
    // No sub-agents, no reads: neither depth nor the read budgets bound it.
    expect(agentLimits(rec({})).map((l) => l.path.join("."))).toEqual([
      "budgets.maxTurns",
      "budgets.maxToolCalls",
      "budgets.deadlineSeconds",
    ])
  })

  it("says a policy as a sentence about the agent", () => {
    const TASK = "samples.substrate.reamde.dev/tasks/task"
    expect(
      policySentence(rec({ action: "gate", selector: { kinds: [TASK] } }))
    ).toEqual({
      text: "Asks you before it can add, change and delete tasks",
      tone: "warn",
      everyone: true,
    })
    expect(
      policySentence(
        rec({
          action: "refuse",
          selector: { kinds: [TASK], ops: ["delete"], agents: ["a/b/c"] },
        })
      )
    ).toMatchObject({
      text: "Can’t delete tasks",
      tone: "bad",
      everyone: false,
    })
    expect(policySentence(rec({ action: "allow", selector: {} })).text).toBe(
      "Can add, change and delete anything in your data without asking"
    )
  })
})
