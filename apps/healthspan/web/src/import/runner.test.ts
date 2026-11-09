import { SubstrateClient } from "@substrate/client"
import { FakeLocks } from "@substrate/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { Import, KIND, type ImportRecord } from "../kinds"
import { Server, VirtualClock, browser, urlOf } from "./harness"
import { labSourceId, recordId } from "./ids"
import type { Plan, PlannedRecord } from "./importers/types"
import { labPlan, resultRecord, type ImportRow } from "./plan"
import { blobDigestOf, planBytes, storedPlan } from "./planfile"
import { refPath, reportKey, resultKey } from "./records"
import { RunRegistry } from "./registry"
import { LEASE_MS } from "./runner"
import { clientStore } from "./store"

const ID = "0194a1b2-0000-7000-8000-00000000abcd"
const LAB = "Example Lab"

const ROWS: ImportRow[] = [
  ["1", "1", "labs/ldl_cholesterol", "3.4", "mmol/L"],
  ["2", "1", "labs/hdl_cholesterol", "1.5", "mmol/L"],
  ["3", "1", "labs/triglycerides", "1.1", "mmol/L"],
  ["4", "2", "labs/ferritin", "80", "ug/L"],
  ["5", "2", "labs/crp", "0.6", "mg/L"],
].map(([key, report, metric, value, unit]) => ({
  key,
  report,
  metric,
  value,
  unit,
  takenAt:
    report === "1" ? "2026-03-12T12:00:00+00:00" : "2026-04-02T12:00:00+00:00",
}))

const REPORTS = [
  { key: "1", takenAt: "2026-03-12T12:00:00+00:00", accession: "A1" },
  { key: "2", takenAt: "2026-04-02T12:00:00+00:00" },
]

const PLAN: Plan = labPlan({
  name: "results.csv",
  importer: "csv",
  lab: LAB,
  reports: REPORTS,
  rows: ROWS,
})

const reportId = (key: string) => recordId(reportKey(key), ID)
const resultId = (key: string) => recordId(resultKey(key), ID)

let server: Server
let clock: VirtualClock

/** The import as `planned`: its plan stored and named. */
function seedPlanned(extra: Record<string, unknown> = {}) {
  const bytes = planBytes(storedPlan(PLAN))
  const digest = blobDigestOf(bytes)
  server.blobs.set(digest, { bytes, at: clock.now() })
  return server.fake.seed(KIND.import, ID, {
    ...Import.write({
      name: "results.csv",
      importer: "csv",
      status: "planned",
      lab: LAB,
      plan: digest,
      documents: [],
    }),
    ...extra,
  })
}

function imp(): ImportRecord {
  return Import.read(server.importRecord(ID)!)
}

const live = (kind: string) => server.all(kind)

const pointing = () =>
  [...live(KIND.labresult), ...live(KIND.labreport)].filter(
    (r) => refPath(r.properties.origin) === `${KIND.import}/${ID}`
  )

/** A result as the runner writes it, every reference resolved. */
function written(row: ImportRow): Record<string, unknown> {
  const rec: PlannedRecord = resultRecord(row)
  return {
    ...rec.properties,
    source: `${KIND.source}/${imp().source}`,
    report: `${KIND.labreport}/${reportId(row.report)}`,
    origin: `${KIND.import}/${ID}`,
  }
}

const a = (fetch?: typeof globalThis.fetch) =>
  browser(server, "a", { labName: LAB, fetch })
const b = (fetch?: typeof globalThis.fetch) =>
  browser(server, "b", { labName: LAB, fetch })

beforeEach(() => {
  clock = new VirtualClock()
  server = new Server(clock)
  vi.stubGlobal("fetch", server.fetch)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("the runner on a file's plan", () => {
  it("runs a planned import: the lab's source, each report, each result, then imported", async () => {
    seedPlanned()
    const out = await a().resume(ID)
    expect(out).toEqual({
      state: "imported",
      counts: { created: 7, updated: 0, unchanged: 0, stale: 0, left: 0 },
    })
    const done = imp()
    expect(done.status).toBe("imported")
    expect(done.runner).toBeUndefined()
    expect(done.counts).toEqual({
      created: 7,
      updated: 0,
      unchanged: 0,
      stale: 0,
      left: 0,
    })
    expect(done.importedAt).toBeDefined()
    const source = labSourceId(LAB)
    expect(done.source).toBe(source)
    expect(server.live(KIND.source, source)?.properties).toEqual({
      name: LAB,
      class: "lab",
    })
    expect(
      live(KIND.labreport)
        .map((r) => r.id)
        .sort()
    ).toEqual([reportId("1"), reportId("2")].sort())
    expect(
      live(KIND.labresult)
        .map((r) => r.id)
        .sort()
    ).toEqual(ROWS.map((r) => resultId(r.key)).sort())
    expect(pointing()).toHaveLength(7)
    // A result names its report and the lab's source.
    expect(server.live(KIND.labresult, resultId("4"))?.properties).toEqual(
      written(ROWS[3])
    )
    expect(server.live(KIND.labreport, reportId("1"))?.properties).toEqual({
      at: REPORTS[0].takenAt,
      lab: LAB,
      accession: "A1",
      source: `${KIND.source}/${source}`,
      origin: `${KIND.import}/${ID}`,
    })
    // Every planned write is a create at version 0.
    const puts = server.fake.requests.filter(
      (r) => r.method === "PUT" && r.path.includes("/labs/")
    )
    expect(
      puts.every((r) => (r.body as { ifVersion?: number }).ifVersion === 0)
    ).toBe(true)
  })

  it("takes the lease for 60 seconds, held to the version read", async () => {
    seedPlanned()
    await a().resume(ID)
    const take = server.fake.requests.find(
      (r) => r.method === "PATCH" && r.path.endsWith(ID)
    )!
    const body = take.body as {
      ifVersion: number
      properties: { runner: { device: string; until: string } }
    }
    expect(body.ifVersion).toBe(1)
    expect(body.properties.runner.device).toBe("a")
    expect(Date.parse(body.properties.runner.until)).toBe(
      Date.parse("2026-11-01T10:00:00Z") + LEASE_MS
    )
  })

  it("renews the lease every 20 seconds while it writes, with the counts so far", async () => {
    seedPlanned()
    const slow: typeof fetch = async (input, init) => {
      if (init?.method === "PUT") clock.advance(8_000)
      return server.fetch(input, init)
    }
    await a(slow).resume(ID)
    const leases = server.fake.requests
      .filter((r) => r.method === "PATCH" && r.path.endsWith(ID))
      .map(
        (r) =>
          (
            r.body as {
              properties: {
                runner?: { until: string } | null
                counts?: { created: number }
              }
            }
          ).properties
      )
    // The take, at least two renewals, and the end.
    expect(leases.length).toBeGreaterThanOrEqual(4)
    const untils = leases
      .map((p) => p.runner?.until)
      .filter((u): u is string => !!u)
      .map(Date.parse)
    expect(untils).toEqual([...untils].sort((x, y) => x - y))
    expect(leases.some((p) => (p.counts?.created ?? 0) > 0)).toBe(true)
    expect(imp().status).toBe("imported")
  })

  it("stops at once when a renewal is refused: another browser took over", async () => {
    seedPlanned()
    let puts = 0
    const meddled: typeof fetch = async (input, init) => {
      if (init?.method === "PUT" && urlOf(input).includes("/labs/")) {
        clock.advance(8_000)
        if (++puts === 3)
          server.fake.change(KIND.import, ID, {
            runner: {
              device: "b",
              until: new Date(clock.t + LEASE_MS).toISOString(),
            },
          })
      }
      return server.fetch(input, init)
    }
    const out = await a(meddled).resume(ID)
    expect(out.state).toBe("lost")
    expect(imp().runner?.device).toBe("b")
    // The renewal refused is the last request the browser sent.
    const last = server.fake.requests.at(-1)!
    expect(last.method).toBe("PATCH")
    expect(last.path.endsWith(ID)).toBe(true)
    expect(live(KIND.labresult).length).toBeLessThan(ROWS.length)
  })

  it("refuses to resume while another browser's lease is live, and Undo waits it out", async () => {
    seedPlanned({
      status: "importing",
      source: `${KIND.source}/lab/x`,
      runner: { device: "b", until: new Date(clock.t + 40_000).toISOString() },
    })
    const one = a()
    expect(await one.resume(ID)).toEqual({
      state: "held",
      until: clock.t + 40_000,
    })
    expect(server.fake.writes()).toEqual([])
    const out = await one.undo(ID)
    expect(out).toEqual({ state: "undone", remaining: 0 })
    expect(one.progress).toContainEqual({
      phase: "waiting",
      until: Date.parse("2026-11-01T10:00:40Z"),
    })
    // It waited past the other lease's end by 30 seconds before deleting.
    expect(one.progress).toContainEqual({
      phase: "settling",
      until: Date.parse("2026-11-01T10:01:10Z"),
    })
    expect(imp().status).toBe("undone")
  })

  it("answers a lost answer: a write that landed meets 409, compares equal, and counts unchanged", async () => {
    seedPlanned()
    let puts = 0
    const lossy: typeof fetch = async (input, init) => {
      const res = await server.fetch(input, init)
      if (
        init?.method === "PUT" &&
        urlOf(input).includes("/labresult/") &&
        ++puts === 2
      )
        throw new TypeError("Failed to fetch")
      return res
    }
    expect(await a(lossy).resume(ID)).toEqual({
      state: "imported",
      counts: { created: 6, updated: 0, unchanged: 1, stale: 0, left: 0 },
    })
    expect(live(KIND.labresult)).toHaveLength(5)
  })

  it("stops on a conflict that does not compare equal, and resumes from stopped through importing", async () => {
    seedPlanned()
    const clash = resultId("3")
    server.fake.seed(KIND.labresult, clash, {
      metric: `${KIND.metric}/labs/triglycerides`,
      value: { value: "9.9", unit: "mmol/L" },
      at: "2026-03-12T12:00:00+00:00",
      origin: `${KIND.import}/${ID}`,
    })
    const out = await a().resume(ID)
    expect(out.state).toBe("stopped")
    expect(imp().status).toBe("stopped")
    expect(imp().problem).toMatch(/Result 3 is already in the repository/)
    expect(imp().runner).toBeUndefined()
    // The report and the two results before it.
    expect(imp().counts?.created).toBe(3)

    // The owner mends the row; a resume moves stopped to importing in the
    // patch that takes the lease, then writes from the start.
    server.fake.change(KIND.labresult, clash, written(ROWS[2]))
    const marker = server.fake.requests.length
    const again = await b().resume(ID)
    expect(again).toEqual({
      state: "imported",
      counts: { created: 3, updated: 0, unchanged: 4, stale: 0, left: 0 },
    })
    const take = server.fake.requests
      .slice(marker)
      .find((r) => r.method === "PATCH" && r.path.endsWith(ID))!
    expect(
      (take.body as { properties: Record<string, unknown> }).properties
    ).toMatchObject({
      status: "importing",
      problem: null,
      runner: { device: "b" },
    })
    expect(live(KIND.labresult)).toHaveLength(5)
  })

  it("resumes an importing import whose lease lapsed: what landed compares equal", async () => {
    seedPlanned()
    let puts = 0
    const dies: typeof fetch = async (input, init) => {
      if (
        init?.method === "PUT" &&
        urlOf(input).includes("/labresult/") &&
        ++puts === 3
      )
        clock.advance(LEASE_MS)
      return server.fetch(input, init)
    }
    expect((await a(dies).resume(ID)).state).toBe("lost")
    expect(imp().status).toBe("importing")
    clock.advance(LEASE_MS)
    expect(await b().resume(ID)).toMatchObject({
      state: "imported",
      counts: { created: 4, unchanged: 3 },
    })
    expect(live(KIND.labresult)).toHaveLength(5)
  })

  it("runs the undo again from undoing", async () => {
    seedPlanned()
    await a().resume(ID)
    server.fake.change(KIND.import, ID, {
      status: "undoing",
      runner: { device: "a", until: new Date(clock.t - 1000).toISOString() },
    })
    expect(await b().resume(ID)).toEqual({ state: "undone", remaining: 0 })
    expect(pointing()).toEqual([])
  })

  it("undoes a partial import, and a second browser resuming during and after it does nothing", async () => {
    seedPlanned()
    server.fake.seed(KIND.labresult, "someone-elses", {
      metric: `${KIND.metric}/labs/ferritin`,
      at: "2026-04-02T12:00:00+00:00",
    })
    let puts = 0
    const stopsEarly: typeof fetch = async (input, init) => {
      if (
        init?.method === "PUT" &&
        urlOf(input).includes("/labresult/") &&
        ++puts === 3
      )
        return new Response(
          JSON.stringify({ error: { code: "validation", message: "refused" } }),
          { status: 422, headers: { "Content-Type": "application/json" } }
        )
      return server.fetch(input, init)
    }
    expect((await a(stopsEarly).resume(ID)).state).toBe("stopped")
    expect(live(KIND.labresult)).toHaveLength(3)

    // Undo holds the lease while it waits for writes in flight to land.
    server.fake.change(KIND.import, ID, {
      status: "undoing",
      runner: {
        device: "a",
        until: new Date(clock.t + LEASE_MS).toISOString(),
      },
    })
    expect((await b().resume(ID)).state).toBe("held")
    expect(await a().undo(ID)).toEqual({ state: "undone", remaining: 0 })
    expect(pointing()).toEqual([])
    // What the import did not write stays.
    expect(server.live(KIND.labresult, "someone-elses")).toBeDefined()
    expect(await b().resume(ID)).toEqual({ state: "nothing", status: "undone" })
  })

  it("refuses a late write onto an id Undo deleted, and Undo again deletes a delayed first write", async () => {
    seedPlanned()
    let puts = 0
    const half: typeof fetch = async (input, init) => {
      if (
        init?.method === "PUT" &&
        urlOf(input).includes("/labresult/") &&
        ++puts === 4
      )
        clock.advance(LEASE_MS * 2)
      return server.fetch(input, init)
    }
    expect((await a(half).resume(ID)).state).toBe("lost")
    const landed = resultId("1")
    const notLanded = resultId("5")
    expect(server.live(KIND.labresult, landed)).toBeDefined()
    expect(server.live(KIND.labresult, notLanded)).toBeUndefined()

    expect(await b().undo(ID)).toEqual({ state: "undone", remaining: 0 })
    const late = new SubstrateClient("https://substrate.example.com", "tok")

    // A late write onto a deleted id meets the tombstone's version.
    await expect(
      late.put(KIND.labresult, landed, written(ROWS[0]), 0)
    ).rejects.toMatchObject({ status: 409 })
    expect(server.live(KIND.labresult, landed)).toBeUndefined()

    // A first write that had not landed when Undo ran can still land...
    await late.put(KIND.labresult, notLanded, written(ROWS[4]), 0)
    expect(pointing()).toHaveLength(1)
    // ...and Undo again, from undone, deletes it.
    expect(await b().undo(ID)).toEqual({ state: "undone", remaining: 0 })
    expect(pointing()).toEqual([])
  })

  it("takes a lab's source where one of class lab answers the 409, else the next id", async () => {
    server.fake.seed(KIND.source, labSourceId(LAB), {
      name: "A scale",
      class: "scale",
    })
    server.fake.seed(KIND.source, labSourceId(LAB, 2), {
      name: LAB,
      class: "lab",
    })
    seedPlanned()
    await a().resume(ID)
    expect(imp().source).toBe(labSourceId(LAB, 2))
    expect(server.live(KIND.source, labSourceId(LAB))?.properties.class).toBe(
      "scale"
    )
  })

  it("reads the plan from its blob on a browser that did not stage it", async () => {
    seedPlanned()
    const out = await b().resume(ID)
    expect(out.state).toBe("imported")
    const reads = server.fake.requests.filter((r) => r.path.includes("/blobs/"))
    // The blob read is the wrapper's, not the fake's: the fake saw none.
    expect(reads).toEqual([])
    expect(live(KIND.labresult)).toHaveLength(5)
  })
})

describe("the runs of a page", () => {
  it("runs one import at a time per browser, under a Web Lock", async () => {
    const locks = new FakeLocks()
    const uninstall = locks.install()
    try {
      seedPlanned()
      const registry = new RunRegistry(
        clientStore(
          new SubstrateClient("https://substrate.example.com", "tok")
        ),
        { device: "a", clock, lockPrefix: "healthspan.import." }
      )
      const release = await locks.hold(`healthspan.import.${ID}`)
      expect(await registry.resume(ID)).toEqual({
        state: "failed",
        words: "Another tab of this browser is running it.",
      })
      const undo = registry.undo(ID)
      await Promise.resolve()
      expect(registry.state(ID).running).toBe(true)
      release()
      expect(await undo).toEqual({ state: "undone", remaining: 0 })
      expect(registry.state(ID)).toMatchObject({
        running: false,
        outcome: { state: "undone" },
      })
    } finally {
      uninstall()
    }
  })

  it("stages a file's plan and runs it, the plan kept for the run", async () => {
    const registry = new RunRegistry(
      clientStore(new SubstrateClient("https://substrate.example.com", "tok")),
      { device: "a", clock, lockPrefix: "healthspan.import." }
    )
    const outcomes = await registry.start([{ id: ID, plan: PLAN, lab: LAB }])
    expect(outcomes).toEqual([{ state: "planned" }])
    for (let i = 0; i < 50 && imp().status !== "imported"; i++)
      await new Promise((r) => setTimeout(r, 0))
    expect(imp()).toMatchObject({
      status: "imported",
      importer: "csv",
      lab: LAB,
      source: labSourceId(LAB),
    })
    expect(registry.plan(ID)?.records).toHaveLength(7)
    expect(live(KIND.labresult)).toHaveLength(5)
  })
})
