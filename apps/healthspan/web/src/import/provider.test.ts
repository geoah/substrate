import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { Import, KIND, type ImportRecord } from "../kinds"
import { Server, VirtualClock, browser, urlOf } from "./harness"
import { derivedMintedId, recordId } from "./ids"
import type { Plan, PlannedRecord, RecordKey } from "./importers/types"
import { LEASE_MS } from "./runner"

// A provider's plans, written by hand in the shape the importers make:
// its source, an organization, reports and results keyed by the provider's
// own keys and decided by revision.

const ORG_KIND = "substrate.reamde.dev/clinical/organization"
const SOURCE: RecordKey = { kind: KIND.source, namespace: "source", key: "nhs" }
const ORG: RecordKey = { kind: ORG_KIND, namespace: "ods", key: "A81001" }
const ODS = "https://fhir.nhs.uk/Id/ods-organization-code"

const report = (key: string): RecordKey => ({
  kind: KIND.labreport,
  namespace: "nhs",
  key,
})
const result = (key: string): RecordKey => ({
  kind: KIND.labresult,
  namespace: "nhs",
  key,
})

const sourceRecord: PlannedRecord = {
  ...SOURCE,
  mode: "create",
  properties: { name: "Riverside Surgery", class: "lab" },
}

function orgRecord(
  revision: number,
  props: Record<string, unknown>
): PlannedRecord {
  return { ...ORG, mode: "merge", revision, properties: props }
}

function reportRecord(
  key: string,
  revision: number,
  extra: Record<string, unknown> = {}
): PlannedRecord {
  return {
    ...report(key),
    mode: "maintain",
    revision,
    createOnly: ["review"],
    properties: {
      at: "2026-03-12T09:00:00Z",
      lab: "Riverside Surgery",
      source: { $ref: SOURCE },
      review: "pending",
      ...extra,
    },
  }
}

function resultRecord(
  key: string,
  rep: string,
  value: string,
  revision: number,
  extra: Record<string, unknown> = {}
): PlannedRecord {
  return {
    ...result(key),
    mode: "maintain",
    revision,
    properties: {
      metric: `${KIND.metric}/labs/ferritin`,
      source: { $ref: SOURCE },
      report: { $ref: report(rep) },
      at: "2026-03-12T09:00:00Z",
      form: "gauge",
      value: { value, unit: "ug/L" },
      note: null,
      ...extra,
    },
  }
}

function plan(records: PlannedRecord[], name = "nhs-2026-03"): Plan {
  return {
    importer: "nhs",
    name,
    source: SOURCE,
    records: [sourceRecord, ...records],
    documents: [],
    left: [],
  }
}

const A = "0194a1b2-0000-7000-8000-0000000000a1"
const B = "0194a1b2-0000-7000-8000-0000000000b2"

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

const idOf = (k: RecordKey) => recordId(k)
const live = (k: RecordKey) => server.live(k.kind, idOf(k))
const imp = (id: string): ImportRecord => Import.read(server.importRecord(id)!)
const originOf = (id: string) => `${KIND.import}/${id}`

/** An import staged and run in one browser. */
async function importPlan(
  id: string,
  p: Plan,
  fetch?: typeof globalThis.fetch
) {
  const one = browser(server, "a", { fetch })
  const staged = await one.stage({ id, plan: p })
  expect(staged).toEqual({ state: "planned" })
  return one.resume(id)
}

describe("a provider's ids", () => {
  it("derives each id from healthspan/<namespace>/<kind name>/<key>, whatever the key's length", () => {
    expect(idOf(report("T1"))).toBe(
      derivedMintedId("healthspan/nhs/labreport/T1")
    )
    expect(idOf(SOURCE)).toBe(derivedMintedId("healthspan/source/source/nhs"))
    expect(idOf(ORG)).toBe(
      derivedMintedId("healthspan/ods/organization/A81001")
    )
    const long = idOf(result(`T1/${"x".repeat(500)}`))
    expect(long).toMatch(/^[0-7][0-9a-hjkmnp-tv-z]{25}$/)
    // internal/runner/testdata/id_vectors.json
    expect(derivedMintedId("x")).toBe("1de4b45ds6g1282rkwn7xtrcqn")
  })

  it("lands a second import of the same export on the same records, each unchanged", async () => {
    const p = plan([
      reportRecord("T1", 100),
      resultRecord("T1/ferr", "T1", "80", 100),
    ])
    expect(await importPlan(A, p)).toEqual({
      state: "imported",
      counts: { created: 3, updated: 0, unchanged: 0, stale: 0, left: 0 },
    })
    expect(await importPlan(B, p)).toEqual({
      state: "imported",
      counts: { created: 0, updated: 0, unchanged: 3, stale: 0, left: 0 },
    })
    expect(server.all(KIND.labresult)).toHaveLength(1)
    expect(live(result("T1/ferr"))?.properties.origin).toBe(originOf(A))
  })
})

describe("a reference", () => {
  it("is left out where no import has written its record, with a note, and kept where one has", async () => {
    // T0's report an earlier import wrote; T9's no import has.
    await importPlan(A, plan([reportRecord("T0", 100)]))
    const p = plan([
      resultRecord("T0/ferr", "T0", "70", 100),
      resultRecord("T9/ferr", "T9", "90", 100),
    ])
    expect(await importPlan(B, p)).toMatchObject({
      state: "imported",
      counts: { created: 2, left: 1 },
    })
    expect(live(result("T0/ferr"))?.properties.report).toBe(
      `${KIND.labreport}/${idOf(report("T0"))}`
    )
    expect(live(result("T9/ferr"))?.properties.report).toBeUndefined()
    expect(imp(B).skipped).toMatch(
      /Result T9\/ferr: its reference to report T9 was left out/
    )
  })
})

describe("the decision on a record already there", () => {
  it("creates one that is absent, with origin the import", async () => {
    await importPlan(A, plan([reportRecord("T1", 100)]))
    expect(live(report("T1"))?.properties).toMatchObject({
      origin: originOf(A),
      revision: 100,
      source: `${KIND.source}/${idOf(SOURCE)}`,
      review: "pending",
    })
  })

  it("restores a tombstone by a PUT held to its version, with origin this import", async () => {
    await importPlan(A, plan([reportRecord("T1", 100)]))
    const id = idOf(report("T1"))
    server.fake.records.set(`${KIND.labreport}/${id}`, {
      ...server.fake.records.get(`${KIND.labreport}/${id}`)!,
      version: 5,
      deletedAt: "2026-11-01T10:05:00Z",
    })
    expect(await importPlan(B, plan([reportRecord("T1", 100)]))).toMatchObject({
      counts: { created: 1, unchanged: 1 },
    })
    expect(live(report("T1"))?.properties.origin).toBe(originOf(B))
    const restore = server.seen
      .filter((r) => r.method === "PUT" && r.path.endsWith(id))
      .at(-1)
    expect((restore?.body as { ifVersion: number }).ifVersion).toBe(5)
  })

  it("restores a tombstone as the plan creates it: a value the older export gave and this one does not is not brought back", async () => {
    await importPlan(
      A,
      plan([
        reportRecord("T1", 200),
        resultRecord("T1/a", "T1", "1", 200, { note: "Haemolysed" }),
      ])
    )
    await browser(server, "a").undo(A)
    expect(live(result("T1/a"))).toBeUndefined()
    // Not yet collected: the tombstone keeps the note and revision 200.
    expect(
      await importPlan(
        B,
        plan([reportRecord("T1", 100), resultRecord("T1/a", "T1", "1", 100)])
      )
    ).toMatchObject({ counts: { created: 2 } })
    const restored = live(result("T1/a"))!.properties
    expect(restored.note).toBeUndefined()
    expect(restored.revision).toBe(100)
    expect(restored.origin).toBe(originOf(B))
  })

  it("leaves one with the same content at a revision no lower unchanged, and writes nothing", async () => {
    await importPlan(A, plan([reportRecord("T1", 200)]))
    const before = live(report("T1"))!.version
    expect(await importPlan(B, plan([reportRecord("T1", 100)]))).toMatchObject({
      counts: { unchanged: 2, updated: 0, stale: 0 },
    })
    expect(live(report("T1"))!.version).toBe(before)
  })

  it("raises the revision alone of one with the same content at a lower revision, counted updated", async () => {
    await importPlan(A, plan([reportRecord("T1", 100)]))
    const marker = server.fake.requests.length
    expect(await importPlan(B, plan([reportRecord("T1", 300)]))).toMatchObject({
      counts: { updated: 1, unchanged: 1 },
    })
    const id = idOf(report("T1"))
    const write = server.fake.requests
      .slice(marker)
      .filter((r) => r.method === "PUT" && r.path.endsWith(id))
      .at(-1)!
    expect((write.body as { properties: unknown }).properties).toEqual({
      revision: 300,
    })
    expect(live(report("T1"))?.properties.revision).toBe(300)
    expect(live(report("T1"))?.properties.origin).toBe(originOf(A))
  })

  it("updates one with other content at a revision no higher, writing null where the provider dropped a value, origin left", async () => {
    await importPlan(
      A,
      plan([
        reportRecord("T1", 100),
        resultRecord("T1/ferr", "T1", "80", 100, { note: "Repeat in a year" }),
      ])
    )
    const marker = server.fake.requests.length
    expect(
      await importPlan(
        B,
        plan([
          reportRecord("T1", 100),
          resultRecord("T1/ferr", "T1", "85", 200),
        ])
      )
    ).toMatchObject({ counts: { updated: 1, unchanged: 2 } })
    const id = idOf(result("T1/ferr"))
    const write = server.fake.requests
      .slice(marker)
      .filter((r) => r.method === "PUT" && r.path.endsWith(id))
      .at(-1)!
    expect(
      (write.body as { properties: Record<string, unknown> }).properties
    ).toMatchObject({
      value: { value: "85", unit: "ug/L" },
      note: null,
      revision: 200,
    })
    const after = live(result("T1/ferr"))!.properties
    expect(after.note).toBeUndefined()
    expect(after.value).toEqual({ value: "85", unit: "ug/L" })
    expect(after.origin).toBe(originOf(A))
  })

  it("compares quantities by number and unit, instants as instants, and references by target", async () => {
    await importPlan(A, plan([resultRecord("T1/ferr", "T1", "80", 100)]))
    const id = idOf(result("T1/ferr"))
    // The server keeps the digits as written and serves references and
    // instants in its own form.
    const stored = server.fake.records.get(`${KIND.labresult}/${id}`)!
    server.fake.records.set(`${KIND.labresult}/${id}`, {
      ...stored,
      properties: {
        ...stored.properties,
        value: { value: "80.0", unit: "ug/L" },
        at: "2026-03-12T10:00:00+01:00",
        metric: { ref: stored.properties.metric },
      },
    })
    expect(
      await importPlan(B, plan([resultRecord("T1/ferr", "T1", "80", 100)]))
    ).toMatchObject({ counts: { unchanged: 2, updated: 0 } })
  })

  it("leaves one a newer export wrote stale", async () => {
    await importPlan(A, plan([reportRecord("T1", 300, { status: "final" })]))
    expect(
      await importPlan(
        B,
        plan([reportRecord("T1", 100, { status: "preliminary" })])
      )
    ).toMatchObject({ counts: { stale: 1 } })
    expect(live(report("T1"))?.properties.status).toBe("final")
  })

  it("decides again when the version moved under its write", async () => {
    await importPlan(A, plan([reportRecord("T1", 100, { status: "final" })]))
    const id = idOf(report("T1"))
    let meddled = 0
    // Another device writes the report between the read and the write, twice.
    const racing: typeof fetch = async (input, init) => {
      if (
        init?.method === "PUT" &&
        urlOf(input).endsWith(id) &&
        typeof init.body === "string" &&
        !init.body.includes('"ifVersion":0') &&
        meddled < 2
      ) {
        meddled++
        server.fake.change(KIND.labreport, id, { lab: `Edited ${meddled}` })
      }
      return server.fetch(input, init)
    }
    expect(
      await importPlan(
        B,
        plan([reportRecord("T1", 200, { status: "amended" })]),
        racing
      )
    ).toMatchObject({ state: "imported", counts: { updated: 1 } })
    expect(meddled).toBe(2)
    expect(live(report("T1"))?.properties).toMatchObject({
      status: "amended",
      lab: "Riverside Surgery",
      revision: 200,
    })
  })

  it("stops the import when the version moves under every write", async () => {
    await importPlan(A, plan([reportRecord("T1", 100, { status: "final" })]))
    const id = idOf(report("T1"))
    let n = 0
    const racing: typeof fetch = async (input, init) => {
      if (
        init?.method === "PUT" &&
        urlOf(input).endsWith(id) &&
        typeof init.body === "string" &&
        !init.body.includes('"ifVersion":0')
      )
        server.fake.change(KIND.labreport, id, { lab: `Edited ${++n}` })
      return server.fetch(input, init)
    }
    expect(
      await importPlan(
        B,
        plan([reportRecord("T1", 200, { status: "amended" })]),
        racing
      )
    ).toMatchObject({ state: "stopped" })
    expect(imp(B).problem).toMatch(/Report T1 changed under every write/)
  })

  it("keeps a property written only at create, the owner's review", async () => {
    await importPlan(A, plan([reportRecord("T1", 100)]))
    server.fake.change(KIND.labreport, idOf(report("T1")), {
      review: "checked",
    })
    expect(
      await importPlan(
        B,
        plan([reportRecord("T1", 200, { status: "amended" })])
      )
    ).toMatchObject({ counts: { updated: 1 } })
    expect(live(report("T1"))?.properties).toMatchObject({
      review: "checked",
      status: "amended",
    })
  })
})

describe("a provider's source", () => {
  it("is created once, and taken as it is after the owner renamed it", async () => {
    await importPlan(A, plan([reportRecord("T1", 100)]))
    expect(imp(A).source).toBe(idOf(SOURCE))
    expect(live(SOURCE)?.properties).toEqual({
      name: "Riverside Surgery",
      class: "lab",
    })
    server.fake.change(KIND.source, idOf(SOURCE), { name: "My GP" })
    const out = await importPlan(B, plan([reportRecord("T1", 100)]))
    expect(out).toMatchObject({ counts: { unchanged: 2 } })
    expect(imp(B).source).toBe(idOf(SOURCE))
    expect(live(SOURCE)?.properties.name).toBe("My GP")
    expect(live(SOURCE)?.properties.origin).toBeUndefined()
  })
})

describe("an organization", () => {
  const newer = orgRecord(200, {
    name: "Riverside Surgery",
    identifiers: [{ system: ODS, value: "A81001", label: "ODS code" }],
    address: { lines: ["1 River Road"], city: "Leeds" },
  })
  const older = orgRecord(100, {
    name: "RIVERSIDE SURGERY",
    identifiers: [
      { system: ODS, value: "A81001" },
      { system: "urn:practice", value: "77" },
    ],
    telecom: [{ system: "phone", value: "0113 000 0000" }],
  })

  it("from an older export after a newer one: identifiers unioned, a missing field added, nothing cleared, the revision kept", async () => {
    await importPlan(A, plan([newer]))
    expect(await importPlan(B, plan([older]))).toMatchObject({
      counts: { updated: 1, stale: 0 },
    })
    const o = live(ORG)!.properties
    expect(o.name).toBe("Riverside Surgery")
    expect(o.address).toEqual({ lines: ["1 River Road"], city: "Leeds" })
    expect(o.telecom).toEqual([{ system: "phone", value: "0113 000 0000" }])
    expect(o.identifiers).toEqual([
      { system: ODS, value: "A81001", label: "ODS code" },
      { system: "urn:practice", value: "77" },
    ])
    expect(o.revision).toBe(200)
    expect(o.origin).toBeUndefined()
  })

  it("from a newer export after an older one: the address added and the revision raised", async () => {
    await importPlan(A, plan([older]))
    expect(await importPlan(B, plan([newer]))).toMatchObject({
      counts: { updated: 1 },
    })
    const o = live(ORG)!.properties
    expect(o.name).toBe("RIVERSIDE SURGERY")
    expect(o.address).toEqual({ lines: ["1 River Road"], city: "Leeds" })
    expect(o.revision).toBe(200)
    // Merged again, nothing to add: unchanged, never stale.
    expect(
      await importPlan("0194a1b2-0000-7000-8000-0000000000c3", plan([older]))
    ).toMatchObject({ counts: { unchanged: 2, stale: 0 } })
  })
})

describe("counts", () => {
  it("are the latest run's after a lost answer and a page closed before the counts were saved; what it created is counted by origin", async () => {
    const p = plan([
      reportRecord("T1", 100),
      resultRecord("T1/a", "T1", "1", 100),
      resultRecord("T1/b", "T1", "2", 100),
      resultRecord("T1/c", "T1", "3", 100),
    ])
    let puts = 0
    // The second result lands and its answer is lost; the page then closes
    // before the counts are saved.
    const closes: typeof fetch = async (input, init) => {
      const res = await server.fetch(input, init)
      if (init?.method === "PUT" && urlOf(input).includes("/labresult/")) {
        puts++
        if (puts === 2) throw new TypeError("Failed to fetch")
        if (puts === 3) clock.advance(LEASE_MS)
      }
      return res
    }
    const one = browser(server, "a", { fetch: closes })
    expect(await one.stage({ id: A, plan: p })).toEqual({ state: "planned" })
    expect((await one.resume(A)).state).toBe("lost")
    expect(imp(A).status).toBe("importing")
    clock.advance(LEASE_MS)
    expect(await browser(server, "b").resume(A)).toEqual({
      state: "imported",
      counts: { created: 1, updated: 0, unchanged: 4, stale: 0, left: 0 },
    })
    expect(imp(A).counts).toEqual({
      created: 1,
      updated: 0,
      unchanged: 4,
      stale: 0,
      left: 0,
    })
    // Sources counts what it created by origin, whatever its runs counted.
    const two = browser(server, "b")
    two.install()
    const created = await two.store.pointingAt(
      [KIND.labresult, KIND.labreport],
      originOf(A)
    )
    expect(created).toHaveLength(4)
  })
})

describe("Undo of a provider's imports", () => {
  const first = plan([
    orgRecord(100, {
      name: "Riverside Surgery",
      identifiers: [{ system: ODS, value: "A81001" }],
    }),
    reportRecord("T1", 100),
    resultRecord("T1/a", "T1", "1", 100),
    resultRecord("T1/b", "T1", "2", 100),
  ])
  const second = plan([
    orgRecord(200, {
      name: "Riverside Surgery",
      identifiers: [{ system: ODS, value: "A81001" }],
      address: { city: "Leeds" },
    }),
    reportRecord("T1", 200, { status: "amended" }),
    resultRecord("T1/a", "T1", "1", 200),
    resultRecord("T1/b", "T1", "2.5", 200),
    resultRecord("T1/c", "T1", "3", 200),
  ])

  beforeEach(async () => {
    await importPlan(A, first)
    expect(await importPlan(B, second)).toMatchObject({
      // The organization merged, the report and one result rewritten, the
      // other result's revision raised; the source as it was.
      counts: { created: 1, updated: 4, unchanged: 1 },
    })
  })

  it("of the later deletes only what it created: what it updated stays updated", async () => {
    expect(await browser(server, "a").undo(B)).toEqual({
      state: "undone",
      remaining: 0,
    })
    expect(live(result("T1/c"))).toBeUndefined()
    expect(live(result("T1/b"))?.properties.value).toEqual({
      value: "2.5",
      unit: "ug/L",
    })
    expect(live(report("T1"))?.properties.status).toBe("amended")
    expect(live(result("T1/a"))).toBeDefined()
    expect(live(SOURCE)).toBeDefined()
    expect(live(ORG)).toBeDefined()
  })

  it("of the earlier takes what it created, the later's updates with them, and never the later's own, a source or an organization", async () => {
    expect(await browser(server, "a").undo(A)).toEqual({
      state: "undone",
      remaining: 0,
    })
    expect(live(result("T1/a"))).toBeUndefined()
    expect(live(result("T1/b"))).toBeUndefined()
    expect(live(result("T1/c"))?.properties.origin).toBe(originOf(B))
    expect(live(SOURCE)).toBeDefined()
    expect(live(ORG)?.properties.address).toEqual({ city: "Leeds" })
    // Each delete was held to the version read.
    const deletes = server.fake.requests.filter(
      (r) => r.method === "DELETE" && r.path.includes("/labresult/")
    )
    expect(deletes.length).toBeGreaterThan(0)
    expect(deletes.every((r) => r.query.get("ifVersion"))).toBe(true)
  })

  it("of the earlier hands a report the later's results still name to the later, so the collector leaves them; the later's Undo then takes both", async () => {
    await browser(server, "a").undo(A)
    // Deleted, the report would take T1/c with it when collected.
    expect(live(report("T1"))?.properties.origin).toBe(originOf(B))
    server.sweep()
    expect(live(result("T1/c"))).toBeDefined()
    expect(live(report("T1"))).toBeDefined()
    expect(await browser(server, "a").undo(B)).toEqual({
      state: "undone",
      remaining: 0,
    })
    expect(live(report("T1"))).toBeUndefined()
    expect(live(result("T1/c"))).toBeUndefined()
  })
})

describe("Undo with no plan to read", () => {
  const CONDITION = "substrate.reamde.dev/clinical/condition"
  const condition: RecordKey = { kind: CONDITION, namespace: "nhs", key: "C1" }
  const p = plan([
    reportRecord("T1", 100),
    resultRecord("T1/a", "T1", "1", 100),
    {
      ...condition,
      mode: "maintain",
      revision: 100,
      properties: { name: "Hay fever", status: "active" },
    },
  ])

  /** A browser that cannot read the import's plan, `after` run once the
   * records pointing at the import have been listed. */
  function planless(id: string, after: () => void = () => {}) {
    const digest = imp(id).plan!
    let listed = false
    return browser(server, "b", {
      fetch: async (input, init) => {
        const url = urlOf(input)
        if (url.includes(`/blobs/${digest}`))
          return new Response(
            JSON.stringify({ error: { code: "internal", message: "down" } }),
            { status: 500, headers: { "Content-Type": "application/json" } }
          )
        const res = await server.fetch(input, init)
        if (!listed && url.includes("referencing")) {
          listed = true
          after()
        }
        return res
      },
    })
  }

  it("still finds and deletes every kind the importer creates, clinical records included", async () => {
    await importPlan(A, p)
    expect(live(condition)).toBeDefined()
    expect(await planless(A).undo(A)).toEqual({
      state: "undone",
      remaining: 0,
    })
    expect(live(condition)).toBeUndefined()
    expect(live(result("T1/a"))).toBeUndefined()
    expect(live(report("T1"))).toBeUndefined()
  })

  it("reads each record again before deleting it, leaving one another import made its own since the list", async () => {
    await importPlan(A, p)
    const out = await planless(A, () =>
      server.fake.change(KIND.labresult, idOf(result("T1/a")), {
        origin: originOf(B),
      })
    ).undo(A)
    expect(out).toEqual({ state: "undone", remaining: 0 })
    expect(live(result("T1/a"))?.properties.origin).toBe(originOf(B))
    // Its report goes to the same import rather than taking it along.
    expect(live(report("T1"))?.properties.origin).toBe(originOf(B))
    expect(live(condition)).toBeUndefined()
  })
})
