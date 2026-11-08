import type { SubstrateRecord } from "@substrate/client"
import { describe, expect, it } from "vitest"
import { RecordCache } from "./cache"
import { openRepositoryStorage, type RepositoryStorage } from "./storage"

const kind = "substrate.reamde.dev/template/item"
const other = "substrate.reamde.dev/template/note"
/** Storage whose next record save fails a moment after it is asked, as a
 * full disk does. */
function failingOnce(storage: RepositoryStorage): RepositoryStorage {
  const flaky = Object.create(storage) as RepositoryStorage
  let fail = true
  flaky.writeRecords = async (change) => {
    if (!fail) return storage.writeRecords(change)
    fail = false
    await new Promise((r) => setTimeout(r, 10))
    throw new Error("QuotaExceededError")
  }
  return flaky
}

const record = (k: string, id: string, version = 1): SubstrateRecord => ({
  id,
  kind: k,
  properties: { name: id },
  version,
  createdAt: "2026-11-01T10:00:00Z",
  updatedAt: "2026-11-01T10:00:00Z",
})

describe("the record cache", () => {
  it("keeps the cursor with the records and the rows it moved past", async () => {
    const storage = await openRepositoryStorage("test", "cache-1.example.org")
    const cache = new RecordCache(storage, 0)
    cache.listed([record(kind, "a"), record(kind, "b")], [kind], {
      seq: 10,
      generation: "g1",
    })
    cache.row({ seq: 11, kind, recordId: "a", op: "patch", removes: false })
    cache.row({ seq: 12, kind, recordId: "b", op: "delete", removes: true })
    await cache.save()

    const again = new RecordCache(storage, 0)
    const kept = await again.load([kind])
    expect(kept.records.map((r) => r.id)).toEqual(["a"])
    expect(kept.cursor).toEqual({ seq: 12, generation: "g1" })
    expect(kept.stale).toEqual([`${kind}/a`])

    again.refreshed(`${kind}/a`, record(kind, "a", 2))
    await again.save()
    const third = await new RecordCache(storage, 0).load([kind])
    expect(third.stale).toEqual([])
    expect(third.records[0].version).toBe(2)
    storage.close()
  })

  it("replaces a kind's records on a list and leaves another kind's", async () => {
    const storage = await openRepositoryStorage("test", "cache-2.example.org")
    const cache = new RecordCache(storage, 0)
    cache.listed(
      [record(kind, "a"), record(other, "n")],
      [kind, other],
      undefined
    )
    await cache.save()
    cache.listed([record(kind, "b")], [kind], { seq: 3, generation: "g1" })
    await cache.save()
    const kept = await storage.readRecords()
    expect(
      kept.records.map((r) => `${r.kind.split("/")[2]}/${r.id}`).sort()
    ).toEqual(["item/b", "note/n"])
    expect(
      (await new RecordCache(storage, 0).load([kind])).records.map((r) => r.id)
    ).toEqual(["b"])
    storage.close()
  })

  it("forgets the cursor on a relist and everything on a clear", async () => {
    const storage = await openRepositoryStorage("test", "cache-3.example.org")
    const cache = new RecordCache(storage, 0)
    cache.listed([record(kind, "a")], [kind], { seq: 3, generation: "g1" })
    cache.relist()
    await cache.save()
    expect((await storage.readRecords()).cursor).toBeUndefined()
    await cache.clear()
    expect((await storage.readRecords()).records).toEqual([])
    storage.close()
  })

  it("keeps a save that failed for the next, so the cursor never passes it", async () => {
    const storage = await openRepositoryStorage("test", "cache-4.example.org")
    const cache = new RecordCache(failingOnce(storage), 0)
    cache.listed([record(kind, "a"), record(kind, "b")], [kind], {
      seq: 10,
      generation: "g1",
    })
    await cache.save()
    expect(cache.saveProblem).toBe("QuotaExceededError")
    cache.row({ seq: 11, kind, recordId: "a", op: "delete", removes: true })
    await cache.save()
    expect(cache.saveProblem).toBeUndefined()

    const kept = await new RecordCache(storage, 0).load([kind])
    expect(kept.records.map((r) => r.id)).toEqual(["b"])
    expect(kept.cursor).toEqual({ seq: 11, generation: "g1" })
    storage.close()
  })

  it("puts back a failed save under what was listed since", async () => {
    const storage = await openRepositoryStorage("test", "cache-5.example.org")
    const cache = new RecordCache(failingOnce(storage), 0)
    cache.listed([record(kind, "a")], [kind], { seq: 3, generation: "g1" })
    cache.refreshed(`${kind}/a`, record(kind, "a", 2))
    const failing = cache.save()
    await new Promise((r) => setTimeout(r, 0))
    // Listed again while the failing save is on its way.
    cache.listed([record(kind, "b")], [kind], { seq: 5, generation: "g1" })
    await failing
    expect(cache.saveProblem).toBe("QuotaExceededError")
    await cache.save()

    const kept = await new RecordCache(storage, 0).load([kind])
    expect(kept.records.map((r) => r.id)).toEqual(["b"])
    expect(kept.cursor).toEqual({ seq: 5, generation: "g1" })
    storage.close()
  })

  it("resumes only over the kinds it listed, and shows those it holds", async () => {
    const storage = await openRepositoryStorage("test", "cache-6.example.org")
    const cache = new RecordCache(storage, 0)
    cache.listed([record(kind, "a")], [kind], { seq: 3, generation: "g1" })
    await cache.save()

    const narrower = await new RecordCache(storage, 0).load([kind])
    expect(narrower.cursor).toEqual({ seq: 3, generation: "g1" })

    const wider = await new RecordCache(storage, 0).load([kind, other])
    expect(wider.cursor).toBeUndefined()
    expect(wider.kinds).toEqual([kind])
    expect(wider.records.map((r) => r.id)).toEqual(["a"])
    storage.close()
  })
})
