// @vitest-environment jsdom
/** The record dropdown. What a person is owed here is a list they can OPEN and
 * read: clicking shows the records by title with their kind's glyph, typing
 * asks the SERVER (a collection of thousands is a few keystrokes away, not
 * capped at whatever page the browser happened to load), choosing one inserts
 * its id, a value the list does not hold is still reachable by typing it, and
 * the read ALWAYS ends: rows, "No <plural> yet", or a refusal with a retry.
 * A spinner that never stops (owner report, 2026-09-26: "Reading the
 * collection" forever on a person's Member of) is the failure this guards. */

import {
  QueryClient,
  QueryClientProvider,
  onlineManager,
} from "@tanstack/react-query"
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

type Answer = { records: unknown[]; cursor?: string } | "hang" | Error

const wire = vi.hoisted(() => ({
  /** Every list read the picker made, as its parsed filter. */
  reads: [] as { first: number; filter: Record<string, unknown> }[],
  /** What a read answers, by the filter it carried. */
  answer: ((): unknown => ({ records: [] })) as (
    filter: Record<string, unknown>
  ) => unknown,
}))

vi.mock("@/lib/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/http")>()
  return {
    ...actual,
    request: vi.fn(
      (
        _method: string,
        path: string,
        _body?: unknown,
        opts: { signal?: AbortSignal } = {}
      ) => {
        const url = new URL(path, "http://localhost")
        const filter = JSON.parse(url.searchParams.get("filter") ?? "{}")
        wire.reads.push({
          first: Number(url.searchParams.get("first")),
          filter,
        })
        const answer = wire.answer(filter) as Answer
        if (answer instanceof Error) return Promise.reject(answer)
        if (answer === "hang") {
          // A read the server never answers: it ends only when aborted.
          return new Promise((_, reject) =>
            opts.signal?.addEventListener("abort", () =>
              reject(new Error("aborted"))
            )
          )
        }
        return Promise.resolve(answer)
      }
    ),
  }
})

import { RecordCombobox } from "./record-combobox"

const ORG = "ada.example.com/people/organization"
const FUNCTION = "substrate.reamde.dev/core/function"

function kind(identity: string): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: { properties: {} },
  }
}
const KINDS = [kind(ORG), kind(FUNCTION)]

function row(
  k: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind: k,
    properties,
    labels: {},
    version: 1,
    createdAt: "x",
    updatedAt: "x",
  }
}

const ORGS = [
  row(ORG, "acme", { name: "Acme Robotics", title: "Acme Robotics" }),
  row(ORG, "globex", { name: "Globex", title: "Globex" }),
]

/** The host functions: registry records with no title, named by their id,
 * whose one-liner is what somebody choosing a tool wants to read. */
const HOST_FUNCTIONS = [
  row(FUNCTION, "substrate.reamde.dev/core/query", {
    description: "Read records: one by id, a filtered list, or a ranking.",
  }),
  row(FUNCTION, "substrate.reamde.dev/core/propose", {
    description:
      "Propose a reviewed change to the graph instead of writing it.",
  }),
  row(FUNCTION, "crew.test.dev/summarize", {
    title: "Summarize",
    description: "shorten a note",
  }),
]

function open(
  over: Partial<React.ComponentProps<typeof RecordCombobox>> = {}
): { onSelect: ReturnType<typeof vi.fn> } {
  const onSelect = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RecordCombobox
        id="pick"
        pin={ORG}
        kinds={KINDS}
        value=""
        onSelect={onSelect}
        ariaLabel="Member of"
        {...over}
      />
    </QueryClientProvider>
  )
  fireEvent.click(screen.getByLabelText(over.ariaLabel ?? "Member of"))
  return { onSelect }
}

function search(): HTMLInputElement {
  return screen.getByPlaceholderText(/^Search/) as HTMLInputElement
}

/** The record rows the list is offering right now, in order. */
function showing(): string[] {
  return [...document.querySelectorAll("[cmdk-item]")]
    .filter((el) => !el.hasAttribute("hidden"))
    .map((el) => (el.getAttribute("data-value") ?? "").split(" ")[0])
    .filter((v) => !v.startsWith("use-typed-") && v !== "remove-the-value")
}

beforeEach(() => {
  wire.reads = []
  wire.answer = (filter) => {
    const kinds = (filter.kinds as string[]) ?? []
    if (kinds.includes(FUNCTION)) return { records: HOST_FUNCTIONS }
    if (filter.search) {
      return {
        records: [row(ORG, "org1412", { title: "Harbor Design 1412" })],
      }
    }
    return { records: ORGS, cursor: "more" }
  }
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  onlineManager.setOnline(true)
})

describe("the record dropdown", () => {
  it("stays shut until it is opened, then shows the records by title", async () => {
    const client = new QueryClient()
    render(
      <QueryClientProvider client={client}>
        <RecordCombobox
          pin={ORG}
          kinds={KINDS}
          onSelect={vi.fn()}
          ariaLabel="Member of"
        />
      </QueryClientProvider>
    )
    expect(screen.queryByPlaceholderText(/^Search/)).toBeNull()
    fireEvent.click(screen.getByLabelText("Member of"))
    expect(await screen.findByText("Acme Robotics")).toBeTruthy()
    // Everyday mode names a record; the id it is stored under is not a name.
    expect(screen.queryByText("acme")).toBeNull()
    // Every row wears its kind's glyph.
    expect(
      document.querySelectorAll("[cmdk-item] [data-slot=kind-glyph]").length
    ).toBe(2)
  })

  it("inserts the record chosen, and closes", async () => {
    const { onSelect } = open()
    fireEvent.click(await screen.findByText("Globex"))
    expect(onSelect).toHaveBeenCalledWith("globex")
    await waitFor(() =>
      expect(screen.queryByPlaceholderText(/^Search/)).toBeNull()
    )
  })

  it("searches the server for what is typed, past the page it loaded", async () => {
    open()
    await screen.findByText("Acme Robotics")
    fireEvent.change(search(), { target: { value: "harb" } })
    expect(await screen.findByText("Harbor Design 1412")).toBeTruthy()
    const searched = wire.reads.find((r) => r.filter.search)
    expect(searched?.filter).toEqual({ kinds: [ORG], search: "harb*" })
  })

  it("names an untitled registry record by its id, and finds it by its one-liner", async () => {
    open({ pin: FUNCTION, ariaLabel: "Tool" })
    expect(
      await screen.findByText("substrate.reamde.dev/core/propose")
    ).toBeTruthy()
    expect(screen.getByText(/Propose a reviewed change/)).toBeTruthy()
    fireEvent.change(search(), { target: { value: "shorten" } })
    await waitFor(() => expect(showing()).toEqual(["crew.test.dev/summarize"]))
  })

  it("offers whatever is typed, because a record can be minted at any time", async () => {
    const { onSelect } = open()
    await screen.findByText("Acme Robotics")
    fireEvent.change(search(), { target: { value: "not-yet" } })
    fireEvent.click(screen.getByText(/^Use/))
    expect(onSelect).toHaveBeenCalledWith("not-yet")
  })

  it("leaves out what the caller already holds", async () => {
    open({ exclude: new Set(["acme"]) })
    await screen.findByText("Globex")
    expect(screen.queryByText("Acme Robotics")).toBeNull()
  })

  it("never offers a held id, or the record itself, as a typed one", async () => {
    open({ exclude: new Set(["acme"]), self: "globex" })
    await screen.findByText("No other organizations to choose.")
    fireEvent.change(search(), { target: { value: "acme" } })
    await waitFor(() => expect(wire.reads.length).toBeGreaterThan(1))
    expect(screen.queryByText(/^Use/)).toBeNull()
    fireEvent.change(search(), { target: { value: "globex" } })
    expect(screen.queryByText(/^Use/)).toBeNull()
  })

  it("says the collection is empty in words, not as a failed search", async () => {
    wire.answer = () => ({ records: [] })
    open()
    expect(await screen.findByText("No organizations yet.")).toBeTruthy()
  })

  it("says there is nothing OTHER to choose when only the record itself is there", async () => {
    wire.answer = () => ({ records: [ORGS[0]] })
    open({ self: "acme" })
    expect(
      await screen.findByText("No other organizations to choose.")
    ).toBeTruthy()
  })

  it("ends a read the server never answers in an error with a retry", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    wire.answer = () => "hang"
    open()
    expect(screen.getByText(/Reading/)).toBeTruthy()
    await act(() => vi.advanceTimersByTimeAsync(20_000))
    expect(screen.queryByText(/Reading/)).toBeNull()
    expect(screen.getByText(/took too long/)).toBeTruthy()
    wire.answer = () => ({ records: ORGS })
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    expect(await screen.findByText("Acme Robotics")).toBeTruthy()
  })

  it("says it is waiting for a connection rather than reading forever", async () => {
    onlineManager.setOnline(false)
    open()
    expect(await screen.findByText(/offline/i)).toBeTruthy()
    expect(screen.queryByText(/Reading/)).toBeNull()
  })

  it("says what went wrong, offers a retry, and leaves typing open", async () => {
    wire.answer = () => new Error("network error")
    const { onSelect } = open()
    expect(await screen.findByText(/network error/)).toBeTruthy()
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy()
    fireEvent.change(search(), { target: { value: "typed-anyway" } })
    fireEvent.click(screen.getByText(/^Use/))
    expect(onSelect).toHaveBeenCalledWith("typed-anyway")
  })

  it("says so when the pin names a kind this repository does not declare", () => {
    open({ pin: "gone.example.com/people/organization" })
    expect(screen.getByText(/doesn’t have/)).toBeTruthy()
    expect(wire.reads).toHaveLength(0)
  })

  it("reads as an ADD where a repeated picker grows its list", () => {
    open({ adding: true, addLabel: "Add", ariaLabel: "Add Member of" })
    expect(screen.getByLabelText("Add Member of")).toBeTruthy()
  })

  it("reads the chosen record by the title it is handed, with the id on the hover", () => {
    const client = new QueryClient()
    render(
      <QueryClientProvider client={client}>
        <RecordCombobox
          pin={ORG}
          kinds={KINDS}
          value="p9"
          valueTitle="Initech"
          onSelect={vi.fn()}
          ariaLabel="Employer"
        />
      </QueryClientProvider>
    )
    const trigger = screen.getByLabelText("Employer")
    expect(trigger.textContent).toBe("Initech")
    expect(trigger.querySelector("[title]")?.getAttribute("title")).toBe("p9")
  })
})
