import { Effect } from "effect"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { KIND, type ImportCounts } from "../../../kinds"
import { Server, VirtualClock, browser } from "../../harness"
import { recordId } from "../../ids"
import { fakeContext, inputFromRecord } from "../testing"
import type { Input } from "../types"
import { scanC, transcribedPage } from "./fixtures"
import { dxa } from "."

// The invented scans' plans written by the one runner into the fake
// substrate: created once, unchanged on a second import of the same files;
// a later scan's trend table raises the scans only tables list, never a
// scan its own printout wrote, whichever is imported first; the earlier
// files replayed after it change nothing.

const IDS = [
  "0194a1b2-0000-7000-8000-0000000000a1",
  "0194a1b2-0000-7000-8000-0000000000b2",
  "0194a1b2-0000-7000-8000-0000000000c3",
  "0194a1b2-0000-7000-8000-0000000000d4",
]

let server: Server

beforeEach(() => {
  server = new Server(new VirtualClock())
  vi.stubGlobal("fetch", server.fetch)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

const ctx = fakeContext({
  answers: { centre: "Invented Imaging Centre", zone: "Europe/Athens" },
})

async function importFiles(id: string, input: Input) {
  const plan = await Effect.runPromise(dxa.read(input, ctx))
  const one = browser(server, "a")
  expect(await one.stage({ id, plan })).toEqual({ state: "planned" })
  return { plan, outcome: await one.resume(id) }
}

describe("the invented scans, written", () => {
  it("are created whole, then unchanged on a second import", async () => {
    const input = inputFromRecord("scans", scanC())
    const first = await importFiles(IDS[0], input)
    const records = first.plan.records.length
    expect(first.outcome).toMatchObject({
      state: "imported",
      counts: { created: records, updated: 0, unchanged: 0, stale: 0 },
    })
    const id = recordId({
      kind: KIND.labreport,
      namespace: "dxa/invented-imaging-centre",
      key: "2024-07-15",
    })
    expect(server.live(KIND.labreport, id)?.properties).toMatchObject({
      accession: "c7k2m9q1zz.dfb",
      review: "pending",
    })
    for (const d of first.plan.documents)
      expect([...(server.blobs.get(d.digest)?.bytes ?? [])]).toEqual([
        ...d.bytes,
      ])
    const second = await importFiles(IDS[1], input)
    expect(second.outcome).toMatchObject({
      state: "imported",
      counts: { created: 0, updated: 0, unchanged: records, stale: 0 },
    })
  })

  it("raise the scans only tables list for a later scan, and an older set replayed changes nothing", async () => {
    const older = inputFromRecord("scans", scanC())
    const first = await importFiles(IDS[0], older)
    const later = inputFromRecord("all", { ...scanC(), ...transcribedPage() })
    const second = await importFiles(IDS[1], later)
    const key = (r: { kind: string; key: string }) => `${r.kind} ${r.key}`
    const had = new Map(first.plan.records.map((r) => [key(r), r]))
    const both = second.plan.records.filter(
      (r) => r.mode !== "create" && had.has(key(r))
    )
    // The scan only trend tables list, restated by the later scan's table.
    const restated = both.filter(
      (r) =>
        JSON.stringify(r.properties) !==
        JSON.stringify(had.get(key(r))!.properties)
    )
    expect(restated.length).toBeGreaterThan(0)
    expect(restated.every((r) => r.key.startsWith("2023-10-03"))).toBe(true)
    // Every record of the first set the later one names: a scan only
    // tables list raised, its content where restated; scan C's, read from
    // its printout both times, unchanged; the rest created.
    const own = both.filter((r) => r.key.startsWith("2024-07-15"))
    expect(own.length).toBeGreaterThan(0)
    expect(second.outcome).toMatchObject({
      state: "imported",
      counts: {
        created: second.plan.records.length - both.length - 1,
        updated: both.length - own.length,
        unchanged: 1 + own.length,
        stale: 0,
      },
    })
    // The older set again: what the later scan restated is stale, the rest
    // unchanged.
    const replay = await importFiles(IDS[2], older)
    expect(replay.outcome).toMatchObject({
      state: "imported",
      counts: {
        created: 0,
        updated: 0,
        unchanged: first.plan.records.length - restated.length,
        stale: restated.length,
      },
    })
  })

  it("keep a scan's own printout over a later scan's trend table imported alone, either way round", async () => {
    const ns = "dxa/invented-imaging-centre"
    const reportId = recordId({
      kind: KIND.labreport,
      namespace: ns,
      key: "2024-07-15",
    })
    const fatId = recordId({
      kind: KIND.labresult,
      namespace: ns,
      key: "2024-07-15/fat_mass",
    })
    const original = inputFromRecord("scans", scanC())
    await importFiles(IDS[0], original)
    const report = server.live(KIND.labreport, reportId)!.properties
    const fat = server.live(KIND.labresult, fatId)!.properties
    // The later scan's page alone restates scan C, with other figures, a
    // date for its accession and noon for its time: stale against its
    // printout.
    const page = await importFiles(
      IDS[1],
      inputFromRecord("page", transcribedPage())
    )
    expect(page.outcome).toMatchObject({ state: "imported" })
    const counts = (page.outcome as { counts: ImportCounts }).counts
    expect(counts.stale).toBeGreaterThan(0)
    expect(server.live(KIND.labreport, reportId)!.properties).toEqual(report)
    expect(server.live(KIND.labresult, fatId)!.properties).toEqual(fat)
    // The printout again: nothing of its scan changes.
    const again = await importFiles(IDS[2], original)
    expect(again.outcome).toMatchObject({
      state: "imported",
      counts: { updated: 0 },
    })
    expect(server.live(KIND.labreport, reportId)!.properties).toEqual(report)
    expect(server.live(KIND.labresult, fatId)!.properties).toEqual(fat)
  })
})
