import { Effect } from "effect"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { KIND } from "../../../kinds"
import { Server, VirtualClock, browser } from "../../harness"
import { recordId } from "../../ids"
import { fakeContext, inputFromRecord } from "../testing"
import { exportFiles, WINTER_SAMPLE } from "./fixtures"
import { randox } from "."

// The invented export's plan written by the one runner into the fake
// substrate: everything created once, nothing written again on a second
// import of the same export.

const A = "0194a1b2-0000-7000-8000-0000000000a1"
const B = "0194a1b2-0000-7000-8000-0000000000b2"

let server: Server

beforeEach(() => {
  server = new Server(new VirtualClock())
  vi.stubGlobal("fetch", server.fetch)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function importExport(id: string) {
  const plan = await Effect.runPromise(
    randox.read(inputFromRecord("randox", exportFiles()), fakeContext())
  )
  const one = browser(server, "a")
  expect(await one.stage({ id, plan })).toEqual({ state: "planned" })
  return { plan, outcome: await one.resume(id) }
}

describe("the invented export, written", () => {
  it("is created whole, then unchanged on a second import", async () => {
    const first = await importExport(A)
    const records = first.plan.records.length
    expect(first.outcome).toMatchObject({
      state: "imported",
      counts: { created: records, updated: 0, unchanged: 0, stale: 0 },
    })
    const report = server.live(
      KIND.labreport,
      recordId({
        kind: KIND.labreport,
        namespace: "randox",
        key: WINTER_SAMPLE,
      })
    )
    expect(report?.properties).toMatchObject({
      accession: WINTER_SAMPLE,
      review: "pending",
    })
    for (const d of first.plan.documents)
      expect([...(server.blobs.get(d.digest)?.bytes ?? [])]).toEqual([
        ...d.bytes,
      ])
    const second = await importExport(B)
    expect(second.outcome).toMatchObject({
      state: "imported",
      counts: { created: 0, updated: 0, unchanged: records, stale: 0 },
    })
  })
})
