import { SubstrateClient } from "@substrate/client"
import { FakeSubstrate, until } from "@substrate/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { RecordCache } from "./cache"
import { steadyConnectivity } from "./connectivity"
import { RecordWrites } from "./entry"
import { LinkedRecords, type LinkedSnapshot } from "./linked"
import { Outbox } from "./outbox"
import { openRepositoryStorage, type RepositoryStorage } from "./storage"

const kind = "substrate.reamde.dev/template/item"
const key = (id: string) => `${kind}/${id}`

let fake: FakeSubstrate
let client: SubstrateClient
let storage: RepositoryStorage
let stops: (() => void)[]
let n = 0

beforeEach(async () => {
  fake = new FakeSubstrate()
  vi.stubGlobal("fetch", fake.fetch)
  client = new SubstrateClient(fake.origin, "tok")
  storage = await openRepositoryStorage("test", `linked-${n++}.example.org`)
  stops = []
})

afterEach(() => {
  for (const stop of stops) stop()
  storage.close()
  vi.unstubAllGlobals()
})

function link(
  over: { cache?: RecordCache; outbox?: Outbox } = {}
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
})
