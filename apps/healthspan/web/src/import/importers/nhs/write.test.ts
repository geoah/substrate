import { Effect } from "effect"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { Server, VirtualClock, browser } from "../../harness"
import { recordId } from "../../ids"
import { KINDS } from "../shared"
import { fakeContext, inputFromRecord } from "../testing"
import type { Plan } from "../types"
import { exampleRun, HISTORIC_TEST, year } from "./fixtures/run"
import { nhs } from "."

// The invented run's plan, written end to end by the runner against the
// fake substrate: everything lands, every reference resolves, and a second
// import of the same run changes nothing.

let server: Server

beforeEach(() => {
  server = new Server(new VirtualClock())
  vi.stubGlobal("fetch", server.fetch)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function examplePlan(): Promise<Plan> {
  const input = inputFromRecord(
    "example",
    exampleRun({ years: [year(2019, [HISTORIC_TEST])] })
  )
  return Effect.runPromise(
    nhs.read(input, fakeContext({ ownerContact: "c-owner" }))
  )
}

async function importPlan(id: string, plan: Plan) {
  const one = browser(server, "a")
  expect(await one.stage({ id, plan })).toEqual({ state: "planned" })
  return one.resume(id)
}

describe("the NHS App's plan, written", () => {
  it("lands every record, and a second import of the run changes nothing", async () => {
    const plan = await examplePlan()
    const first = await importPlan("0194a1b2-0000-7000-8000-0000000000a1", plan)
    expect(first).toMatchObject({
      state: "imported",
      counts: { created: plan.records.length, left: 0, stale: 0 },
    })
    const encounter = plan.records.find(
      (r) => r.kind === KINDS.encounter && r.properties.reports
    )!
    expect(
      server.live(encounter.kind, recordId(encounter))?.properties.reports
    ).toEqual([
      `${KINDS.labreport}/${recordId({ kind: KINDS.labreport, namespace: "nhs", key: "t-fbc" })}`,
    ])
    const doc = plan.records.find(
      (r) => r.kind === KINDS.document && r.key === "doc-1"
    )!
    expect(server.live(doc.kind, recordId(doc))?.properties.file).toMatch(
      /^blob-sha256-/
    )
    const again = await importPlan(
      "0194a1b2-0000-7000-8000-0000000000b2",
      await examplePlan()
    )
    expect(again).toMatchObject({
      state: "imported",
      counts: { created: 0, updated: 0, stale: 0, left: 0 },
    })
  })
})
