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
import { useLinkedStatus, useRecord, useRecords } from "./hooks"

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
})
