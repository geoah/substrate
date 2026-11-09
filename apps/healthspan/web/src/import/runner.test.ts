import { SubstrateClient, type SubstrateRecord } from "@substrate/client"
import { FakeLocks, FakeSubstrate } from "@substrate/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { Import, KIND, type ImportRow, type LabImport } from "../kinds"
import { labSourceId, reportId, resultId } from "./ids"
import { refPath, resultProps } from "./records"
import { RunRegistry } from "./registry"
import {
  LEASE_MS,
  resumeImport,
  undoImport,
  type Clock,
  type RunProgress,
} from "./runner"
import { clientStore } from "./store"

/** Time that moves only when the runner waits, or a test moves it: the
 * lease's tests run at once. */
class VirtualClock implements Clock {
  t = Date.parse("2026-11-01T10:00:00Z")
  private timers: { at: number; fire: () => void }[] = []
  now = () => this.t
  advance(ms: number) {
    this.t += ms
    for (const timer of this.timers.filter((x) => x.at <= this.t)) {
      this.timers.splice(this.timers.indexOf(timer), 1)
      timer.fire()
    }
  }
  sleep = (ms: number) => {
    this.advance(Math.max(0, ms))
    return Promise.resolve()
  }
  deadline = (at: number) => {
    const c = new AbortController()
    const timer = { at, fire: () => c.abort() }
    if (at <= this.t) c.abort()
    else this.timers.push(timer)
    return {
      signal: c.signal,
      clear: () => {
        const i = this.timers.indexOf(timer)
        if (i >= 0) this.timers.splice(i, 1)
      },
    }
  }
}

const ORIGIN = "https://substrate.example.com"

const urlOf = (input: string | URL | Request) =>
  typeof input === "string"
    ? input
    : input instanceof URL
      ? input.href
      : input.url
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

let fake: FakeSubstrate
let clock: VirtualClock

function seedImport(extra: Record<string, unknown> = {}): SubstrateRecord {
  return fake.seed(KIND.import, ID, {
    ...Import.write({
      name: "results.csv",
      format: "csv",
      status: "planned",
      rows: ROWS,
      reports: REPORTS,
      results: 0,
    }),
    ...extra,
  })
}

/** One browser: its own client over the fake, its own device id. */
function browser(device: string, fetchImpl: typeof fetch = fake.fetch) {
  const client = new SubstrateClient(ORIGIN, "tok")
  const store = clientStore(client)
  const progress: RunProgress[] = []
  const opts = {
    store,
    device,
    clock,
    labName: LAB,
    onProgress: (p: RunProgress) => progress.push(p),
  }
  return {
    client,
    store,
    progress,
    resume: (o: Partial<typeof opts> = {}) => {
      vi.stubGlobal("fetch", fetchImpl)
      return resumeImport(ID, { ...opts, ...o })
    },
    undo: (o: Partial<typeof opts> = {}) => {
      vi.stubGlobal("fetch", fetchImpl)
      return undoImport(ID, { ...opts, ...o })
    },
  }
}

function imp(): LabImport {
  return Import.read(fake.records.get(`${KIND.import}/${ID}`)!)
}

const live = (kind: string) =>
  [...fake.records.values()].filter((r) => r.kind === kind && !r.deletedAt)

const pointing = () =>
  [...live(KIND.labresult), ...live(KIND.labreport)].filter(
    (r) => refPath(r.properties.origin) === `${KIND.import}/${ID}`
  )

/** The fake with the server's tombstones: a create (`ifVersion: 0`) onto a
 * deleted record meets the tombstone's version and is refused. */
function withTombstones(): typeof fetch {
  return async (input, init = {}) => {
    const url = new URL(urlOf(input))
    const m = /^\/api\/v1\/([^/]+\/[^/]+\/[^/]+)\/([^/]+)$/.exec(url.pathname)
    if (m && init.method === "PUT" && typeof init.body === "string") {
      const body = JSON.parse(init.body) as { ifVersion?: number }
      const here = fake.records.get(`${m[1]}/${decodeURIComponent(m[2])}`)
      if (body.ifVersion === 0 && here?.deletedAt)
        return new Response(
          JSON.stringify({
            error: { code: "conflict", message: `at version ${here.version}` },
          }),
          { status: 409, headers: { "Content-Type": "application/json" } }
        )
    }
    return fake.fetch(input, init)
  }
}

beforeEach(() => {
  fake = new FakeSubstrate(ORIGIN)
  clock = new VirtualClock()
  vi.stubGlobal("fetch", fake.fetch)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("the runner", () => {
  it("runs a planned import: the lab's source, each report, each result, then imported", async () => {
    seedImport()
    const out = await browser("a").resume()
    expect(out).toEqual({ state: "imported", results: 5 })
    const done = imp()
    expect(done.status).toBe("imported")
    expect(done.runner).toBeUndefined()
    expect(done.results).toBe(5)
    expect(done.importedAt).toBeDefined()
    const source = labSourceId(LAB)
    expect(done.source).toBe(source)
    expect(fake.live(KIND.source, source)?.properties).toEqual({
      name: LAB,
      class: "lab",
    })
    expect(
      live(KIND.labreport)
        .map((r) => r.id)
        .sort()
    ).toEqual([reportId(ID, "1"), reportId(ID, "2")].sort())
    expect(
      live(KIND.labresult)
        .map((r) => r.id)
        .sort()
    ).toEqual(ROWS.map((r) => resultId(ID, r.key)).sort())
    expect(pointing()).toHaveLength(7)
    // Every planned write is a create at version 0.
    const puts = fake.requests.filter(
      (r) => r.method === "PUT" && r.path.includes("/labs/")
    )
    expect(
      puts.every((r) => (r.body as { ifVersion?: number }).ifVersion === 0)
    ).toBe(true)
  })

  it("takes the lease for 60 seconds, held to the version read", async () => {
    seedImport()
    const b = browser("a")
    await b.resume()
    const take = fake.requests.find(
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

  it("renews the lease every 20 seconds while it writes", async () => {
    seedImport()
    const slow: typeof fetch = async (input, init) => {
      if (init?.method === "PUT") clock.advance(8_000)
      return fake.fetch(input, init)
    }
    await browser("a", slow).resume()
    const leases = fake.requests
      .filter((r) => r.method === "PATCH" && r.path.endsWith(ID))
      .map(
        (r) =>
          (r.body as { properties: { runner?: { until: string } | null } })
            .properties
      )
    // The take, at least two renewals, and the end.
    expect(leases.length).toBeGreaterThanOrEqual(4)
    const untils = leases
      .map((p) => p.runner?.until)
      .filter((u): u is string => !!u)
      .map(Date.parse)
    expect(untils).toEqual([...untils].sort((a, b) => a - b))
    expect(imp().status).toBe("imported")
  })

  it("stops at once when a renewal is refused: another browser took over", async () => {
    seedImport()
    let puts = 0
    const meddled: typeof fetch = async (input, init) => {
      if (init?.method === "PUT" && urlOf(input).includes("/labs/")) {
        clock.advance(8_000)
        if (++puts === 3)
          fake.change(KIND.import, ID, {
            runner: {
              device: "b",
              until: new Date(clock.t + LEASE_MS).toISOString(),
            },
          })
      }
      return fake.fetch(input, init)
    }
    const out = await browser("a", meddled).resume()
    expect(out.state).toBe("lost")
    expect(imp().runner?.device).toBe("b")
    // The renewal refused is the last request the browser sent.
    const last = fake.requests.at(-1)!
    expect(last.method).toBe("PATCH")
    expect(last.path.endsWith(ID)).toBe(true)
    expect(live(KIND.labresult).length).toBeLessThan(ROWS.length)
  })

  it("refuses to resume while another browser's lease is live, and Undo waits it out", async () => {
    seedImport({
      status: "importing",
      source: `${KIND.source}/lab/x`,
      runner: { device: "b", until: new Date(clock.t + 40_000).toISOString() },
    })
    const a = browser("a")
    expect(await a.resume()).toEqual({
      state: "held",
      until: clock.t + 40_000,
    })
    expect(fake.writes()).toEqual([])
    const out = await a.undo()
    expect(out).toEqual({ state: "undone", remaining: 0 })
    expect(a.progress).toContainEqual({
      phase: "waiting",
      until: Date.parse("2026-11-01T10:00:40Z"),
    })
    // It waited past the other lease's end by 30 seconds before deleting.
    expect(a.progress).toContainEqual({
      phase: "settling",
      until: Date.parse("2026-11-01T10:01:10Z"),
    })
    expect(imp().status).toBe("undone")
  })

  it("answers a lost answer: a write that landed meets 409 and compares equal", async () => {
    seedImport()
    let puts = 0
    const lossy: typeof fetch = async (input, init) => {
      const res = await fake.fetch(input, init)
      if (
        init?.method === "PUT" &&
        urlOf(input).includes("/labresult/") &&
        ++puts === 2
      )
        throw new TypeError("Failed to fetch")
      return res
    }
    expect(await browser("a", lossy).resume()).toEqual({
      state: "imported",
      results: 5,
    })
    expect(live(KIND.labresult)).toHaveLength(5)
  })

  it("stops on a conflict that does not compare equal, and resumes from stopped through importing", async () => {
    seedImport()
    const clash = resultId(ID, "3")
    fake.seed(KIND.labresult, clash, {
      metric: `${KIND.metric}/labs/triglycerides`,
      value: { value: "9.9", unit: "mmol/L" },
      at: "2026-03-12T12:00:00+00:00",
      origin: `${KIND.import}/${ID}`,
    })
    const out = await browser("a").resume()
    expect(out.state).toBe("stopped")
    expect(imp().status).toBe("stopped")
    expect(imp().problem).toMatch(/Result 3 is already in the repository/)
    expect(imp().runner).toBeUndefined()
    expect(imp().results).toBe(2)

    // The owner mends the row; a resume moves stopped to importing in the
    // patch that takes the lease, then writes from the start.
    const props = resultProps(
      { id: ID, source: imp().source!, lab: LAB },
      ROWS[2]
    )
    fake.change(KIND.labresult, clash, props)
    const marker = fake.requests.length
    const again = await browser("b").resume()
    expect(again).toEqual({ state: "imported", results: 5 })
    const take = fake.requests
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
    seedImport()
    let puts = 0
    const dies: typeof fetch = async (input, init) => {
      if (
        init?.method === "PUT" &&
        urlOf(input).includes("/labresult/") &&
        ++puts === 3
      ) {
        clock.advance(LEASE_MS)
        fake.change(KIND.import, ID, { results: 2 })
      }
      return fake.fetch(input, init)
    }
    expect((await browser("a", dies).resume()).state).toBe("lost")
    expect(imp().status).toBe("importing")
    clock.advance(LEASE_MS)
    expect(await browser("b").resume()).toEqual({
      state: "imported",
      results: 5,
    })
    expect(live(KIND.labresult)).toHaveLength(5)
  })

  it("runs the undo again from undoing", async () => {
    seedImport()
    await browser("a").resume()
    fake.change(KIND.import, ID, {
      status: "undoing",
      runner: { device: "a", until: new Date(clock.t - 1000).toISOString() },
    })
    expect(await browser("b").resume()).toEqual({
      state: "undone",
      remaining: 0,
    })
    expect(pointing()).toEqual([])
  })

  it("undoes a partial import, and a second browser resuming during and after it does nothing", async () => {
    seedImport()
    fake.seed(KIND.labresult, "someone-elses", {
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
      return fake.fetch(input, init)
    }
    expect((await browser("a", stopsEarly).resume()).state).toBe("stopped")
    expect(live(KIND.labresult)).toHaveLength(3)

    // Undo holds the lease while it waits for writes in flight to land.
    fake.change(KIND.import, ID, {
      status: "undoing",
      runner: {
        device: "a",
        until: new Date(clock.t + LEASE_MS).toISOString(),
      },
    })
    expect((await browser("b").resume()).state).toBe("held")
    expect(await browser("a").undo()).toEqual({ state: "undone", remaining: 0 })
    expect(pointing()).toEqual([])
    // What the import did not write stays.
    expect(fake.live(KIND.labresult, "someone-elses")).toBeDefined()
    expect(await browser("b").resume()).toEqual({
      state: "nothing",
      status: "undone",
    })
  })

  it("refuses a late write onto an id Undo deleted, and Undo again deletes a delayed first write", async () => {
    seedImport()
    const tomb = withTombstones()
    let puts = 0
    const half: typeof fetch = async (input, init) => {
      if (
        init?.method === "PUT" &&
        urlOf(input).includes("/labresult/") &&
        ++puts === 4
      ) {
        clock.advance(LEASE_MS * 2)
        fake.change(KIND.import, ID, { results: 3 })
      }
      return tomb(input, init)
    }
    expect((await browser("a", half).resume()).state).toBe("lost")
    const landed = resultId(ID, "1")
    const notLanded = resultId(ID, "5")
    expect(fake.live(KIND.labresult, landed)).toBeDefined()
    expect(fake.live(KIND.labresult, notLanded)).toBeUndefined()

    expect(await browser("b", tomb).undo()).toEqual({
      state: "undone",
      remaining: 0,
    })
    const target = { id: ID, source: imp().source!, lab: LAB }
    const late = new SubstrateClient(ORIGIN, "tok")
    vi.stubGlobal("fetch", tomb)

    // A late write onto a deleted id meets the tombstone's version.
    await expect(
      late.put(KIND.labresult, landed, resultProps(target, ROWS[0]), 0)
    ).rejects.toMatchObject({ status: 409 })
    expect(fake.live(KIND.labresult, landed)).toBeUndefined()

    // A first write that had not landed when Undo ran can still land...
    await late.put(KIND.labresult, notLanded, resultProps(target, ROWS[4]), 0)
    expect(pointing()).toHaveLength(1)
    // ...and Undo again, from undone, deletes it.
    expect(await browser("b", tomb).undo()).toEqual({
      state: "undone",
      remaining: 0,
    })
    expect(pointing()).toEqual([])
  })

  it("takes a lab's source where one of class lab answers the 409, else the next id", async () => {
    fake.seed(KIND.source, labSourceId(LAB), {
      name: "A scale",
      class: "scale",
    })
    fake.seed(KIND.source, labSourceId(LAB, 2), { name: LAB, class: "lab" })
    seedImport()
    await browser("a").resume()
    expect(imp().source).toBe(labSourceId(LAB, 2))
    expect(fake.live(KIND.source, labSourceId(LAB))?.properties.class).toBe(
      "scale"
    )
  })
})

describe("the runs of a page", () => {
  it("runs one import at a time per browser, under a Web Lock", async () => {
    const locks = new FakeLocks()
    const uninstall = locks.install()
    try {
      seedImport()
      const registry = new RunRegistry(
        clientStore(new SubstrateClient(ORIGIN, "tok")),
        {
          device: "a",
          clock,
          lockPrefix: "healthspan.import.",
        }
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
})
