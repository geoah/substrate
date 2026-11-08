import { SubstrateClient, defineKind, stringProp } from "@substrate/client"
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
import { ToastHost } from "./toast"
import { useKindWrites, useWrites, type WriteResult } from "./writes"

const kind = "substrate.reamde.dev/template/item"
const Item = defineKind(kind, {
  read: (r) => ({ id: r.id, name: stringProp(r, "name") ?? "" }),
  write: (draft: Partial<{ name: string }>) => {
    if (draft.name !== undefined && !draft.name.trim())
      throw new Error("An item needs a name.")
    return draft.name === undefined ? {} : { name: draft.name.trim() }
  },
})
let fake: FakeSubstrate
let outbox: Outbox
let linked: LinkedRecords

function wrapper(storage = new MemoryStorage()) {
  const client = new SubstrateClient(fake.origin, "tok")
  outbox = new Outbox({ client, storage, connectivity: steadyConnectivity })
  linked = new LinkedRecords({
    client,
    kinds: [kind],
    outbox,
    connectivity: steadyConnectivity,
  })
  const value = {
    client,
    linked,
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
afterEach(async () => {
  linked.stop()
  await outbox.close()
  cleanup()
  vi.unstubAllGlobals()
})

describe("useWrites", () => {
  it("resolves kept once the outbox has a write, with the record as it now shows", async () => {
    const { result } = renderHook(() => useWrites(), { wrapper: wrapper() })
    let created: WriteResult<unknown> | undefined
    await act(async () => {
      created = await result.current.create(kind, "a1", { name: "Milk" })
    })
    expect(created).toMatchObject({
      kept: true,
      id: "a1",
      shown: { id: "a1", version: 0, properties: { name: "Milk" } },
    })
    await until(() => outbox.getSnapshot().waiting === 0)
    expect(fake.live(kind, "a1")?.properties.name).toBe("Milk")
  })

  it("never invents a record it does not show", async () => {
    const { result } = renderHook(() => useWrites(), { wrapper: wrapper() })
    let changed: WriteResult<unknown> | undefined
    await act(async () => {
      changed = await result.current.change(
        "substrate.reamde.dev/template/other",
        "b1",
        { name: "Bread" }
      )
    })
    expect(changed).toEqual({ kept: true, id: "b1", shown: undefined })
  })

  it("hands the words of a write it could not keep to the caller", async () => {
    const storage = new MemoryStorage()
    storage.updateOutbox = () => Promise.reject(new Error("the disk is full"))
    const { result } = renderHook(() => useWrites(), {
      wrapper: wrapper(storage),
    })
    const notKept = vi.fn()
    let created: WriteResult<unknown> | undefined
    await act(async () => {
      created = await result.current.create(
        kind,
        "a1",
        { name: "Milk" },
        { onNotKept: notKept }
      )
    })
    expect(created).toMatchObject({ kept: false })
    expect(notKept).toHaveBeenCalledWith(
      "The change could not be kept in this browser: the disk is full"
    )
  })
})

describe("useKindWrites", () => {
  it("writes a typed draft and answers the record read through the codec", async () => {
    const { result } = renderHook(() => useKindWrites(Item), {
      wrapper: wrapper(),
    })
    let created: WriteResult<{ id: string; name: string }> | undefined
    await act(async () => {
      created = await result.current.create({ name: " Milk " })
    })
    expect(created?.kept && created.shown).toEqual({
      id: created?.kept ? created.id : "",
      name: "Milk",
    })
    const id = created?.kept ? created.id : ""
    await until(() => outbox.getSnapshot().waiting === 0)
    await act(async () => {
      await result.current.edit(id, { name: "Oat milk" }, 1)
    })
    await until(() => outbox.getSnapshot().waiting === 0)
    expect(fake.live(kind, id)?.properties.name).toBe("Oat milk")
  })

  it("says a codec's refusal as a write not kept, and queues nothing", async () => {
    const { result } = renderHook(() => useKindWrites(Item), {
      wrapper: wrapper(),
    })
    const notKept = vi.fn()
    let changed: WriteResult<unknown> | undefined
    await act(async () => {
      changed = await result.current.change(
        "a1",
        { name: "  " },
        { onNotKept: notKept }
      )
    })
    expect(changed).toEqual({ kept: false, words: "An item needs a name." })
    expect(notKept).toHaveBeenCalledWith("An item needs a name.")
    expect(outbox.getSnapshot().waiting).toBe(0)
  })
})
