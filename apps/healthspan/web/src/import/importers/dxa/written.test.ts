import { Effect } from "effect"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { KIND } from "../../../kinds"
import { Server, VirtualClock, browser } from "../../harness"
import { recordId } from "../../ids"
import { fakeContext, inputFromRecord } from "../testing"
import type { Input } from "../types"
import { scanC, transcribedPage } from "./fixtures"
import { dxa } from "."

// The invented scans' plans written by the one runner into the fake
// substrate: created once, unchanged on a second import of the same files;
// a later scan raises every record's revision once; the earlier files
// replayed after it change nothing.

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

  it("raise every record's revision once for a later scan, and an older set replayed changes nothing", async () => {
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
    // Every record of the first set the later one names: its revision
    // raised, its content where restated; the rest created.
    expect(second.outcome).toMatchObject({
      state: "imported",
      counts: {
        created: second.plan.records.length - both.length - 1,
        updated: both.length,
        unchanged: 1,
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
})
