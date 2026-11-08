import type { SubstrateRecord } from "@substrate/client"
import { describe, expect, it } from "vitest"
import { RecordWrites } from "./entry"
import { foldRecords } from "./fold"

const kind = "substrate.reamde.dev/template/item"
const server = new Map<string, SubstrateRecord>([
  [
    `${kind}/a`,
    {
      id: "a",
      kind,
      properties: { name: "Milk", note: "2 l" },
      version: 4,
      createdAt: "2026-11-01T10:00:00Z",
      updatedAt: "2026-11-01T10:00:00Z",
    },
  ],
])

describe("foldRecords", () => {
  it("lays waiting writes over the server's records in order", () => {
    const queue = [
      ...RecordWrites.change(kind, "a", { name: "Oat milk", note: null })
        .entries,
      ...RecordWrites.create(kind, "b", { name: "Eggs" }).entries,
      ...RecordWrites.change(kind, "gone", { name: "x" }).entries,
    ]
    const out = foldRecords(server, queue)
    expect(out.get(`${kind}/a`)).toMatchObject({
      version: 4,
      properties: { name: "Oat milk" },
    })
    expect(out.get(`${kind}/b`)).toMatchObject({
      version: 0,
      properties: { name: "Eggs" },
    })
    expect(out.has(`${kind}/gone`)).toBe(false)
    expect(server.get(`${kind}/a`)?.properties.name).toBe("Milk")
  })

  it("removes a deleted record and skips a parked write", () => {
    const [del] = RecordWrites.delete(kind, "a").entries
    expect(foldRecords(server, [del]).has(`${kind}/a`)).toBe(false)
    expect(
      foldRecords(server, [{ ...del, parked: true }]).has(`${kind}/a`)
    ).toBe(true)
  })
})
