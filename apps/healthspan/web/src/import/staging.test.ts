import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { Import, KIND, type ImportRecord } from "../kinds"
import { Server, VirtualClock, browser, urlOf } from "./harness"
import { recordId } from "./ids"
import type { Plan, PlannedDocument, RecordKey } from "./importers/types"
import { memoryKeep } from "./keep"
import { blobDigestOf, parsePlan, storedPlan } from "./planfile"
import { LEASE_MS, planRefusal } from "./runner"

const ID = "0194a1b2-0000-7000-8000-0000000051a9"

const SOURCE: RecordKey = {
  kind: KIND.source,
  namespace: "source",
  key: "randox",
}

function doc(n: number): PlannedDocument {
  const bytes = new TextEncoder().encode(`%PDF the report of visit ${n}`)
  return {
    digest: blobDigestOf(bytes),
    name: `randox report 2026-0${n}-01.pdf`,
    mediaType: "application/pdf",
    bytes,
  }
}

const DOCS = [1, 2, 3].map(doc)

/** A provider's plan with a document per report. */
const PLAN: Plan = {
  importer: "randox",
  name: "randox",
  source: SOURCE,
  records: [
    {
      ...SOURCE,
      mode: "create",
      properties: { name: "Randox", class: "lab" },
    },
    ...DOCS.map((d, i) => ({
      kind: KIND.labreport,
      namespace: "randox",
      key: `S${i + 1}`,
      mode: "maintain" as const,
      revision: 1_700_000_000,
      properties: {
        at: `2026-0${i + 1}-01T09:00:00Z`,
        lab: "Randox",
        source: { $ref: SOURCE },
        file: { $doc: d.digest },
      },
    })),
  ],
  documents: DOCS,
  left: [{ what: "notes.txt", why: "It is not a Randox file." }],
}

let server: Server
let clock: VirtualClock

beforeEach(() => {
  clock = new VirtualClock()
  server = new Server(clock)
  vi.stubGlobal("fetch", server.fetch)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

const imp = (): ImportRecord => Import.read(server.importRecord(ID)!)

describe("staging", () => {
  it("writes the record first, names each document as it goes up, then the plan in the patch that moves it to planned", async () => {
    const one = browser(server, "a")
    expect(await one.stage({ id: ID, plan: PLAN })).toEqual({
      state: "planned",
    })
    const writes = server.fake.writes()
    expect(writes[0]).toBe(`PUT ${KIND.import}/${ID}`)
    const first = server.fake.requests.find((r) => r.method === "PUT")!
    expect(first.body).toMatchObject({
      ifVersion: 0,
      properties: {
        status: "staging",
        importer: "randox",
        documents: [],
        runner: { device: "a" },
        skipped: "- notes.txt: It is not a Randox file.",
      },
    })
    const done = imp()
    expect(done.status).toBe("planned")
    expect(done.runner).toBeUndefined()
    expect(new Set(done.documents)).toEqual(new Set(DOCS.map((d) => d.digest)))
    // The plan names every document by its digest and holds no bytes.
    const planBlob = server.blobs.get(done.plan!)!
    const read = parsePlan(planBlob.bytes)
    expect(read.documents.map((d) => d.digest)).toEqual(
      DOCS.map((d) => d.digest)
    )
    expect(JSON.stringify(read)).not.toContain("bytes")
    // The bytes went once the patches named them.
    expect(await one.keep.list()).toEqual([])
    // And from planned on, any device runs it.
    expect(await browser(server, "b").resume(ID)).toMatchObject({
      state: "imported",
      counts: { created: 4 },
    })
    const report = server.live(
      KIND.labreport,
      recordId({ kind: KIND.labreport, namespace: "randox", key: "S2" })
    )!
    expect(report.properties.file).toBe(DOCS[1].digest)
  })

  it("keeps a document by documents past the collector's minute, and the collector takes a blob nothing names", async () => {
    await browser(server, "a").stage({ id: ID, plan: PLAN })
    const stray = new TextEncoder().encode("never named")
    const strayDigest = blobDigestOf(stray)
    const client = browser(server, "a")
    client.install()
    await client.store.putBlob(strayDigest, stray, "text/plain", "stray.txt")
    clock.advance(2 * 60_000)
    server.collect()
    expect(server.collected).toEqual([strayDigest])
    for (const d of DOCS) expect(server.blobs.has(d.digest)).toBe(true)
    expect(server.blobs.has(imp().plan!)).toBe(true)
  })

  it("uploads a blob again that the collector took before its patch", async () => {
    const keep = memoryKeep()
    // The first try: the blobs go up, and the patch naming them never
    // answers; the page closes.
    const noPatch: typeof fetch = async (input, init) => {
      if (init?.method === "PATCH") throw new TypeError("Failed to fetch")
      return server.fetch(input, init)
    }
    const first = await browser(server, "a", { fetch: noPatch, keep }).stage({
      id: ID,
      plan: PLAN,
    })
    expect(first.state).toBe("failed")
    const storedAt = server.blobs.get(DOCS[0].digest)!.at
    expect(imp().documents).toEqual([])

    // Opened again before the minute is out: the upload of a blob already
    // stored changes nothing, so the minute still runs from its first
    // store, and the collector takes it before the patch names it.
    clock.t = storedAt + 50_000
    let moved = false
    const late: typeof fetch = async (input, init) => {
      const res = await server.fetch(input, init)
      if (
        init?.method === "PUT" &&
        urlOf(input).includes("/blobs/") &&
        !moved
      ) {
        moved = true
        clock.advance(11_000)
      }
      return res
    }
    expect(
      await browser(server, "a", { fetch: late, keep }).finish(ID)
    ).toEqual({ state: "planned" })
    expect(server.collected).toEqual(DOCS.map((d) => d.digest))
    // Up on the first try, again on finishing, and again after the refusal.
    expect(server.blobPuts.filter((d) => d === DOCS[0].digest)).toHaveLength(3)
    expect(new Set(imp().documents)).toEqual(new Set(DOCS.map((d) => d.digest)))
    for (const d of DOCS) expect(server.blobs.has(d.digest)).toBe(true)
  })

  it("is interrupted after the record and before the plan, and finished in the same browser", async () => {
    const keep = memoryKeep()
    let planPut = false
    const dies: typeof fetch = async (input, init) => {
      const body = typeof init?.body === "string" ? init.body : ""
      if (init?.method === "PATCH" && body.includes('"planned"')) {
        planPut = true
        throw new TypeError("Failed to fetch")
      }
      return server.fetch(input, init)
    }
    const one = browser(server, "a", { fetch: dies, keep })
    const out = await one.stage({ id: ID, plan: PLAN })
    expect(planPut).toBe(true)
    expect(out.state).toBe("failed")
    expect(imp().status).toBe("staging")
    expect(imp().plan).toBeUndefined()
    // The documents are named, so their bytes are gone; the plan is kept.
    const kept = await keep.get(ID)
    expect(kept?.documents).toEqual([])
    expect(kept?.plan.records).toHaveLength(4)
    // No other device resumes it.
    expect(await browser(server, "b").resume(ID)).toEqual({
      state: "nothing",
      status: "staging",
    })

    // The page opens again in this browser: Finish.
    clock.advance(LEASE_MS)
    const again = browser(server, "a", { keep })
    expect(await again.finish(ID)).toEqual({ state: "planned" })
    expect(imp().status).toBe("planned")
    expect(await keep.list()).toEqual([])
    expect((await again.resume(ID)).state).toBe("imported")
  })

  it("is discarded from another device: the lease waited out, the record deleted, and the stager stops", async () => {
    const keep = memoryKeep()
    // The stager stops after the first batch, its page closed.
    const dies: typeof fetch = async (input, init) => {
      if (init?.method === "PUT" && urlOf(input).includes("/blobs/"))
        throw new TypeError("Failed to fetch")
      return server.fetch(input, init)
    }
    expect(
      (
        await browser(server, "a", { fetch: dies, keep }).stage({
          id: ID,
          plan: PLAN,
        })
      ).state
    ).toBe("failed")
    expect(imp().status).toBe("staging")
    expect(imp().runner?.device).toBe("a")

    const other = browser(server, "b")
    expect(await other.discard(ID)).toEqual({ state: "discarded" })
    // It waited for the stager's lease to lapse, then took it.
    expect(other.progress).toContainEqual(
      expect.objectContaining({ phase: "waiting" })
    )
    expect(server.importRecord(ID)?.deletedAt).toBeDefined()
    const del = server.fake.requests.find((r) => r.method === "DELETE")!
    expect(del.query.get("ifVersion")).toBe(
      String(server.importRecord(ID)!.version - 1)
    )

    // The stager comes back, finds its record gone, and drops what it kept.
    expect(await browser(server, "a", { keep }).finish(ID)).toEqual({
      state: "discarded",
    })
    expect(await keep.list()).toEqual([])
  })

  it("refuses a plan whose documents one import cannot name, before anything goes up", async () => {
    const many: PlannedDocument[] = Array.from({ length: 14_000 }, (_, i) =>
      doc(i)
    )
    const plan: Plan = { ...PLAN, documents: many }
    expect(planRefusal(plan)).toMatch(/14,000 documents/)
    expect(planRefusal(PLAN)).toBeUndefined()
    const out = await browser(server, "a").stage({ id: ID, plan })
    expect(out.state).toBe("failed")
    expect(server.fake.requests).toEqual([])
    expect(server.blobPuts).toEqual([])
  })

  it("names each document once, however many records name it", () => {
    const twice: Plan = { ...PLAN, documents: [...DOCS, DOCS[0]] }
    expect(storedPlan(twice).documents).toHaveLength(3)
  })
})

describe("what a browser keeps", () => {
  it("keeps a staging import in IndexedDB per repository, its bytes dropped as they are named", async () => {
    const { indexedDbKeep } = await import("./keep")
    const here = indexedDbKeep("https://one.example.com")
    const there = indexedDbKeep("https://two.example.com")
    await here.put({
      id: ID,
      name: "randox",
      importer: "randox",
      plan: storedPlan(PLAN),
      documents: DOCS.map((d) => ({ ...d })),
      startedAt: 1,
    })
    expect(await here.list()).toEqual([ID])
    expect(await there.list()).toEqual([])
    await here.dropDocuments(ID, [DOCS[0].digest])
    const kept = await here.get(ID)
    expect(kept?.documents.map((d) => d.digest)).toEqual(
      DOCS.slice(1).map((d) => d.digest)
    )
    expect(kept?.documents[0].bytes).toEqual(DOCS[1].bytes)
    await here.drop(ID)
    expect(await here.list()).toEqual([])
  })
})
