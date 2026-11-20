import { Effect, Result } from "effect"
import { describe, expect, it } from "vitest"
import type { MetricRecord } from "../../../catalogue/types"
import { KIND } from "../../../kinds"
import { EMPTY_GRAPH } from "../../../labs/units"
import { LAB_SOURCE } from "../../records"
import { manual } from "../manual"
import type { Context, Input } from "../types"
import { csv } from "."

const METRICS: MetricRecord[] = [
  {
    id: "labs/ferritin",
    name: "Ferritin",
    unit: "ug/L",
    codes: [{ system: "http://loinc.org", code: "2276-4" }],
    category: "labs",
    precision: 0,
  },
]

function ctx(answers: Record<string, string> = {}): Context {
  return {
    today: new Date("2026-11-09T12:00:00Z"),
    metrics: METRICS,
    units: EMPTY_GRAPH,
    sources: [],
    answers,
  }
}

function fileInput(name: string, text: string): Input {
  const bytes = new TextEncoder().encode(text)
  return {
    name,
    files: [
      {
        path: name,
        size: bytes.length,
        type: "text/csv",
        bytes: () => Promise.resolve(bytes),
        text: () => Promise.resolve(text),
      },
    ],
  }
}

const run = <A, E>(e: Effect.Effect<A, E>) =>
  Effect.runPromise(Effect.result(e))

describe("the CSV importer", () => {
  it("takes a table of lab results as its own, and not a folder", async () => {
    const input = fileInput(
      "r.csv",
      "Date,Test,Value,Unit\n2026-03-12,Ferritin,80,ug/L\n"
    )
    expect(await Effect.runPromise(csv.detect(input))).toMatchObject({
      importer: "csv",
      confidence: 0.6,
      why: "A table of 1 row of lab results.",
    })
    expect(
      await Effect.runPromise(
        csv.detect({ name: "x", files: [...input.files, ...input.files] })
      )
    ).toBeUndefined()
  })

  it("asks for the lab where the file names none, then plans in the import's own namespace", async () => {
    const input = fileInput(
      "r.csv",
      "Date,Test,Value,Unit\n2026-03-12,Ferritin,80,ug/L\n"
    )
    const asked = await run(csv.read(input, ctx()))
    expect(Result.isFailure(asked) && asked.failure).toMatchObject({
      _tag: "NeedsAnswers",
      questions: [{ key: "lab" }],
    })
    const read = await run(csv.read(input, ctx({ lab: "Example Lab" })))
    expect(Result.isSuccess(read)).toBe(true)
    if (!Result.isSuccess(read)) return
    expect(read.success).toMatchObject({
      importer: "csv",
      name: "r.csv",
      source: LAB_SOURCE,
      documents: [],
    })
    expect(
      read.success.records.map((r) => [r.kind, r.namespace, r.key])
    ).toEqual([
      [KIND.labreport, "import", "1"],
      [KIND.labresult, "import", "1"],
    ])
  })
})

describe("the hand entry", () => {
  it("plans one report and one result from its answers", async () => {
    const read = await run(
      manual.read(
        { name: "Entered by hand", files: [] },
        ctx({
          metric: "labs/ferritin",
          day: "2026-03-12",
          value: "80",
          unit: "ug/L",
          lab: "Example Lab",
        })
      )
    )
    expect(Result.isSuccess(read)).toBe(true)
    if (!Result.isSuccess(read)) return
    expect(read.success.importer).toBe("manual")
    expect(read.success.records).toHaveLength(2)
    expect(read.success.records[1].properties).toMatchObject({
      metric: `${KIND.metric}/labs/ferritin`,
      value: { value: "80", unit: "ug/L" },
    })
    const missing = await run(
      manual.read({ name: "", files: [] }, ctx({ metric: "labs/ferritin" }))
    )
    expect(Result.isFailure(missing) && missing.failure._tag).toBe(
      "NeedsAnswers"
    )
  })
})
