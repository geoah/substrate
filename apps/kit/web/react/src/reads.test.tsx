import type { AppManifest, Session } from "@substrate/auth"
import { SubstrateClient, stringProp } from "@substrate/client"
import {
  MemoryStorage,
  Outbox,
  RecordWrites,
  steadyConnectivity,
} from "@substrate/local"
import { FakeSubstrate, until } from "@substrate/testing"
import { act, cleanup, render, waitFor } from "@testing-library/react"
import { useContext, useEffect, type ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  SubstrateContext,
  useConnection,
  type SubstrateContextValue,
} from "./context"
import { useInvalidate, useRead } from "./hooks"
import { SubstrateProvider } from "./provider"
import { ReadCache, type ReadCacheOptions, type ReadState } from "./reads"

const item = "substrate.reamde.dev/template/item"
const note = "substrate.reamde.dev/template/note"
const sample = "substrate.reamde.dev/observations/observation"
const patient = "substrate.reamde.dev/clinical/patient"

let fake: FakeSubstrate
let client: SubstrateClient
let outbox: Outbox
let reads: ReadCache
/** How many times each kind's read ran. */
let counts: Record<string, number>

/** The session's reads, the feed's changes gathered for 30 ms. */
function open(opts: Partial<ReadCacheOptions> = {}) {
  reads = new ReadCache({
    client,
    outbox,
    follow: { pauses: [0] },
    coalesce: 30,
    ...opts,
  })
  reads.start()
}

beforeEach(() => {
  fake = new FakeSubstrate()
  vi.stubGlobal("fetch", fake.fetch)
  // A scoped app token: its lists carry no head.
  client = new SubstrateClient(fake.origin, "app-token")
  outbox = new Outbox({
    client,
    storage: new MemoryStorage(),
    connectivity: steadyConnectivity,
  })
  counts = {}
  open()
})
afterEach(async () => {
  cleanup()
  reads.close()
  await outbox.close()
  vi.unstubAllGlobals()
})

function Provider(props: { children: ReactNode }) {
  const value = { client, outbox, reads } as unknown as SubstrateContextValue
  return (
    <SubstrateContext.Provider value={value}>
      {props.children}
    </SubstrateContext.Provider>
  )
}

/** The names of a kind's records, read through the client. */
function names(kind: string) {
  return async (c: SubstrateClient, signal: AbortSignal) => {
    counts[kind] = (counts[kind] ?? 0) + 1
    const page = await c.list({ kinds: [kind] }, signal)
    return page.records.map((r) => stringProp(r, "name") ?? "").sort()
  }
}

/** Each reader's states as drawn, by its label. */
let drawn: Record<string, ReadState<string[]>[]>
const last = (label: string) => drawn[label]?.at(-1)

function Names(props: {
  label: string
  readKey: string | null
  kind: string
  kinds?: readonly string[]
  staleAfter?: number
}) {
  const state = useRead(props.readKey, names(props.kind), {
    kinds: props.kinds ?? [props.kind],
    staleAfter: props.staleAfter,
  })
  ;(drawn[props.label] ??= []).push(state)
  return null
}

beforeEach(() => {
  drawn = {}
})

const watches = () => fake.requests.filter((r) => r.path === "/api/v1/changes")
const answered = (label: string, value: string[]) => () =>
  JSON.stringify(last(label)?.data) === JSON.stringify(value)

/** The page goes to the back and comes to the front again. */
function backToFront() {
  for (const state of ["hidden", "visible"]) {
    Object.defineProperty(document, "visibilityState", {
      value: state,
      configurable: true,
    })
    document.dispatchEvent(new Event("visibilitychange"))
  }
}

describe("useRead", () => {
  it("shares one read between the readers of a key, and reads nothing for a null key", async () => {
    fake.seed(item, "a", { name: "Milk" })
    render(
      <Provider>
        <Names label="one" readKey="items" kind={item} />
        <Names label="two" readKey="items" kind={item} />
        <Names label="none" readKey={null} kind={note} />
      </Provider>
    )
    expect(last("one")).toMatchObject({ loading: true, refreshing: false })
    await act(() => until(answered("one", ["Milk"]), 2000, "the read"))
    expect(last("two")?.data).toEqual(["Milk"])
    expect(last("two")).toMatchObject({ loading: false, refreshing: false })
    expect(counts[item]).toBe(1)
    expect(last("none")).toMatchObject({
      data: undefined,
      loading: false,
      refreshing: false,
    })
    expect(counts[note]).toBeUndefined()
    // The null key named no kind for the feed either.
    expect(watches().map((r) => r.query.getAll("kinds"))).toEqual([[item]])
  })

  it("reads again when the outbox lands a write of its kinds, keeping the last answer meanwhile", async () => {
    // The feed's changes wait longer than the test: only the outbox reads
    // it again here.
    reads.close()
    open({ coalesce: 60_000 })
    fake.seed(item, "a", { name: "Milk" })
    render(
      <Provider>
        <Names label="items" readKey="items" kind={item} />
      </Provider>
    )
    await act(() => until(answered("items", ["Milk"]), 2000, "the read"))
    await act(async () => {
      await outbox.write(RecordWrites.create(item, "b", { name: "Bread" }))
    })
    await act(() =>
      until(answered("items", ["Bread", "Milk"]), 2000, "the read again")
    )
    expect(counts[item]).toBe(2)
    // The first answer stayed while the second was read.
    expect(drawn.items).toContainEqual(
      expect.objectContaining({ data: ["Milk"], refreshing: true })
    )
  })

  it("reads again on useInvalidate, aborting the read it replaces", async () => {
    reads.close()
    open({ coalesce: 60_000 })
    fake.seed(item, "a", { name: "Milk" })
    const signals: AbortSignal[] = []
    let invalidate!: (kinds: readonly string[]) => void
    function Slow() {
      const again = useInvalidate()
      useEffect(() => {
        invalidate = again
      })
      const state = useRead(
        "slow",
        async (c, signal) => {
          signals.push(signal)
          if (signals.length === 1) await new Promise(() => {})
          return (await names(item)(c, signal)).join()
        },
        { kinds: [item] }
      )
      return <p>{state.data ?? "…"}</p>
    }
    const { container } = render(
      <Provider>
        <Slow />
      </Provider>
    )
    await act(() => until(() => signals.length === 1, 2000, "the first read"))
    // A write made through the client, which the outbox never saw.
    await client.put(item, "b", { name: "Bread" }, 0)
    act(() => invalidate([item]))
    await act(() => until(() => container.textContent === "Bread,Milk"))
    expect(signals[0].aborted).toBe(true)
    expect(signals[1].aborted).toBe(false)
  })

  it("reads again on the feed's changes, gathered into one read", async () => {
    reads.close()
    open({ coalesce: 40 })
    fake.seed(item, "a", { name: "Milk" })
    render(
      <Provider>
        <Names label="items" readKey="items" kind={item} />
      </Provider>
    )
    await act(() => until(answered("items", ["Milk"]), 2000, "the read"))
    fake.change(item, "a", { name: "Oat milk" })
    fake.seed(item, "b", { name: "Bread" })
    fake.seed(item, "c", { name: "Eggs" })
    await act(() =>
      until(answered("items", ["Bread", "Eggs", "Oat milk"]), 2000, "the feed")
    )
    expect(counts[item]).toBe(2)
    // A change to a kind no read names reads nothing again.
    fake.seed(note, "n", { name: "Call" })
    await new Promise((r) => setTimeout(r, 80))
    expect(counts[item]).toBe(2)
  })

  it("never follows a sample kind, and reads it again when the page comes back after staleAfter", async () => {
    fake.seed(sample, "s1", { name: "72" })
    render(
      <Provider>
        <Names label="sample" readKey="hr" kind={sample} staleAfter={0} />
        <Names label="items" readKey="items" kind={item} staleAfter={0} />
      </Provider>
    )
    await act(() => until(answered("sample", ["72"]), 2000, "the sample"))
    await act(() => until(answered("items", []), 2000, "the items"))
    // Read at once, with no probe and no watch of its own.
    expect(
      fake.requests.some(
        (r) =>
          r.query.get("first") === "1" &&
          r.query.get("filter")?.includes(sample)
      )
    ).toBe(false)
    expect(watches().map((r) => r.query.getAll("kinds"))).toEqual([[item]])

    fake.seed(sample, "s2", { name: "75" })
    act(() => backToFront())
    await act(() => until(answered("sample", ["72", "75"]), 2000, "the sample"))
    expect(counts[sample]).toBe(2)
    // The feed keeps the items current: coming back reads nothing of them.
    expect(counts[item]).toBe(1)
  })

  it("holds a read until the watch has opened, so a change between them is seen", async () => {
    fake.seed(item, "a", { name: "Milk" })
    let letOpen!: () => void
    const gate = new Promise<void>((r) => (letOpen = r))
    let asked = false
    vi.stubGlobal("fetch", async (input: string, init: RequestInit) => {
      if (new URL(input).pathname === "/api/v1/changes") {
        asked = true
        await gate
      }
      return fake.fetch(input, init)
    })
    render(
      <Provider>
        <Names label="items" readKey="items" kind={item} />
      </Provider>
    )
    await act(() => until(() => asked, 2000, "the watch"))
    expect(counts[item]).toBeUndefined()
    expect(last("items")).toMatchObject({ loading: true })
    // A change after the read was asked for and before the stream opens,
    // which a watch from the live head will not carry.
    fake.change(item, "a", { name: "Oat milk" })
    letOpen()
    await act(() => until(answered("items", ["Oat milk"]), 2000, "the read"))
    expect(counts[item]).toBe(1)
  })

  it("starts again from the last change it saw when its kinds grow", async () => {
    fake.seed(item, "a", { name: "Milk" })
    fake.seed(note, "n", { name: "Call" })
    function Both(props: { notes: boolean }) {
      return (
        <Provider>
          <Names label="items" readKey="items" kind={item} />
          {props.notes && <Names label="notes" readKey="notes" kind={note} />}
        </Provider>
      )
    }
    const { rerender } = render(<Both notes={false} />)
    await act(() => until(answered("items", ["Milk"]), 2000, "the items"))
    fake.change(item, "a", { name: "Oat milk" })
    const seen = fake.head
    await act(() =>
      until(answered("items", ["Oat milk"]), 4000, "the item's change")
    )
    rerender(<Both notes />)
    await act(() => until(answered("notes", ["Call"]), 2000, "the notes"))
    const [first, second] = watches()
    expect(first.query.get("from")).toBeNull()
    expect(second.query.getAll("kinds")).toEqual([item, note])
    expect(second.query.get("from")).toBe(String(seen))
    expect(second.query.get("generation")).toBe("g1")
    expect(fake.openWatches).toBe(1)
  })

  it("on a relist, opens a fresh watch before it reads again", async () => {
    fake.seed(item, "a", { name: "Milk" })
    render(
      <Provider>
        <Names label="items" readKey="items" kind={item} />
      </Provider>
    )
    await act(() => until(answered("items", ["Milk"]), 2000, "the read"))
    fake.compact()
    fake.dropWatches()
    await act(() => until(() => counts[item] === 2, 2000, "the read again"))
    const index = (match: (r: (typeof fake.requests)[number]) => boolean) =>
      fake.requests.findLastIndex(match)
    const refused = index(
      (r) => r.path === "/api/v1/changes" && r.query.get("from") !== null
    )
    const fresh = index(
      (r) => r.path === "/api/v1/changes" && r.query.get("from") === null
    )
    const reread = index(
      (r) => r.path === "/api/v1/records" && r.query.get("first") === "200"
    )
    expect(refused).toBeGreaterThan(0)
    expect(fresh).toBeGreaterThan(refused)
    expect(reread).toBeGreaterThan(fresh)
  })

  it("leaves out a kind the repository does not hold, follows the rest, and reads that read by staleAfter", async () => {
    fake.undeclared.add(patient)
    fake.seed(item, "a", { name: "Milk" })
    render(
      <Provider>
        <Names
          label="items"
          readKey="items"
          kind={item}
          kinds={[item, patient]}
          staleAfter={0}
        />
      </Provider>
    )
    await act(() => until(answered("items", ["Milk"]), 2000, "the read"))
    expect(watches().map((r) => r.query.getAll("kinds"))).toEqual([[item]])
    expect(fake.openWatches).toBe(1)
    // The watch stands: a change of the followed kind still reads again.
    fake.change(item, "a", { name: "Oat milk" })
    await act(() =>
      until(answered("items", ["Oat milk"]), 4000, "the item's change")
    )
    expect(fake.openWatches).toBe(1)
    // The feed does not keep it current, so coming back reads it again.
    const before = counts[item]
    act(() => backToFront())
    await act(() => until(() => counts[item] === before + 1))
  })

  it("aborts every read when the session's reads close", async () => {
    const signals: AbortSignal[] = []
    function Pending() {
      useRead("pending", (_c, signal) => {
        signals.push(signal)
        return new Promise<never>(() => {})
      })
      return null
    }
    render(
      <Provider>
        <Pending />
      </Provider>
    )
    await act(() => until(() => signals.length === 1))
    reads.close()
    // Still drawn, and aborted all the same.
    expect(signals[0].aborted).toBe(true)
  })

  it("aborts a key's read when its last reader leaves", async () => {
    const signals: AbortSignal[] = []
    function Pending() {
      useRead("pending", (_c, signal) => {
        signals.push(signal)
        return new Promise<never>(() => {})
      })
      return null
    }
    function Both(props: { first: boolean; second: boolean }) {
      return (
        <Provider>
          {props.first && <Pending />}
          {props.second && <Pending />}
        </Provider>
      )
    }
    const { rerender } = render(<Both first second />)
    await act(() => until(() => signals.length === 1))
    rerender(<Both first={false} second />)
    await act(() => Promise.resolve())
    expect(signals[0].aborted).toBe(false)
    rerender(<Both first={false} second={false} />)
    await act(() => until(() => signals[0].aborted, 2000, "the abort"))
    expect(signals).toHaveLength(1)
  })
})

describe("useRead under SubstrateProvider", () => {
  const manifest: AppManifest = {
    id: "template",
    name: "Substrate Template",
    clientID: "https://app.example.com/client.json",
    redirectURI: "https://app.example.com/",
    scopes: ["write:substrate.reamde.dev/template/*"],
    details: [],
    softwareVersion: "1.0.0",
  }
  const session: Session = {
    server: "https://substrate.example.com",
    repository: "reads.example.org",
    token: "tok-a",
    scopes: manifest.scopes,
    clientID: manifest.clientID,
    details: [],
    signedInAt: 0,
  }

  it("aborts every read when the session signs out, and drops its answers", async () => {
    localStorage.clear()
    localStorage.setItem("substrate.session.template", JSON.stringify(session))
    const signals: AbortSignal[] = []
    let open: SubstrateContextValue | null = null
    function Pending() {
      const value = useContext(SubstrateContext)
      useEffect(() => {
        open = value
      })
      useRead("pending", (_c, signal) => {
        signals.push(signal)
        return new Promise<never>(() => {})
      })
      const answered = useRead("answered", async () => "kept")
      return <p>{answered.data}</p>
    }
    function Shell() {
      const { phase } = useConnection()
      return phase === "open" ? <Pending /> : null
    }
    render(
      <SubstrateProvider manifest={manifest} kinds={[item]}>
        <Shell />
      </SubstrateProvider>
    )
    await waitFor(() => expect(signals).toHaveLength(1), { timeout: 3000 })
    await waitFor(() => expect(document.body.textContent).toBe("kept"))
    const repository = open as SubstrateContextValue | null
    expect(repository?.reads.snapshot("answered").data).toBe("kept")
    await act(async () => {
      await repository?.signOut()
    })
    expect(signals[0].aborted).toBe(true)
    // The session's answers went with it.
    expect(repository?.reads.snapshot("answered").data).toBeUndefined()
    localStorage.clear()
  })
})
