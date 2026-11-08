import { SubstrateClient } from "@substrate/client"
import {
  LinkedRecords,
  MemoryStorage,
  Outbox,
  RecordCache,
  steadyConnectivity,
} from "@substrate/local"
import { FakeSubstrate, until } from "@substrate/testing"
import { act, cleanup, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SubstrateContext, type SubstrateContextValue } from "./context"
import type { LiveCollection } from "./live"
import { ToastHost } from "./toast"
import { useWrites } from "./writes"

const kind = "substrate.reamde.dev/template/item"
let fake: FakeSubstrate
let outbox: Outbox

function wrapper(storage = new MemoryStorage()) {
  const client = new SubstrateClient(fake.origin, "tok")
  outbox = new Outbox({ client, storage, connectivity: steadyConnectivity })
  const linked = new LinkedRecords({
    client,
    kinds: [kind],
    outbox,
    connectivity: steadyConnectivity,
  })
  const value = {
    client,
    linked,
    collection: linked as LiveCollection,
    outbox,
    cache: new RecordCache(storage),
    storage,
  } as unknown as SubstrateContextValue
  return ({ children }: { children: ReactNode }) => (
    <ToastHost>
      <SubstrateContext.Provider value={value}>
        {children}
      </SubstrateContext.Provider>
    </ToastHost>
  )
}

beforeEach(() => {
  fake = new FakeSubstrate()
  vi.stubGlobal("fetch", fake.fetch)
})
afterEach(() => {
  outbox.close()
  cleanup()
  vi.unstubAllGlobals()
})

describe("useWrites", () => {
  it("resolves once the outbox keeps a write, with the record as it now shows", async () => {
    const { result } = renderHook(() => useWrites(), { wrapper: wrapper() })
    let created: Awaited<ReturnType<typeof result.current.create>> = null
    await act(async () => {
      created = await result.current.create(kind, "a1", { name: "Milk" })
    })
    expect(created).toMatchObject({
      id: "a1",
      version: 0,
      properties: { name: "Milk" },
    })
    await until(() => outbox.getSnapshot().waiting === 0)
    expect(fake.live(kind, "a1")?.properties.name).toBe("Milk")
  })

  it("hands the words of a write it could not keep to the caller", async () => {
    const storage = new MemoryStorage()
    storage.updateOutbox = () => Promise.reject(new Error("the disk is full"))
    const { result } = renderHook(() => useWrites(), {
      wrapper: wrapper(storage),
    })
    const notKept = vi.fn()
    let created: unknown = "unset"
    await act(async () => {
      created = await result.current.create(
        kind,
        "a1",
        { name: "Milk" },
        { onNotKept: notKept }
      )
    })
    expect(created).toBeNull()
    expect(notKept).toHaveBeenCalledWith(
      "The change could not be kept in this browser: the disk is full"
    )
  })
})
