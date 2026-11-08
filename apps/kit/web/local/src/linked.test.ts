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
})
