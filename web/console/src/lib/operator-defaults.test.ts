/** The rules the Providers page reads the operator's defaults by: where an
 * AI model row's key comes from and the pill it wears, whose key a card
 * names, the operator's name, and the allowance read in the reader's own
 * time zone (the suite runs in Europe/Athens, UTC+2 in November). */

import { describe, expect, it } from "vitest"

import type { OperatorDefault, SubstrateRecord } from "@/lib/api/types"

import {
  modelKey,
  modelName,
  operatorWords,
  packageWhoseKey,
  percentOf,
  resetWords,
  spendSplit,
  welcomeFacts,
} from "./operator-defaults"

function row(id: string, properties: Record<string, unknown> = {}) {
  return { id, properties } as unknown as SubstrateRecord
}

function item(
  over: Partial<OperatorDefault> & Pick<OperatorDefault, "id">
): OperatorDefault {
  return {
    slot: "llm",
    label: over.id,
    source: "operator",
    operatorOffers: true,
    readiness: "ready",
    ...over,
  }
}

const CRANE = operatorWords({ name: "Crane" })

describe("modelKey", () => {
  it("reads the operator's item: On, Set up, Needs attention", () => {
    expect(modelKey(row("openai"), item({ id: "llm/openai" })).state).toBe("on")
    expect(
      modelKey(
        row("openai"),
        item({ id: "llm/openai", source: "none", readiness: "not-set-up" })
      ).state
    ).toBe("setup")
    const failing = modelKey(
      row("openai", { apiKey: "••••" }),
      item({
        id: "llm/openai",
        source: "own",
        readiness: "failing",
        message: "401",
      })
    )
    expect(failing).toMatchObject({ state: "attention", message: "401" })
  })

  it("names the operator's models only where its key serves the row", () => {
    const models = ["gpt-5"]
    expect(
      modelKey(row("openai"), item({ id: "llm/openai", models })).models
    ).toEqual(models)
    expect(
      modelKey(row("openai"), item({ id: "llm/openai", source: "own", models }))
        .models
    ).toBeUndefined()
  })

  it("falls back to the row's own key without the operator's read", () => {
    expect(modelKey(row("a", { apiKey: "••••" }), undefined)).toEqual({
      state: "on",
      source: "own",
      operatorOffers: false,
    })
    expect(modelKey(row("a"), undefined).state).toBe("setup")
  })
})

describe("names", () => {
  it("titles a row by its label, the seeded labels as the vendor's name", () => {
    expect(modelName(row("openai", { label: "openai" }))).toBe("OpenAI")
    expect(modelName(row("router", { label: "OpenRouter" }))).toBe("OpenRouter")
    expect(modelName(row("gemini"))).toBe("Gemini")
  })

  it("names the operator by its site name, or as this server", () => {
    expect(CRANE.possessive).toBe("Crane’s")
    expect(operatorWords({}).Name).toBe("This server")
    expect(operatorWords(undefined).possessive).toBe("this server’s")
    expect(operatorWords({ name: "  " }).name).toBe("this server")
  })
})

describe("packageWhoseKey", () => {
  const pkg = "providers.substrate.reamde.dev/firecrawl"

  it("names the operator's tool key, and the reader's own where they replaced it", () => {
    const tool = item({ id: `function/${pkg}`, slot: "function" })
    expect(packageWhoseKey([tool], pkg, "Firecrawl", CRANE)).toEqual({
      line: "Using Crane’s key",
      notice: undefined,
    })
    expect(
      packageWhoseKey([{ ...tool, source: "own" }], pkg, "Firecrawl", CRANE)
    ).toEqual({ line: "Using your key" })
  })

  it("says nothing where the operator is not in the picture", () => {
    const own = item({
      id: `function/${pkg}`,
      slot: "function",
      source: "own",
      operatorOffers: false,
    })
    expect(packageWhoseKey([own], pkg, "Firecrawl", CRANE)).toBeUndefined()
    expect(packageWhoseKey([], pkg, "Firecrawl", CRANE)).toBeUndefined()
  })
})

describe("the allowance", () => {
  it("says when it resets in the reader's own time, and how soon", () => {
    const resetsAt = "2026-11-07T00:00:00Z"
    expect(resetWords(resetsAt, Date.parse("2026-11-06T13:20:00Z"))).toBe(
      "Resets at 02:00, in 10 hours"
    )
    expect(resetWords(resetsAt, Date.parse("2026-11-06T23:59:30Z"))).toBe(
      "Resets at 02:00, in under a minute"
    )
    expect(resetWords(resetsAt, Date.parse("2026-11-06T23:15:00Z"))).toBe(
      "Resets at 02:00, in 45 minutes"
    )
  })

  it("splits today's spend by slot, leaving out the ones that spent nothing", () => {
    expect(
      spendSplit({
        centsPerDay: 200,
        usedCents: 37,
        resetsAt: "2026-11-07T00:00:00Z",
        bySlot: [
          { slot: "llm", cents: 31 },
          { slot: "embeddings", cents: 6 },
          { slot: "function", cents: 0 },
        ],
      }).map((s) => [s.word, s.cents])
    ).toEqual([
      ["chat", 31],
      ["search by meaning", 6],
    ])
  })

  it("fills a share of the day's limit, and no further", () => {
    expect(percentOf(50, 200)).toBe(25)
    expect(percentOf(250, 200)).toBe(100)
    expect(percentOf(5, 0)).toBe(100)
  })
})

describe("welcomeFacts", () => {
  it("names what works on the operator's keys and the accounts waiting on the reader", () => {
    expect(
      welcomeFacts([
        item({ id: "llm/openai" }),
        item({ id: "embeddings", slot: "embeddings" }),
        item({ id: "llm/gemini", source: "own" }),
        item({
          id: "oauth/google",
          slot: "oauth",
          label: "Google",
          readiness: "needs-connect",
        }),
      ])
    ).toEqual({ works: ["chat", "search by meaning"], connect: ["Google"] })
  })
})
