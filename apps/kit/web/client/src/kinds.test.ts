import { describe, expect, it } from "vitest"
import { defineKind, parseKind } from "./kinds"
import { stringProp, type SubstrateRecord } from "./records"

const record = (properties: Record<string, unknown>): SubstrateRecord => ({
  id: "0190a1b2-0000-7000-8000-000000000001",
  kind: "substrate.reamde.dev/template/item",
  properties,
  version: 3,
  createdAt: "2026-11-01T10:00:00Z",
  updatedAt: "2026-11-01T10:00:00Z",
})

describe("defineKind", () => {
  const Item = defineKind("substrate.reamde.dev/template/item", {
    read: (r) => ({ id: r.id, name: stringProp(r, "name") ?? "" }),
    write: (d: Partial<{ name: string; note: string | null }>) => {
      const out: Record<string, unknown> = {}
      if (d.name !== undefined) out.name = d.name.trim()
      if (d.note !== undefined) out.note = d.note
      return out
    },
  })

  it("reads and writes through the app's codec", () => {
    expect(Item.read(record({ name: "Milk" }))).toEqual({
      id: "0190a1b2-0000-7000-8000-000000000001",
      name: "Milk",
    })
    expect(Item.write({ name: "  Eggs " })).toEqual({ name: "Eggs" })
    // A field the draft leaves out is not written; null clears one.
    expect(Item.write({ note: null })).toEqual({ note: null })
    expect(Item.is(record({}))).toBe(true)
    expect(Item.ref("x")).toBe("substrate.reamde.dev/template/item/x")
  })

  it("throws for a write of a kind that declares none", () => {
    const ReadOnly = defineKind<string, { name: string }>(
      "substrate.reamde.dev/template/item",
      { read: (r) => r.id }
    )
    expect(() => ReadOnly.write({})).toThrow(/declares no write/)
  })

  it("refuses a kind not named in full", () => {
    expect(() => defineKind("item", { read: () => 0 })).toThrow(/named in full/)
    expect(parseKind("tasks/task")).toBeUndefined()
    expect(parseKind("example/tasks/task")).toBeUndefined()
    expect(parseKind("ada.example.com/tasks/task")).toEqual({
      authority: "ada.example.com",
      package: "tasks",
      name: "task",
    })
    expect(parseKind("~x/tasks/task")).toBeUndefined()
  })

  it("takes a kind of the repository's own, under ~", () => {
    expect(parseKind("~/tasks/task")).toEqual({
      authority: "~",
      package: "tasks",
      name: "task",
    })
    expect(defineKind("~/tasks/task", { read: (r) => r.id }).kind).toBe(
      "~/tasks/task"
    )
  })
})
