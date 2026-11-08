import { SubstrateClient, type SubstrateRecord } from "@substrate/client"
import { describe, expect, it } from "vitest"
import { LiveCollection } from "./live"

const kind = "substrate.reamde.dev/template/item"
const record = (version: number, name: string): SubstrateRecord => ({
  id: "a",
  kind,
  properties: { name },
  version,
  createdAt: "2026-11-01T10:00:00Z",
  updatedAt: "2026-11-01T10:00:00Z",
})
const tick = () => new Promise((r) => setTimeout(r, 0))

describe("LiveCollection, deprecated", () => {
  it("still shows a write laid over by hand, undoes it, and takes the server's answer", async () => {
    const live = new LiveCollection(
      new SubstrateClient("https://substrate.example.com"),
      [kind]
    )
    const undo = live.apply(kind, "a", record(0, "Milk"))
    await tick()
    expect(live.get(kind, "a")?.properties.name).toBe("Milk")
    undo()
    await tick()
    expect(live.get(kind, "a")).toBeUndefined()

    live.apply(kind, "a", record(0, "Milk"))
    live.landed(record(1, "Milk"))
    await tick()
    expect(live.get(kind, "a")?.version).toBe(1)
    live.landed(record(0, "stale"))
    await tick()
    expect(live.get(kind, "a")?.properties.name).toBe("Milk")
  })
})
