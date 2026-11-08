import type { SubstrateRecord } from "@substrate/client"
import { describe, expect, it } from "vitest"
import { RecordCache } from "./cache"
import { openRepositoryStorage } from "./storage"

const kind = "substrate.reamde.dev/template/item"
const other = "substrate.reamde.dev/template/note"
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
})
