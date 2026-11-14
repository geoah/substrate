import { SubstrateClient } from "@substrate/client"
import { FakeSubstrate, until } from "@substrate/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { RecordCache } from "./cache"
import { steadyConnectivity, type Connectivity } from "./connectivity"
import { RecordWrites } from "./entry"
import { LinkedRecords, type LinkedSnapshot } from "./linked"
import { Outbox } from "./outbox"
import { openRepositoryStorage, type RepositoryStorage } from "./storage"

const kind = "substrate.reamde.dev/template/item"
const note = "substrate.reamde.dev/template/note"
const key = (id: string) => `${kind}/${id}`

/** Requests whose answers wait until `open`, each answered as the server
 * stood when it was asked. */
function holding(match: (method: string, path: string) => boolean) {
  let open!: () => void
  const gate = new Promise<void>((r) => (open = r))
  let held = 0
  vi.stubGlobal("fetch", async (input: string, init?: RequestInit) => {
    const res = await fake.fetch(input, init)
    if (match(init?.method ?? "GET", new URL(input).pathname)) {
      held++
      await gate
    }
    return res
  })
  return { open, held: () => held }
}

/** A network whose page a test brings to the front. */
function front(): Connectivity & { show(): void } {
  const listeners = new Set<(e: "online" | "offline" | "visible") => void>()
  return {
    online: () => true,
    subscribe(fn) {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    show() {
      for (const fn of listeners) fn("visible")
    },
  }
}

const tick = (ms = 30) => new Promise((r) => setTimeout(r, ms))

/** The `after` of every list the fake was asked, in order. */
const lists = () =>
  fake.requests
    .filter((r) => r.path === "/api/v1/records" && r.method === "GET")
    .map((r) => r.query.get("after"))

let fake: FakeSubstrate
let client: SubstrateClient
let storage: RepositoryStorage
let stops: (() => unknown)[]
let n = 0

beforeEach(async () => {
  fake = new FakeSubstrate()
  vi.stubGlobal("fetch", fake.fetch)
  client = new SubstrateClient(fake.origin, "tok")
  storage = await openRepositoryStorage("test", `linked-${n++}.example.org`)
  stops = []
})

afterEach(async () => {
  for (const stop of stops) await stop()
  storage.close()
  vi.unstubAllGlobals()
})

function link(
  over: {
    cache?: RecordCache
    outbox?: Outbox
    kinds?: string[]
    connectivity?: Connectivity
    limit?: number
    readRetryDelays?: number[]
    now?: () => number
  } = {}
): LinkedRecords {
  const linked = new LinkedRecords({
    client,
    kinds: [kind],
    connectivity: steadyConnectivity,
    kept: "items",
    follow: { pauses: [5] },
    ...over,
  })
  linked.start()
  stops.push(() => linked.stop())
  return linked
}

const name = (s: LinkedSnapshot, id: string) =>
  s.records.get(key(id))?.properties.name

describe("the linked records", () => {
  it("lists after the watch's bookmark and follows the feed from it", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const linked = link()
    await until(() => linked.getSnapshot().status === "live", 2000, "live")
    expect(name(linked.getSnapshot(), "a1")).toBe("Milk")
    const [watch, list] = fake.requests
    expect(watch.path).toBe("/api/v1/changes")
    expect(watch.query.get("from")).toBeNull()
    expect(list.path).toBe("/api/v1/records")

    fake.change(kind, "a1", { name: "Oat milk" })
    await until(
      () => name(linked.getSnapshot(), "a1") === "Oat milk",
      2000,
      "the change"
    )
  })

  it("reads again a record that changed while the list was read", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    vi.stubGlobal("fetch", async (input: string, init?: RequestInit) => {
      const res = await fake.fetch(input, init)
      // Answered as the list stood, then changed before the answer arrives.
      if (new URL(input).pathname === "/api/v1/records")
        fake.change(kind, "a1", { name: "Oat milk" })
      return res
    })
    const linked = link()
    await until(
      () => name(linked.getSnapshot(), "a1") === "Oat milk",
      2000,
      "the reread"
    )
    expect(linked.getSnapshot().records.get(key("a1"))?.version).toBe(2)
  })

  it("opens offline with what it kept, and says it is reconnecting", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const cache = new RecordCache(storage, 0)
    const first = link({ cache })
    await until(() => first.getSnapshot().status === "live")
    first.stop()
    await cache.save()

    fake.offline = true
    const second = link({ cache: new RecordCache(storage, 0) })
    await until(
      () => second.getSnapshot().status === "reconnecting",
      2000,
      "reconnecting"
    )
    const s = second.getSnapshot()
    expect(name(s, "a1")).toBe("Milk")
    expect(s.cached).toBe(true)
    expect(s.words).toBe(
      "Offline. Changes will show when the connection is back."
    )
  })

  it("resumes from the kept cursor, reading what changed meanwhile", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const cache = new RecordCache(storage, 0)
    const first = link({ cache })
    await until(() => first.getSnapshot().status === "live")
    first.stop()
    await cache.save()
    fake.change(kind, "a1", { name: "Oat milk" })
    const head = fake.head

    fake.requests.length = 0
    const second = link({ cache: new RecordCache(storage, 0) })
    await until(
      () => name(second.getSnapshot(), "a1") === "Oat milk",
      2000,
      "the change"
    )
    expect(fake.requests[0].query.get("from")).toBe(String(head - 1))
    expect(fake.requests.some((r) => r.path === "/api/v1/records")).toBe(false)
  })

  it("reads everything again when the cursor no longer addresses the history", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const cache = new RecordCache(storage, 0)
    const first = link({ cache })
    await until(() => first.getSnapshot().status === "live")
    first.stop()
    await cache.save()
    fake.compact()
    fake.seed(kind, "b2", { name: "Eggs" })

    const second = link({ cache: new RecordCache(storage, 0) })
    await until(
      () => name(second.getSnapshot(), "b2") === "Eggs",
      2000,
      "the relist"
    )
    expect(second.getSnapshot().status).toBe("live")
  })

  it("reads again from the first page when a list's cursor is refused part way, and says nothing", async () => {
    for (let i = 0; i < 520; i++) fake.seed(kind, `r${i}`, { name: `n${i}` })
    fake.refuseCursors(1)
    const linked = link()
    await until(() => linked.getSnapshot().status === "live", 4000, "live")
    const snap = linked.getSnapshot()
    expect(snap.records.size).toBe(520)
    expect(snap.words).toBeUndefined()
    expect(lists()).toEqual([null, "500", null, "500"])
  })

  it("says a list refused again after reading from the first page has failed, and tries no more on its own", async () => {
    for (let i = 0; i < 520; i++) fake.seed(kind, `r${i}`, { name: `n${i}` })
    fake.refuseCursors(2)
    const linked = link()
    await until(
      () => linked.getSnapshot().status === "error",
      4000,
      "the failure"
    )
    expect(linked.getSnapshot().words).toMatch(/not being updated/)
    await tick(100)
    expect(lists()).toEqual([null, "500", null, "500"])
  })

  it("says a watch refused again right after it read everything again has failed, rather than read again", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const cache = new RecordCache(storage, 0)
    const first = link({ cache })
    await until(() => first.getSnapshot().status === "live")
    first.stop()
    await cache.save()
    const watches = () =>
      fake.requests.filter((r) => r.path === "/api/v1/changes").length
    const before = watches()
    fake.fail(
      { path: /^\/api\/v1\/changes$/ },
      410,
      {
        error: {
          code: "compacted",
          message: "the cursor addresses another history",
        },
      },
      {},
      2
    )

    const second = link({ cache: new RecordCache(storage, 0) })
    await until(
      () => second.getSnapshot().status === "error",
      2000,
      "the failure"
    )
    expect(second.getSnapshot().words).toMatch(/could not resume/)
    await tick(100)
    expect(watches() - before).toBe(2)
  })

  // Rule W, one recovery a minute: the clock the allowance reads is the
  // test's, and the history moves twice, the follow refused at each
  // reconnect.
  const recoveries = async (secondAfter: number) => {
    fake.seed(kind, "a1", { name: "Milk" })
    let clock = 1_000_000
    const linked = link({ now: () => clock })
    await until(() => linked.getSnapshot().status === "live", 2000, "live")
    const listed = lists().length
    fake.compact()
    fake.dropWatches()
    await until(
      () => lists().length > listed && linked.getSnapshot().status === "live",
      2000,
      "the first recovery"
    )
    // The recovery's watch opened at the head its read gave, with a
    // bookmark, its list landed and it delivered a change: nothing of that
    // makes the allowance whole.
    await until(() => fake.openWatches === 1, 2000, "the reopened watch")
    fake.change(kind, "a1", { name: "Oat milk" })
    await until(
      () => name(linked.getSnapshot(), "a1") === "Oat milk",
      2000,
      "the delivered change"
    )
    const recovered = lists().length
    clock += secondAfter
    fake.compact()
    fake.dropWatches()
    return { linked, recovered }
  }

  it("says a watch refused again 59,999 ms after its last recovery began has failed, though a bookmark, a list and a change came between", async () => {
    const { linked, recovered } = await recoveries(59_999)
    await until(
      () => linked.getSnapshot().status === "error",
      2000,
      "the failure"
    )
    expect(linked.getSnapshot().words).toMatch(/could not resume/)
    await tick(100)
    expect(lists().length).toBe(recovered)
  })

  it("recovers again from a refusal 60,000 ms after its last recovery began", async () => {
    const { linked, recovered } = await recoveries(60_000)
    await until(
      () =>
        lists().length > recovered && linked.getSnapshot().status === "live",
      2000,
      "the second recovery"
    )
    expect(linked.getSnapshot().words).toBeUndefined()
  })

  it("shows a write at once and keeps showing it until the outbox says it landed", async () => {
    const outbox = new Outbox({
      client,
      storage,
      connectivity: steadyConnectivity,
    })
    stops.push(() => outbox.close())
    const linked = link({ outbox })
    await until(() => linked.getSnapshot().status === "live")
    const seen: LinkedSnapshot[] = []
    linked.subscribe(() => seen.push(linked.getSnapshot()))

    await outbox.write(RecordWrites.create(kind, "a1", { name: "Milk" }))
    await until(
      () => outbox.getSnapshot().waiting === 0,
      2000,
      "the write to land"
    )
    await until(() => !linked.getSnapshot().pending.size)
    const shown = seen.filter((s) => s.records.has(key("a1")))
    expect(shown[0].pending.has(key("a1"))).toBe(true)
    expect(shown[0].records.get(key("a1"))?.version).toBe(0)
    // From the first time it showed, it never left.
    expect(
      seen.slice(seen.indexOf(shown[0])).every((s) => s.records.has(key("a1")))
    ).toBe(true)
    expect(linked.getSnapshot().records.get(key("a1"))?.version).toBe(1)
  })

  it("shows a record as the server holds it while its write is refused", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    fake.change(kind, "a1", { name: "Oat milk" })
    const outbox = new Outbox({
      client,
      storage,
      connectivity: steadyConnectivity,
    })
    stops.push(() => outbox.close())
    const linked = link({ outbox })
    await until(() => linked.getSnapshot().status === "live")
    await outbox.write(RecordWrites.edit(kind, "a1", { name: "Whole milk" }, 1))
    await until(
      () => linked.getSnapshot().parked.has(key("a1")),
      2000,
      "the refusal"
    )
    expect(name(linked.getSnapshot(), "a1")).toBe("Oat milk")
    expect(linked.getSnapshot().pending.size).toBe(0)
  })

  it("says the end of its access", async () => {
    fake.fail({ path: /changes/ }, 401, {
      error: { code: "auth", message: "expired" },
    })
    const linked = link()
    await until(
      () => linked.getSnapshot().status === "signedOut",
      2000,
      "signed out"
    )
  })

  it("never brings back a record deleted while it was read", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const linked = link()
    await until(() => linked.statusSnapshot().status === "live")
    const read = holding(
      (method, path) => method === "GET" && path.endsWith("/a1")
    )
    fake.change(kind, "a1", { name: "Oat milk" })
    await until(() => read.held() === 1, 2000, "the read")
    await client.delete(kind, "a1")
    await until(() => !linked.recordSnapshot(kind, "a1"), 2000, "the delete")
    read.open()
    await tick()
    expect(linked.recordSnapshot(kind, "a1")).toBeUndefined()
  })

  it("never lays a read older than a write that landed over it", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const outbox = new Outbox({
      client,
      storage,
      connectivity: steadyConnectivity,
    })
    stops.push(() => outbox.close())
    const linked = link({ outbox })
    await until(() => linked.statusSnapshot().status === "live")
    const read = holding(
      (method, path) => method === "GET" && path.endsWith("/a1")
    )
    fake.change(kind, "a1", { name: "Oat milk" })
    await until(() => read.held() === 1, 2000, "the read")
    // The feed goes quiet, so nothing reads the landed write again.
    fake.fail({ path: /changes/ }, 503, undefined, {}, 1000)
    fake.dropWatches()
    await outbox.write(RecordWrites.edit(kind, "a1", { name: "Whole milk" }, 2))
    await until(() => outbox.getSnapshot().waiting === 0, 2000, "the write")
    read.open()
    await tick()
    expect(linked.recordSnapshot(kind, "a1")).toMatchObject({
      version: 3,
      properties: { name: "Whole milk" },
    })
  })

  it("reads again a kind that joins the ones it kept", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    fake.seed(note, "n1", { name: "Shop" })
    const cache = new RecordCache(storage, 0)
    const first = link({ cache })
    await until(() => first.statusSnapshot().status === "live")
    first.stop()
    await cache.close()

    fake.requests.length = 0
    const list = holding((_method, path) => path === "/api/v1/records")
    const second = link({
      cache: new RecordCache(storage, 0),
      kinds: [kind, note],
    })
    await until(() => second.hasListed(kind), 2000, "the kept records")
    expect(second.hasListed(note)).toBe(false)
    expect(second.recordSnapshot(kind, "a1")?.properties.name).toBe("Milk")
    list.open()
    await until(() => second.hasListed(note), 2000, "the list")
    expect(second.recordSnapshot(note, "n1")?.properties.name).toBe("Shop")
    expect(fake.requests[0].query.get("from")).toBeNull()
  })

  it("lets its first list finish when the page comes to the front", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const net = front()
    const list = holding((_method, path) => path === "/api/v1/records")
    const linked = link({ connectivity: net })
    await until(() => list.held() === 1, 2000, "the list")
    net.show()
    net.show()
    list.open()
    await until(() => linked.statusSnapshot().status === "live")
    const paths = fake.requests.map((r) => r.path)
    expect(paths.filter((p) => p === "/api/v1/changes")).toHaveLength(1)
    expect(paths.filter((p) => p === "/api/v1/records")).toHaveLength(1)

    fake.change(kind, "a1", { name: "Oat milk" })
    await until(
      () => linked.recordSnapshot(kind, "a1")?.properties.name === "Oat milk",
      2000,
      "the change"
    )
  })

  it("follows the feed after a reload cuts its list short", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const list = holding(
      (_method, path) =>
        path === "/api/v1/records" &&
        fake.requests.filter((r) => r.path === path).length === 1
    )
    const linked = link()
    await until(() => list.held() === 1, 2000, "the list")
    await linked.reload()
    list.open()
    await until(() => linked.statusSnapshot().status === "live")
    fake.change(kind, "a1", { name: "Oat milk" })
    await until(
      () => linked.recordSnapshot(kind, "a1")?.properties.name === "Oat milk",
      2000,
      "the change"
    )
  })

  it("keeps a kind's map and a record's object through every change of anything else", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    fake.seed(kind, "a2", { name: "Eggs" })
    fake.seed(note, "n1", { name: "Shop" })
    const outbox = new Outbox({
      client,
      storage,
      connectivity: steadyConnectivity,
    })
    stops.push(() => outbox.close())
    const linked = link({ outbox, kinds: [kind, note] })
    await until(() => linked.statusSnapshot().status === "live")
    const notes = linked.kindSnapshot(note)
    const eggs = linked.recordSnapshot(kind, "a2")
    const items = linked.kindSnapshot(kind)
    const status = linked.statusSnapshot()
    expect(linked.kindSnapshot(kind)).toBe(items)

    fake.change(kind, "a1", { name: "Oat milk" })
    await until(
      () => linked.recordSnapshot(kind, "a1")?.properties.name === "Oat milk"
    )
    expect(linked.kindSnapshot(kind)).not.toBe(items)
    expect(linked.kindSnapshot(note)).toBe(notes)
    expect(linked.recordSnapshot(kind, "a2")).toBe(eggs)
    expect(linked.statusSnapshot()).toBe(status)

    await outbox.write(RecordWrites.create(kind, "a3", { name: "Bread" }))
    await until(() => outbox.getSnapshot().waiting === 0)
    await until(() => !linked.statusSnapshot().pending)
    expect(linked.kindSnapshot(note)).toBe(notes)
    expect(linked.recordSnapshot(kind, "a2")).toBe(eggs)
    const settled = linked.kindSnapshot(kind)
    await outbox.sendNow()
    await tick()
    expect(linked.kindSnapshot(kind)).toBe(settled)

    fake.fail({ path: /changes/ }, 503, undefined, {}, 1000)
    fake.dropWatches()
    await until(() => linked.statusSnapshot().status === "reconnecting")
    expect(linked.statusSnapshot()).not.toBe(status)
    expect(linked.kindSnapshot(kind)).toBe(settled)
    expect(linked.kindSnapshot(note)).toBe(notes)
  })

  it("says which records have a write waiting and which one refused", async () => {
    fake.fail({ method: "PUT", path: /\/a1$/ }, 422, {
      error: { code: "validation", message: "name is required" },
    })
    const outbox = new Outbox({
      client,
      storage,
      connectivity: steadyConnectivity,
    })
    stops.push(() => outbox.close())
    const linked = link({ outbox })
    await until(() => linked.statusSnapshot().status === "live")
    await outbox.write(RecordWrites.create(kind, "a1", {}))
    expect(linked.writeState(kind, "a1")).toBe("pending")
    await until(() => linked.writeState(kind, "a1") === "parked")
    expect(linked.statusSnapshot()).toMatchObject({ pending: 0, parked: 1 })
    expect(linked.recordSnapshot(kind, "a1")).toBeUndefined()
  })

  it("drops a record whose create met its row gone since, and a read of it from before", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const cache = new RecordCache(storage, 0)
    const outbox = new Outbox({
      client,
      storage,
      connectivity: steadyConnectivity,
    })
    stops.push(() => outbox.close())
    const linked = link({ outbox, cache })
    await until(() => !!linked.recordSnapshot(kind, "a1"), 2000, "the list")
    // The feed goes quiet, so only the create says the row is gone.
    fake.fail({ path: /changes/ }, 503, undefined, {}, 1000)
    fake.dropWatches()
    let gets = 0
    const read = holding(
      (method, path) => method === "GET" && path.endsWith("/a1") && ++gets === 1
    )
    void linked.refresh(kind, "a1")
    await until(() => read.held() === 1, 2000, "the read")
    await client.delete(kind, "a1")
    // The create's earlier try landed and its answer was lost.
    fake.fail({ method: "PUT" }, 409, {
      error: { code: "conflict", message: "it exists" },
    })
    await outbox.write(RecordWrites.create(kind, "a1", { name: "Milk" }))
    await until(() => outbox.getSnapshot().waiting === 0, 2000, "the create")
    expect(linked.recordSnapshot(kind, "a1")).toBeUndefined()
    read.open()
    await tick()
    expect(linked.recordSnapshot(kind, "a1")).toBeUndefined()
    expect(linked.isAbsent(kind, "a1")).toBe(true)
    await cache.save()
    expect((await storage.readRecords()).records).toEqual([])
  })

  it("reads by itself a record its kind's list cut off at the limit, and keeps the kind read in part", async () => {
    for (const id of ["a1", "a2", "a3"]) fake.seed(kind, id, { name: id })
    const cache = new RecordCache(storage, 0)
    const linked = link({ cache, limit: 2 })
    await until(() => linked.statusSnapshot().status === "live")
    expect(linked.statusSnapshot().limitReached).toBe(2)
    expect(linked.hasListed(kind)).toBe(false)
    expect(linked.recordSnapshot(kind, "a3")).toBeUndefined()
    expect(linked.isAbsent(kind, "a3")).toBe(false)
    expect(linked.isAbsent(kind, "gone")).toBe(false)

    linked.resolve(kind, "a3")
    linked.resolve(kind, "gone")
    await until(
      () =>
        linked.recordSnapshot(kind, "a3")?.properties.name === "a3" &&
        linked.isAbsent(kind, "gone"),
      2000,
      "the reads"
    )
    linked.stop()
    await cache.close()

    const again = link({ cache: new RecordCache(storage, 0), limit: 2 })
    await until(() => !!again.recordSnapshot(kind, "a1"), 2000, "the kept ones")
    expect(again.hasListed(kind)).toBe(false)
    expect(again.isAbsent(kind, "other")).toBe(false)
  })

  it("follows again after a list the page came to the front during, when the follow ended meanwhile", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const net = front()
    const list = holding((_method, path) => path === "/api/v1/records")
    const linked = link({ connectivity: net })
    await until(() => list.held() === 1, 2000, "the list")
    fake.fail({ path: /changes/ }, 400, {
      error: { code: "bad_request", message: "not now" },
    })
    fake.dropWatches()
    await until(() => linked.statusSnapshot().status === "error", 2000, "ended")
    net.show()
    list.open()
    await until(() => fake.openWatches === 1, 2000, "the follow again")
    fake.change(kind, "a1", { name: "Oat milk" })
    await until(
      () => linked.recordSnapshot(kind, "a1")?.properties.name === "Oat milk",
      2000,
      "the change"
    )
    expect(linked.statusSnapshot().status).toBe("live")
  })

  it("forgets a record read absent when the history changes, and finds it past the list's limit", async () => {
    for (const id of ["a1", "a2", "a3"]) fake.seed(kind, id, { name: id })
    const linked = link({ limit: 2 })
    await until(() => linked.statusSnapshot().status === "live")
    linked.resolve(kind, "z")
    await until(() => linked.isAbsent(kind, "z"), 2000, "z read absent")

    // Away, z is made and the history moves on.
    fake.offline = true
    fake.dropWatches()
    await until(() => linked.statusSnapshot().status === "reconnecting")
    fake.seed(kind, "z", { name: "z" })
    fake.compact()
    const heard = vi.fn()
    linked.subscribe(heard)
    fake.offline = false
    await until(
      () =>
        !linked.isAbsent(kind, "z") &&
        linked.statusSnapshot().status === "live",
      2000,
      "the relist"
    )
    expect(heard).toHaveBeenCalled()
    // z is past the new list's limit, so a screen showing it asks again.
    expect(linked.statusSnapshot().limitReached).toBe(2)
    expect(linked.recordSnapshot(kind, "z")).toBeUndefined()
    linked.resolve(kind, "z")
    await until(
      () => linked.recordSnapshot(kind, "z")?.properties.name === "z",
      2000,
      "z found"
    )
  })

  it("keeps no record absent that its kind's whole list already says is", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const linked = link()
    await until(() => linked.statusSnapshot().status === "live")
    await fake.fetch(`${fake.origin}/api/v1/${kind}/a1`, {
      method: "DELETE",
      headers: { Authorization: "Bearer tok" },
    })
    await until(() => !linked.recordSnapshot(kind, "a1"), 2000, "the delete")
    expect(linked.isAbsent(kind, "a1")).toBe(true)
    // Peeked at: what the list says is not kept twice.
    expect(
      (linked as unknown as { absentKeys: Set<string> }).absentKeys.size
    ).toBe(0)
  })

  it("reads again by itself a record whose read failed, while the watch stays open", async () => {
    for (const id of ["a1", "a2", "a3"]) fake.seed(kind, id, { name: id })
    const linked = link({ limit: 2, readRetryDelays: [20] })
    await until(() => linked.statusSnapshot().status === "live")
    fake.fail({ method: "GET", path: /\/a3$/ }, 503)
    linked.resolve(kind, "a3")
    await until(() => !!linked.readError(kind, "a3"), 2000, "the failure")
    expect(linked.isAbsent(kind, "a3")).toBe(false)
    await until(
      () => linked.recordSnapshot(kind, "a3")?.properties.name === "a3",
      2000,
      "the read again"
    )
    expect(linked.readError(kind, "a3")).toBeUndefined()
    expect(fake.openWatches).toBe(1)
    expect(linked.statusSnapshot().status).toBe("live")
  })

  it("stops reading a failed record by itself after its tries, and reads it when asked", async () => {
    for (const id of ["a1", "a2", "a3"]) fake.seed(kind, id, { name: id })
    const linked = link({ limit: 2, readRetryDelays: [10] })
    await until(() => linked.statusSnapshot().status === "live")
    fake.fail({ method: "GET", path: /\/a3$/ }, 503, undefined, {}, 2)
    linked.resolve(kind, "a3")
    const reads = () =>
      fake.requests.filter((r) => r.path.endsWith("/a3")).length
    await until(() => reads() === 2, 2000, "the try again")
    await tick(60)
    expect(reads()).toBe(2)
    expect(linked.readError(kind, "a3")).toContain("Try again soon")
    linked.retryRead(kind, "a3")
    expect(linked.readError(kind, "a3")).toBeUndefined()
    await until(() => !!linked.recordSnapshot(kind, "a3"), 2000, "the read")
  })

  it("counts only the records of kinds cut off at the limit when it opens from what it kept", async () => {
    const at = (k: string, id: string) => ({
      id,
      kind: k,
      properties: {},
      version: 1,
      createdAt: "2026-11-01T10:00:00Z",
      updatedAt: "2026-11-01T10:00:00Z",
    })
    await storage.writeRecords({
      replaceKinds: [kind, note],
      put: [at(kind, "a1"), at(kind, "a2"), at(note, "n1"), at(note, "n2")],
      remove: [],
      cursor: { seq: 4, generation: fake.generation },
      kinds: [kind, note],
      partial: [kind],
      stale: [],
    })
    fake.offline = true
    const linked = link({
      cache: new RecordCache(storage, 0),
      kinds: [kind, note],
    })
    await until(() => linked.statusSnapshot().cached, 2000, "the kept records")
    expect(linked.statusSnapshot().limitReached).toBe(2)
  })

  it("takes another set of kinds with the outbox's waiting writes kept and the records still read shown", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    fake.seed(note, "n1", { name: "Call" })
    const outbox = new Outbox({
      client,
      storage,
      connectivity: steadyConnectivity,
    })
    stops.push(() => outbox.close())
    const cache = new RecordCache(storage, 0)
    const linked = link({ outbox, cache })
    await until(() => linked.getSnapshot().status === "live")
    const milk = linked.recordSnapshot(kind, "a1")
    fake.offline = true
    await outbox.write(RecordWrites.change(note, "n1", { name: "Call Ada" }))
    fake.offline = false

    linked.setKinds([kind, note])
    expect(linked.kinds).toEqual([kind, note])
    // Still shown while the new set is read, the same object.
    expect(linked.recordSnapshot(kind, "a1")).toBe(milk)
    expect(linked.hasListed(note)).toBe(false)
    await until(() => linked.hasListed(note), 2000, "the added kind")
    expect(linked.recordSnapshot(kind, "a1")).toBe(milk)
    await until(
      () => linked.recordSnapshot(note, "n1")?.properties.name === "Call Ada",
      2000,
      "the waiting write, laid over"
    )
    const watch = fake.requests
      .filter((r) => r.path === "/api/v1/changes")
      .at(-1)
    expect(watch?.query.getAll("kinds")).toEqual([kind, note])

    linked.setKinds([note])
    expect(linked.recordSnapshot(kind, "a1")).toBeUndefined()
    expect(linked.hasListed(kind)).toBe(false)
    await until(() => linked.getSnapshot().status === "live")
    fake.change(kind, "a1", { name: "Oat milk" })
    fake.change(note, "n1", { name: "Call Ada now" })
    await until(
      () =>
        linked.recordSnapshot(note, "n1")?.properties.name === "Call Ada now"
    )
    expect(linked.recordSnapshot(kind, "a1")).toBeUndefined()
    await cache.save()
    expect((await storage.readRecords()).records.map((r) => r.kind)).toEqual([
      note,
    ])
  })

  it("does nothing for the same kinds", async () => {
    const linked = link()
    await until(() => linked.getSnapshot().status === "live")
    const asked = fake.requests.length
    linked.setKinds([kind])
    await tick()
    expect(fake.requests.length).toBe(asked)
  })

  it("shows a held write as not made until its moment, then as made", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const outbox = new Outbox({
      client,
      storage,
      connectivity: steadyConnectivity,
    })
    stops.push(() => outbox.close())
    const linked = link({ outbox })
    await until(() => linked.getSnapshot().status === "live")
    fake.offline = true
    await outbox.write(
      RecordWrites.edit(kind, "a1", { name: "Oat milk" }, 1, {
        until: Date.now() + 120,
      })
    )
    await tick()
    expect(linked.recordSnapshot(kind, "a1")?.properties.name).toBe("Milk")
    await until(
      () => linked.recordSnapshot(kind, "a1")?.properties.name === "Oat milk",
      2000,
      "the moment"
    )
  })
})
