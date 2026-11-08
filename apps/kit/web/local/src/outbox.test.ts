import { SubstrateClient } from "@substrate/client"
import { FakeSubstrate, until } from "@substrate/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { Connectivity } from "./connectivity"
import { RecordWrites, type OutboxFile } from "./entry"
import { foldRecords } from "./fold"
import { Outbox, OutboxNotSaved } from "./outbox"
import {
  MemoryStorage,
  openRepositoryStorage,
  type RepositoryStorage,
} from "./storage"

const kind = "substrate.reamde.dev/template/item"

/** A network a test turns off and on, as the browser's events say it. */
function network(): Connectivity & { set(on: boolean): void } {
  let on = true
  const listeners = new Set<(e: "online" | "offline" | "visible") => void>()
  return {
    online: () => on,
    subscribe(fn) {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    set(next) {
      on = next
      for (const fn of listeners) fn(next ? "online" : "offline")
    },
  }
}

let fake: FakeSubstrate
let client: SubstrateClient
let net: ReturnType<typeof network>
let storage: RepositoryStorage
let outboxes: Outbox[]
let n = 0

function open(
  over: Partial<ConstructorParameters<typeof Outbox>[0]> = {}
): Outbox {
  const o = new Outbox({
    client,
    storage,
    connectivity: net,
    backoff: [5],
    random: () => 0.5,
    ...over,
  })
  outboxes.push(o)
  return o
}

beforeEach(async () => {
  fake = new FakeSubstrate()
  vi.stubGlobal("fetch", fake.fetch)
  client = new SubstrateClient(fake.origin, "tok")
  net = network()
  storage = await openRepositoryStorage("test", `outbox-${n++}.example.org`)
  outboxes = []
})

afterEach(() => {
  for (const o of outboxes) o.close()
  storage.close()
  vi.unstubAllGlobals()
})

const settled = (o: Outbox) => () =>
  !o.getSnapshot().flushing && o.getSnapshot().sendable === 0

describe("the outbox", () => {
  it("keeps a create made offline across a reload and sends it when the network is back", async () => {
    net.set(false)
    const first = open()
    await first.write(RecordWrites.create(kind, "a1", { name: "Milk" }))
    expect(first.getSnapshot().waiting).toBe(1)
    first.close()

    const second = open()
    await second.ready
    expect(second.getSnapshot().entries.map((e) => e.recordID)).toEqual(["a1"])
    await second.flush()
    expect(second.getSnapshot().state).toEqual({
      type: "offline",
      words: "You're offline.",
    })

    const landed: string[] = []
    second.onLanded((e, r) => landed.push(`${e.method} ${r?.version}`))
    net.set(true)
    await until(settled(second), 2000, "the create to land")
    expect(fake.writes()).toEqual([`PUT ${kind}/a1`])
    expect(fake.requests[0].body).toEqual({
      properties: { name: "Milk" },
      ifVersion: 0,
    })
    expect(landed).toEqual(["PUT 1"])
    expect(second.getSnapshot()).toMatchObject({
      waiting: 0,
      state: { type: "sent" },
    })
  })

  it("lands a create whose earlier try landed with its answer lost on the same row", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    const o = open()
    await o.write(
      RecordWrites.create(kind, "a1", { name: "Milk", note: "2 l" })
    )
    await until(settled(o))
    expect(fake.writes()).toEqual([`PUT ${kind}/a1`, `PUT ${kind}/a1`])
    expect(fake.requests[1].body).toEqual({
      properties: { name: "Milk", note: "2 l" },
    })
    expect(fake.live(kind, "a1")?.properties).toEqual({
      name: "Milk",
      note: "2 l",
    })
    expect(o.getSnapshot().parked).toEqual([])
  })

  it("parks an edit on a version another device moved, and keeps the owner's choice", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    fake.change(kind, "a1", { name: "Oat milk" })
    const o = open()
    await o.write(RecordWrites.edit(kind, "a1", { name: "Whole milk" }, 1))
    await until(
      () => o.getSnapshot().parked.length === 1,
      2000,
      "the conflict to park"
    )
    const [parked] = o.getSnapshot().parked
    expect(parked).toMatchObject({ conflict: true, attempts: 1 })
    expect(parked.lastError).toMatch(
      /changed on your substrate since you began editing/
    )
    expect(o.getSnapshot().state.type).toBe("refused")
    // While it waits for the owner, the record shows as the server holds it.
    const server = new Map([[`${kind}/a1`, fake.live(kind, "a1")!]])
    expect(
      foldRecords(server, o.getSnapshot().entries).get(`${kind}/a1`)?.properties
        .name
    ).toBe("Oat milk")

    await o.resolve(parked.id, true)
    await until(settled(o))
    expect(fake.live(kind, "a1")?.properties.name).toBe("Whole milk")
    expect(fake.requests.at(-1)?.body).toEqual({
      properties: { name: "Whole milk" },
    })
  })

  it("sends a second edit made on the same version after the first, not against it", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    net.set(false)
    const o = open()
    await o.write(RecordWrites.edit(kind, "a1", { name: "Oat milk" }, 1))
    await o.write(RecordWrites.edit(kind, "a1", { note: "2 l" }, 1))
    net.set(true)
    await until(settled(o))
    expect(o.getSnapshot().parked).toEqual([])
    expect(
      fake.requests.map((r) => (r.body as { ifVersion?: number })?.ifVersion)
    ).toEqual([1, 2])
    expect(fake.live(kind, "a1")?.properties).toEqual({
      name: "Oat milk",
      note: "2 l",
    })
  })

  it("sends a change made in passing on top of a record that moved since", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    fake.change(kind, "a1", { note: "2 l" })
    const o = open({ versionOf: () => 1 })
    await o.write(RecordWrites.change(kind, "a1", { done: true }))
    await until(settled(o))
    expect(fake.live(kind, "a1")?.properties).toEqual({
      name: "Milk",
      note: "2 l",
      done: true,
    })
    expect(
      fake.requests.map(
        (r) =>
          `${r.method} ${(r.body as { ifVersion?: number })?.ifVersion ?? ""}`
      )
    ).toEqual(["PATCH 1", "GET ", "PATCH 2"])
  })

  it("waits out a busy server for as long as it asks", async () => {
    fake.fail({ method: "PUT" }, 503, undefined, { "Retry-After": "30" })
    const o = open()
    const before = Date.now()
    await o.write(RecordWrites.create(kind, "a1", { name: "Milk" }))
    await until(() => o.getSnapshot().state.type === "offline")
    const state = o.getSnapshot().state
    expect(
      state.type === "offline" && state.retryAt! - before
    ).toBeGreaterThanOrEqual(30_000)
    expect(o.getSnapshot().waiting).toBe(1)
    await o.sendNow()
    expect(o.getSnapshot()).toMatchObject({
      waiting: 0,
      state: { type: "sent" },
    })
  })

  it("tries again with backoff after the network fails mid-send", async () => {
    fake.offline = true
    const o = open()
    await o.write(RecordWrites.create(kind, "a1", { name: "Milk" }))
    await until(() => o.getSnapshot().state.type === "offline")
    fake.offline = false
    await until(settled(o), 2000, "the backoff's next try")
    expect(fake.live(kind, "a1")).toBeDefined()
  })

  it("stops on a refused token and keeps what waits", async () => {
    fake.fail({ method: "PUT" }, 401, {
      error: { code: "auth", message: "expired" },
    })
    const o = open()
    await o.write(RecordWrites.create(kind, "a1", { name: "Milk" }))
    await until(() => o.getSnapshot().state.type === "signedOut")
    expect(o.getSnapshot().waiting).toBe(1)
    expect(o.getSnapshot().parked).toEqual([])
  })

  it("parks a validation refusal, holds its record's later writes, and sends the rest", async () => {
    fake.fail({ method: "PUT", path: /\/a1$/ }, 422, {
      error: { code: "validation", message: "name is required" },
    })
    net.set(false)
    const o = open()
    await o.write(RecordWrites.create(kind, "a1", {}))
    await o.write(RecordWrites.change(kind, "a1", { name: "x" }))
    await o.write(RecordWrites.create(kind, "b2", { name: "Eggs" }))
    net.set(true)
    await until(() => o.getSnapshot().state.type === "refused")
    expect(fake.writes()).toEqual([`PUT ${kind}/a1`, `PUT ${kind}/b2`])
    expect(o.getSnapshot()).toMatchObject({ waiting: 2, sendable: 0 })
    expect(o.getSnapshot().parked[0].lastError).toBe("name is required")

    await o.discard(o.getSnapshot().parked[0].id)
    expect(o.getSnapshot().waiting).toBe(0)
  })

  it("lets a delete replace the record's writes still waiting", async () => {
    net.set(false)
    const o = open()
    await o.write(RecordWrites.create(kind, "a1", { name: "Milk" }))
    await o.write(RecordWrites.change(kind, "a1", { name: "Oat milk" }))
    await o.write(RecordWrites.delete(kind, "a1"))
    expect(o.getSnapshot().entries.map((e) => e.method)).toEqual(["DELETE"])
    net.set(true)
    await until(settled(o))
    // Never created, so already gone: done.
    expect(fake.writes()).toEqual([`DELETE ${kind}/a1`])
    expect(o.getSnapshot().waiting).toBe(0)
  })

  it("drops a write a newer one already overtook", async () => {
    fake.seed(kind, "a1", { name: "Milk" })
    fake.fail({ method: "PATCH" }, 422, {
      error: {
        code: "validation",
        message: "revision 3 is below the stored revision 4",
      },
    })
    const o = open()
    await o.write(RecordWrites.change(kind, "a1", { name: "x" }))
    await until(settled(o))
    expect(o.getSnapshot()).toMatchObject({ waiting: 0, parked: [] })
  })

  it("sends a write queued while a send runs", async () => {
    const o = open()
    await o.write(RecordWrites.create(kind, "a1", { name: "Milk" }))
    await o.write(RecordWrites.create(kind, "b2", { name: "Eggs" }))
    await until(settled(o))
    expect(fake.writes().sort()).toEqual([`PUT ${kind}/a1`, `PUT ${kind}/b2`])
  })

  it("refuses a write it could not keep, and says why", async () => {
    const failing = new MemoryStorage()
    failing.updateOutbox = () => Promise.reject(new Error("QuotaExceededError"))
    const o = open({ storage: failing })
    await expect(
      o.write(RecordWrites.create(kind, "a1", {}))
    ).rejects.toBeInstanceOf(OutboxNotSaved)
    expect(o.getSnapshot()).toMatchObject({
      waiting: 0,
      saveProblem: "QuotaExceededError",
    })
  })

  it("keeps two tabs' writes, each saved over what the other kept", async () => {
    net.set(false)
    const a = open()
    const b = open()
    await Promise.all([a.ready, b.ready])
    await a.write(RecordWrites.create(kind, "a1", { name: "Milk" }))
    await b.write(RecordWrites.create(kind, "b2", { name: "Eggs" }))
    const kept: OutboxFile = await storage.readOutbox()
    expect(kept.entries.map((e) => e.recordID)).toEqual(["a1", "b2"])
  })
})
