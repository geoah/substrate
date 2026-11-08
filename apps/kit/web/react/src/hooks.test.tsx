import { SubstrateClient, defineKind, stringProp } from "@substrate/client"
import {
  LinkedRecords,
  MemoryStorage,
  Outbox,
  steadyConnectivity,
} from "@substrate/local"
import { FakeSubstrate, until } from "@substrate/testing"
import { act, cleanup, render } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SubstrateContext, type SubstrateContextValue } from "./context"
import {
  useLinkedStatus,
  useRecord,
  useRecords,
  type RecordRead,
} from "./hooks"

const Item = defineKind("substrate.reamde.dev/template/item", {
  read: (r) => ({ id: r.id, name: stringProp(r, "name") ?? "" }),
})
const Note = defineKind("substrate.reamde.dev/template/note", {
  read: (r) => ({ id: r.id, text: stringProp(r, "text") ?? "" }),
})

let fake: FakeSubstrate
let outbox: Outbox
let linked: LinkedRecords

beforeEach(() => {
  fake = new FakeSubstrate()
  vi.stubGlobal("fetch", fake.fetch)
  const client = new SubstrateClient(fake.origin, "tok")
  outbox = new Outbox({
    client,
    storage: new MemoryStorage(),
    connectivity: steadyConnectivity,
  })
  linked = new LinkedRecords({
    client,
    kinds: [Item.kind, Note.kind],
    outbox,
    connectivity: steadyConnectivity,
  })
})
afterEach(async () => {
  linked.stop()
  await outbox.close()
  cleanup()
  vi.unstubAllGlobals()
})

function Provider(props: { children: React.ReactNode }) {
  const value = { linked, outbox } as unknown as SubstrateContextValue
  return (
    <SubstrateContext.Provider value={value}>
      {props.children}
    </SubstrateContext.Provider>
  )
}

describe("the record hooks", () => {
  it("draw a list again only when its own kind changes, and keep each value", async () => {
    fake.seed(Item.kind, "a", { name: "Milk" })
    fake.seed(Note.kind, "n", { text: "Call" })
    const items: (readonly { id: string; name: string }[])[] = []
    let noteDraws = 0
    let statusDraws = 0
    function Items() {
      items.push(useRecords(Item))
      return null
    }
    function Notes() {
      useRecords(Note)
      noteDraws++
      return null
    }
    function Status() {
      useLinkedStatus()
      statusDraws++
      return null
    }
    render(
      <Provider>
        <Items />
        <Notes />
        <Status />
      </Provider>
    )
    linked.start()
    await act(() =>
      until(() => linked.statusSnapshot().status === "live", 2000, "live")
    )
    await act(() => until(() => items.at(-1)?.length === 1))
    const itemDraws = items.length
    const notesBefore = noteDraws
    const statusBefore = statusDraws
    const listed = items.at(-1)

    fake.change(Note.kind, "n", { text: "Call back" })
    await act(() =>
      until(
        () => linked.recordSnapshot(Note.kind, "n")?.version === 2,
        2000,
        "the note's change"
      )
    )
    expect(noteDraws).toBeGreaterThan(notesBefore)
    expect(items.length).toBe(itemDraws)
    expect(statusDraws).toBe(statusBefore)

    fake.seed(Item.kind, "b", { name: "Bread" })
    await act(() => until(() => items.at(-1)?.length === 2))
    // The record that did not change reads as the same value.
    expect(items.at(-1)?.find((i) => i.id === "a")).toBe(listed?.[0])
  })

  it("tells a record still being read from one that is not there", async () => {
    fake.seed(Item.kind, "a", { name: "Milk" })
    const reads: string[] = []
    function One(props: { id: string }) {
      const read = useRecord(Item, props.id)
      reads.push(`${props.id}:${read.state}`)
      return null
    }
    render(
      <Provider>
        <One id="a" />
        <One id="gone" />
      </Provider>
    )
    expect(reads).toEqual(["a:loading", "gone:loading"])
    linked.start()
    await act(() => until(() => reads.includes("gone:absent")))
    expect(reads).toContain("a:present")
  })

  it("reads by itself a record past a list cut off at its limit before saying it is absent", async () => {
    for (const id of ["a", "b", "c"]) fake.seed(Item.kind, id, { name: id })
    linked = new LinkedRecords({
      client: new SubstrateClient(fake.origin, "tok"),
      kinds: [Item.kind],
      outbox,
      connectivity: steadyConnectivity,
      limit: 2,
    })
    const reads: string[] = []
    function One(props: { id: string }) {
      const read = useRecord(Item, props.id)
      reads.push(`${props.id}:${read.state}`)
      return null
    }
    linked.start()
    await act(() => until(() => linked.statusSnapshot().status === "live"))
    render(
      <Provider>
        <One id="c" />
        <One id="gone" />
      </Provider>
    )
    await act(() =>
      until(() => reads.includes("c:present") && reads.includes("gone:absent"))
    )
    expect(reads.indexOf("c:loading")).toBeLessThan(reads.indexOf("c:present"))
    expect(reads.indexOf("gone:loading")).toBeLessThan(
      reads.indexOf("gone:absent")
    )
  })

  it("finds again a record it read absent once the history changes, past the new list's limit", async () => {
    for (const id of ["a", "b", "c"]) fake.seed(Item.kind, id, { name: id })
    linked = new LinkedRecords({
      client: new SubstrateClient(fake.origin, "tok"),
      kinds: [Item.kind],
      outbox,
      connectivity: steadyConnectivity,
      limit: 2,
      follow: { pauses: [5] },
    })
    const reads: string[] = []
    function One(props: { id: string }) {
      const read = useRecord(Item, props.id)
      reads.push(`${props.id}:${read.state}`)
      return null
    }
    linked.start()
    await act(() => until(() => linked.statusSnapshot().status === "live"))
    render(
      <Provider>
        <One id="z" />
      </Provider>
    )
    await act(() => until(() => reads.at(-1) === "z:absent"))

    // Away, z is made and the history moves on.
    fake.offline = true
    fake.dropWatches()
    await act(() =>
      until(() => linked.statusSnapshot().status === "reconnecting")
    )
    fake.seed(Item.kind, "z", { name: "z" })
    fake.compact()
    fake.offline = false
    await act(() => until(() => reads.at(-1) === "z:present"))
    expect(linked.statusSnapshot().limitReached).toBe(2)
  })

  it("says a read failed and reads it again on Try Again, the watch open throughout", async () => {
    for (const id of ["a", "b", "c"]) fake.seed(Item.kind, id, { name: id })
    linked = new LinkedRecords({
      client: new SubstrateClient(fake.origin, "tok"),
      kinds: [Item.kind],
      outbox,
      connectivity: steadyConnectivity,
      limit: 2,
      readRetryDelays: [],
    })
    const reads: RecordRead<{ id: string; name: string }>[] = []
    const read = () => reads.at(-1)
    function One() {
      reads.push(useRecord(Item, "c"))
      return null
    }
    linked.start()
    await act(() => until(() => linked.statusSnapshot().status === "live"))
    fake.fail({ method: "GET", path: /\/c$/ }, 503)
    render(
      <Provider>
        <One />
      </Provider>
    )
    await act(() => until(() => read()?.state === "failed"))
    expect(read()).toMatchObject({
      words: expect.stringContaining("Try again soon") as string,
    })
    act(() => {
      const failed = read()
      if (failed?.state === "failed") failed.retry()
    })
    await act(() => until(() => read()?.state === "present"))
    expect(read()?.value?.name).toBe("c")
    expect(fake.openWatches).toBe(1)
  })
})
