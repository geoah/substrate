/** The engine's two rules over llm/model records (internal/engine/
 * llmmodels.go), as the console reads them: what a row offers and at what
 * price, and what an agent on `runsOn` runs on. Every case here is one the
 * server decides the same way. */

import { parseAllDocuments } from "yaml"
import { describe, expect, it } from "vitest"

import type { OperatorDefault, SubstrateRecord } from "@/lib/api/types"
import {
  agentProviderId,
  agentRuns,
  agentRunsOn,
  chooseRunsOn,
  continuedRunsOn,
  modelFacts,
  priceWords,
  pricingWithout,
  refusalWords,
  rowOffers,
  runsOnValue,
  threadRuns,
  windowWords,
  type AgentRunsContext,
} from "@/lib/llm-models"
import {
  modelItem,
  modelKey,
  operatorWords,
  stateResolves,
  uncheckedKey,
} from "@/lib/operator-defaults"

const shippedModels = import.meta.glob<string>(
  "../../../../kinds/substrate.reamde.dev/llm/models.yaml",
  { query: "?raw", import: "default", eager: true }
)

const PROVIDER = "substrate.reamde.dev/llm/provider"
const MODEL = "substrate.reamde.dev/llm/model"
const AGENT = "substrate.reamde.dev/core/agent"
const THREAD = "substrate.reamde.dev/llm/thread"

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>,
  over: Partial<SubstrateRecord> = {}
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 3,
    createdAt: "2026-11-01T00:00:00Z",
    updatedAt: "2026-11-01T00:00:00Z",
    ...over,
  }
}

function row(id: string, properties: Record<string, unknown> = {}) {
  return record(PROVIDER, id, { label: id, wire: "openai", ...properties })
}

function model(
  id: string,
  rowId: string,
  modelId: string,
  properties: Record<string, unknown> = {},
  over: Partial<SubstrateRecord> = {}
) {
  return record(
    MODEL,
    id,
    {
      provider: { ref: `${PROVIDER}/${rowId}` },
      model: modelId,
      ...properties,
    },
    over
  )
}

function agentOn(runsOn: string[]) {
  return record(AGENT, "ada.example.com/llm/helper", {
    runsOn: runsOn.map((id) => ({ ref: `${MODEL}/${id}` })),
  })
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

/** The context the console builds: the operator read's items where it
 * answered (null from a server older than it), "pending" while it has
 * not. */
function context(
  rows: SubstrateRecord[] | undefined,
  models: SubstrateRecord[] | undefined,
  items: OperatorDefault[] | null | "pending" = null
): AgentRunsContext {
  const keyOf = (r: SubstrateRecord) =>
    items === "pending"
      ? uncheckedKey(r, "checking")
      : modelKey(r, items ? modelItem(items, r.id) : undefined)
  return {
    rows,
    models,
    keyOf,
    resolves: (id) => {
      const r = id ? rows?.find((x) => x.id === id) : undefined
      return r ? stateResolves(keyOf(r).state) : undefined
    },
  }
}

const OPUS = model(
  "anthropic:claude-opus-5-5",
  "anthropic",
  "claude-opus-5-5",
  {
    label: "Claude Opus 5.5",
    pricing: { inputPer1M: "4", outputPer1M: "20", cacheReadPer1M: "0.20" },
    contextWindow: 200000,
  }
)

describe("reading an agent", () => {
  it("reads runsOn's record ids, best first, and the provider of one that names it", () => {
    const agent = agentOn(["b", "a:x"])
    expect(agentRunsOn(agent)).toEqual(["b", "a:x"])
    expect(agentProviderId(agent)).toBeUndefined()
    const pair = record(AGENT, "x", {
      provider: { ref: `${PROVIDER}/openai` },
      model: "gpt-5",
    })
    expect(agentRunsOn(pair)).toBeUndefined()
    expect(agentProviderId(pair)).toBe("openai")
    // A bare id as authored reads as the id.
    expect(agentRunsOn(record(AGENT, "y", { runsOn: ["m1"] }))).toEqual(["m1"])
  })

  it("writes runsOn as the references it stores", () => {
    expect(runsOnValue(["anthropic:claude-opus-5-5"])).toEqual([
      { ref: `${MODEL}/anthropic:claude-opus-5-5` },
    ])
  })

  it("never reads a row or a model out of a record id", () => {
    const odd = model("whatever-id", "my:gateway", "anthropic/claude:opus")
    expect(modelFacts(odd)).toMatchObject({
      id: "whatever-id",
      row: "my:gateway",
      model: "anthropic/claude:opus",
    })
  })
})

describe("rule A: what a row offers", () => {
  it("prices a model from its record where the row lists no entry for it", () => {
    const offers = rowOffers(row("anthropic"), [OPUS], undefined)
    expect(offers).toHaveLength(1)
    expect(offers[0]).toMatchObject({
      model: "claude-opus-5-5",
      source: "record",
      window: 200000,
    })
    expect(priceWords(offers[0].price!)).toBe(
      "$4 input · $20 output · $0.20 cache reads"
    )
  })

  it("lets the row's own entry win over the record, and marks a different price", () => {
    const anthropic = row("anthropic", {
      pricing: [
        { model: "claude-opus-5-5", inputPer1M: "5", outputPer1M: "25" },
      ],
    })
    const [offer] = rowOffers(anthropic, [OPUS], undefined)
    expect(offer.source).toBe("row")
    expect(priceWords(offer.price!)).toBe("$5 input · $25 output")
    expect(offer.shadowsRecord).toBe(true)
    // The record's window stands where the entry states none.
    expect(offer.window).toBe(200000)
  })

  it("does not mark an entry that charges what the record charges", () => {
    const anthropic = row("anthropic", {
      pricing: [
        {
          model: "claude-opus-5-5",
          inputPer1M: "4.0",
          outputPer1M: "20",
          cacheReadPer1M: "0.2",
          contextWindow: 100000,
        },
      ],
    })
    const [offer] = rowOffers(anthropic, [OPUS], undefined)
    expect(offer.source).toBe("row")
    expect(offer.shadowsRecord).toBeUndefined()
    // The entry's own window wins.
    expect(offer.window).toBe(100000)
  })

  it("marks an entry whose unstated cache rate charges what the record states differently", () => {
    const anthropic = row("anthropic", {
      pricing: [
        { model: "claude-opus-5-5", inputPer1M: "4", outputPer1M: "20" },
      ],
    })
    // The record reads its cache at 0.20; the entry at a tenth of 4, 0.40.
    expect(rowOffers(anthropic, [OPUS], undefined)[0].shadowsRecord).toBe(true)
  })

  it("prices a partial legacy entry with zeros for what it leaves out", () => {
    const openai = row("openai", {
      pricing: [{ model: "gpt-5", inputPer1M: "1.25" }],
    })
    const [offer] = rowOffers(openai, [], undefined)
    expect(offer).toMatchObject({ model: "gpt-5", source: "row" })
    expect(priceWords(offer.price!)).toBe("$1.25 input · $0 output")
  })

  it("takes the later of two entries for one model", () => {
    const openai = row("openai", {
      pricing: [
        { model: "gpt-5", inputPer1M: "1", outputPer1M: "2" },
        { model: "gpt-5-mini", inputPer1M: "0.25", outputPer1M: "2" },
        { model: "gpt-5", inputPer1M: "3", outputPer1M: "4" },
        // No model: no entry.
        { inputPer1M: "9", outputPer1M: "9" },
      ],
    })
    const offers = rowOffers(openai, [], undefined)
    expect(offers.map((o) => o.model)).toEqual(["gpt-5", "gpt-5-mini"])
    expect(priceWords(offers[0].price!)).toBe("$3 input · $4 output")
  })

  it("lists a record without both base rates as unpriced, with its window", () => {
    const bare = model("g:flash", "gemini", "gemini-2.5-flash", {
      label: "Gemini 2.5 Flash",
      contextWindow: 1048576,
    })
    const half = model("g:pro", "gemini", "gemini-2.5-pro", {
      pricing: { inputPer1M: "1.25" },
    })
    const offers = rowOffers(row("gemini"), [bare, half], undefined)
    expect(offers).toEqual([
      {
        model: "gemini-2.5-flash",
        record: modelFacts(bare),
        window: 1048576,
      },
      { model: "gemini-2.5-pro", record: modelFacts(half) },
    ])
    expect(modelFacts(half).price).toBeUndefined()
  })

  it("reads only this row's live records, the lower id first for one model", () => {
    const offers = rowOffers(
      row("openai"),
      [
        model("z", "openai", "gpt-5", {
          pricing: { inputPer1M: "9", outputPer1M: "9" },
        }),
        model("a", "openai", "gpt-5", {
          pricing: { inputPer1M: "1.25", outputPer1M: "10" },
        }),
        model("other", "gemini", "gpt-5-mini", {
          pricing: { inputPer1M: "1", outputPer1M: "1" },
        }),
        model(
          "gone",
          "openai",
          "gpt-4o",
          { pricing: { inputPer1M: "1", outputPer1M: "1" } },
          { deletedAt: "2026-11-02T00:00:00Z" }
        ),
      ],
      undefined
    )
    expect(offers.map((o) => [o.model, o.record?.id])).toEqual([["gpt-5", "a"]])
    expect(priceWords(offers[0].price!)).toBe("$1.25 input · $10 output")
  })

  it("offers exactly the operator's list on a row the operator serves", () => {
    const openai = row("openai", {
      pricing: [{ model: "gpt-4o", inputPer1M: "1", outputPer1M: "1" }],
    })
    const gpt5 = model("o:gpt-5", "openai", "gpt-5", {
      label: "GPT-5",
      pricing: { inputPer1M: "1.25", outputPer1M: "10" },
      contextWindow: 400000,
    })
    const mini = model("o:mini", "openai", "gpt-5-mini")
    const offers = rowOffers(openai, [gpt5, mini], ["gpt-5", "o3"])
    expect(offers).toEqual([
      { model: "gpt-5", record: modelFacts(gpt5), source: "operator" },
      { model: "o3", source: "operator" },
    ])
  })

  it("removes one model's entries from the row's own list, and leaves the rest", () => {
    const openai = row("openai", {
      pricing: [
        { model: "gpt-5", inputPer1M: "1" },
        { model: "gpt-5-mini", inputPer1M: "0.25" },
        { model: "gpt-5", inputPer1M: "3" },
      ],
    })
    expect(pricingWithout(openai, "gpt-5")).toEqual([
      { model: "gpt-5-mini", inputPer1M: "0.25" },
    ])
    expect(pricingWithout(row("x"), "gpt-5")).toEqual([])
  })

  it("prices every seeded row's models from the shipped records alone", () => {
    const sources = Object.values(shippedModels)
    expect(sources).toHaveLength(1)
    const records = parseAllDocuments(sources[0]).map((doc) => {
      const d = doc.toJS() as {
        kind: string
        metadata: { id: string }
        data: { properties: Record<string, unknown> }
      }
      expect(d.kind).toBe(MODEL)
      return record(MODEL, d.metadata.id, d.data.properties)
    })
    for (const id of ["openai", "anthropic", "gemini"]) {
      const offers = rowOffers(row(id), records, undefined)
      expect(offers.length).toBeGreaterThan(0)
      for (const offer of offers) {
        expect(offer.source).toBe("record")
        expect(offer.window).toBeGreaterThan(0)
        expect(offer.record?.label).toBeTruthy()
      }
    }
    const opus = rowOffers(row("anthropic"), records, undefined).find(
      (o) => o.model === "claude-opus-5-5"
    )
    expect(priceWords(opus!.price!)).toBe(
      "$4 input · $20 output · $0.20 cache reads"
    )
    expect(windowWords(opus!.window!)).toBe("200k tokens")
  })
})

describe("rule B: what a new thread runs on", () => {
  const keyed = row("anthropic", { apiKey: "••••" })
  const keyless = row("openai")
  const gemini = row("gemini")
  const gpt5 = model("o:gpt-5", "openai", "gpt-5")
  const flash = model("g:flash", "gemini", "gemini-2.5-flash")

  it("takes the first entry whose record and row are live and whose row has its own key", () => {
    const choice = chooseRunsOn(
      ["anthropic:claude-opus-5-5", "o:gpt-5"],
      context([keyed, keyless], [OPUS, gpt5])
    )
    expect(choice.chosen?.id).toBe("anthropic:claude-opus-5-5")
    expect(choice.canStart).toBe(true)
    expect(choice.entries[1]).toMatchObject({
      available: false,
      refusal: { code: "no-key", row: "openai" },
    })
  })

  it("skips a deleted record", () => {
    const deleted = model(
      "gone",
      "anthropic",
      "claude-opus-5",
      {},
      { deletedAt: "2026-11-02T00:00:00Z" }
    )
    const choice = chooseRunsOn(
      ["missing", "gone", "anthropic:claude-opus-5-5"],
      context([keyed], [deleted, OPUS])
    )
    expect(choice.entries.map((e) => e.refusal?.code)).toEqual([
      "no-record",
      "no-record",
      undefined,
    ])
    expect(choice.chosen?.id).toBe("anthropic:claude-opus-5-5")
  })

  it("skips a record whose row was deleted", () => {
    const deletedRow = row("gemini", { apiKey: "••••" })
    const choice = chooseRunsOn(
      ["g:flash", "anthropic:claude-opus-5-5"],
      context(
        [{ ...deletedRow, deletedAt: "2026-11-02T00:00:00Z" }, keyed],
        [flash, OPUS]
      )
    )
    expect(choice.entries[0].refusal).toEqual({ code: "no-row", row: "gemini" })
    expect(choice.chosen?.id).toBe("anthropic:claude-opus-5-5")
    // A row that was never there reads the same.
    expect(
      chooseRunsOn(["g:flash"], context([keyed], [flash])).entries[0].refusal
    ).toEqual({ code: "no-row", row: "gemini" })
  })

  it("skips a keyless row the operator does not serve", () => {
    const items = [
      item({
        id: "llm/openai",
        source: "none",
        readiness: "not-set-up",
        operatorOffers: false,
      }),
    ]
    const choice = chooseRunsOn(
      ["o:gpt-5", "anthropic:claude-opus-5-5"],
      context([keyless, keyed], [gpt5, OPUS], items)
    )
    expect(choice.entries[0].refusal).toEqual({ code: "no-key", row: "openai" })
    expect(choice.chosen?.id).toBe("anthropic:claude-opus-5-5")
  })

  it("takes a model on the operator's list, and skips one off it", () => {
    const items = [item({ id: "llm/openai", models: ["gpt-5-mini"] })]
    const mini = model("o:mini", "openai", "gpt-5-mini")
    const choice = chooseRunsOn(
      ["o:gpt-5", "o:mini"],
      context([keyless], [gpt5, mini], items)
    )
    expect(choice.entries[0]).toMatchObject({
      available: false,
      refusal: { code: "not-served", row: "openai", served: ["gpt-5-mini"] },
    })
    expect(choice.chosen?.id).toBe("o:mini")
    expect(choice.canStart).toBe(true)
  })

  it("never moves for readiness: a refused key, an alert, Needs attention", () => {
    const items = [
      item({
        id: "llm/anthropic",
        source: "own",
        operatorOffers: false,
        readiness: "failing",
        message: "Anthropic refused the key (401)",
      }),
      item({
        id: "llm/openai",
        models: ["gpt-5"],
        readiness: "failing",
        message: "an open alert",
      }),
    ]
    const ctx = context([keyed, keyless], [OPUS, gpt5], items)
    expect(
      chooseRunsOn(["anthropic:claude-opus-5-5", "o:gpt-5"], ctx).chosen?.id
    ).toBe("anthropic:claude-opus-5-5")
    expect(
      chooseRunsOn(["o:gpt-5", "anthropic:claude-opus-5-5"], ctx).chosen?.id
    ).toBe("o:gpt-5")
    // A key of the row's own that the server cannot resolve (no endpoint)
    // still runs: what the vendor or the wire answers stops a run, never
    // the choice.
    const notSetUp = [
      item({
        id: "llm/anthropic",
        source: "own",
        operatorOffers: false,
        readiness: "not-set-up",
      }),
    ]
    expect(
      chooseRunsOn(
        ["anthropic:claude-opus-5-5"],
        context([keyed], [OPUS], notSetUp)
      ).canStart
    ).toBe(true)
  })

  it("lets a row's own key run a model the operator's list leaves out", () => {
    const items = [
      item({ id: "llm/anthropic", source: "own", operatorOffers: true }),
    ]
    expect(
      chooseRunsOn(
        ["anthropic:claude-opus-5-5"],
        context([keyed], [OPUS], items)
      ).canStart
    ).toBe(true)
  })

  it("cannot start where no entry can, naming the first row a key would open", () => {
    const choice = chooseRunsOn(
      ["missing", "g:flash", "o:gpt-5"],
      context([keyless, gemini], [flash, gpt5])
    )
    expect(choice.canStart).toBe(false)
    expect(choice.chosen).toBeUndefined()
    expect(choice.keyRow).toBe("gemini")
    // Nothing a key would open: no row to name.
    const none = chooseRunsOn(["missing"], context([keyless], [gpt5]))
    expect(none).toMatchObject({ canStart: false })
    expect(none.keyRow).toBeUndefined()
  })

  it("cannot tell while the operator's read or the records have not answered", () => {
    const pending = chooseRunsOn(
      ["o:gpt-5", "anthropic:claude-opus-5-5"],
      context([keyless, keyed], [gpt5, OPUS], "pending")
    )
    // A keyless row may still be the operator's: it stays first, and a later
    // entry with a key says a chat can start.
    expect(pending.entries[0].available).toBeUndefined()
    expect(pending.chosen?.id).toBe("o:gpt-5")
    expect(pending.canStart).toBe(true)
    expect(
      chooseRunsOn(["o:gpt-5"], context([keyless], [gpt5], "pending")).canStart
    ).toBeUndefined()
    expect(
      chooseRunsOn(["o:gpt-5"], context([keyless], undefined)).canStart
    ).toBeUndefined()
    expect(
      chooseRunsOn(["o:gpt-5"], context(undefined, [gpt5])).canStart
    ).toBeUndefined()
  })
})

describe("rule B: a continued thread", () => {
  const keyed = row("anthropic", { apiKey: "••••" })
  const keyless = row("openai")
  const gpt5 = model("o:gpt-5", "openai", "gpt-5")
  const ids = ["anthropic:claude-opus-5-5", "o:gpt-5"]

  it("stays on the row and model it is stamped with, never a later entry", () => {
    const ctx = context([keyed, keyless], [OPUS, gpt5])
    const on = continuedRunsOn(ids, { row: "openai", model: "gpt-5" }, ctx)
    expect(on.entry?.id).toBe("o:gpt-5")
    // The stamped row has no key: the thread cannot go on, whatever the
    // first entry offers.
    expect(on.available).toBe(false)
    expect(
      continuedRunsOn(ids, { row: "anthropic", model: "claude-opus-5-5" }, ctx)
        .available
    ).toBe(true)
  })

  it("refuses a thread whose row and model the agent no longer names", () => {
    const ctx = context([keyed], [OPUS])
    expect(
      continuedRunsOn(ids, { row: "anthropic", model: "claude-opus-5" }, ctx)
    ).toEqual({ available: false, dropped: true })
  })
})

describe("what an agent runs on", () => {
  const keyed = row("anthropic", { apiKey: "••••" })
  const keyless = row("openai")
  const gpt5 = model("o:gpt-5", "openai", "gpt-5", { label: "GPT-5" })

  it("reads an agent on provider and model as before", () => {
    const pair = record(AGENT, "x", {
      provider: { ref: `${PROVIDER}/openai` },
      model: "gpt-5",
    })
    expect(agentRuns(pair, context([keyless], []))).toEqual({
      row: "openai",
      model: "gpt-5",
      canRun: false,
      keyRow: "openai",
    })
    expect(agentRuns(pair, context([keyless], [], "pending"))).toEqual({
      row: "openai",
      model: "gpt-5",
    })
  })

  it("names the entry a new thread takes, and the row a key would open where none can", () => {
    const agent = agentOn(["o:gpt-5", "anthropic:claude-opus-5-5"])
    expect(
      agentRuns(agent, context([keyless, keyed], [gpt5, OPUS]))
    ).toMatchObject({
      row: "anthropic",
      model: "claude-opus-5-5",
      label: "Claude Opus 5.5",
      canRun: true,
    })
    const stuck = agentRuns(
      agentOn(["missing", "o:gpt-5"]),
      context([keyless], [gpt5])
    )
    expect(stuck).toMatchObject({
      row: "openai",
      model: "gpt-5",
      label: "GPT-5",
      canRun: false,
      keyRow: "openai",
    })
  })

  it("reads a conversation's next turn: a new chat's choice, or the thread's own row", () => {
    const agent = agentOn(["anthropic:claude-opus-5-5", "o:gpt-5"])
    const ctx = context([keyed, keyless], [OPUS, gpt5])
    expect(threadRuns(agent, undefined, "", ctx)).toMatchObject({
      row: "anthropic",
      canRun: true,
    })
    const thread = record(THREAD, "t1", { provider: "openai", model: "gpt-5" })
    expect(threadRuns(agent, thread, "t1", ctx)).toEqual({
      row: "openai",
      canRun: false,
      keyRow: "openai",
    })
    // Not read yet: nothing to say.
    expect(threadRuns(agent, undefined, "t1", ctx)).toEqual({})
  })
})

describe("the words", () => {
  const rows = [
    row("openai", { label: "openai" }),
    row("gw", { label: "My gateway" }),
  ]
  const crane = operatorWords({ name: "Crane" })

  it("says why an entry cannot take a new thread", () => {
    expect(refusalWords({ code: "no-record" }, rows, crane)).toBe(
      "This model record was deleted, or never existed."
    )
    expect(refusalWords({ code: "no-row", row: "gemini" }, rows, crane)).toBe(
      "Its provider, Gemini, doesn’t exist."
    )
    expect(refusalWords({ code: "no-key", row: "gw" }, rows, crane)).toBe(
      "My gateway has no API key."
    )
    expect(
      refusalWords(
        { code: "not-served", row: "openai", served: ["gpt-5", "gpt-5-mini"] },
        rows,
        crane
      )
    ).toBe("Crane serves only gpt-5 and gpt-5-mini on OpenAI.")
    expect(
      refusalWords(
        { code: "not-served", row: "openai", served: [] },
        rows,
        operatorWords(undefined)
      )
    ).toBe("This server serves no model on OpenAI.")
  })

  it("says a price as written, its stated cache rates beside it", () => {
    const facts = modelFacts(
      model("m", "r", "x", {
        pricing: {
          inputPer1M: "0.10",
          outputPer1M: "0.50",
          cacheWritePer1M: "0.125",
        },
      })
    )
    expect(priceWords(facts.price!)).toBe(
      "$0.10 input · $0.50 output · $0.125 cache writes"
    )
    expect(windowWords(1048576)).toBe("1M tokens")
  })
})
