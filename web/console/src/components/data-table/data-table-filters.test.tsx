// @vitest-environment jsdom
/** The toolbar's contract, pinned after the 2026-08-06 redlines: the × is a
 * real button that removes its filter (it used to be an svg inside the
 * trigger Button, dead under `[&_svg]:pointer-events-none`), and one active
 * filter is enough to earn the toolbar-level "Clear all". */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest"

import { DataTableFilters } from "./data-table-filters"
import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { ActiveFilter } from "@/lib/filters"
import type { DeclaredProperty } from "@/lib/definition"

const fields: DeclaredProperty[] = [
  { name: "title", kind: "string", repeated: false },
  { name: "prominence", kind: "state", repeated: false, states: ["known"] },
]

const titleFilter: ActiveFilter = { field: "title", op: "eq", value: "geo" }

function preferences(
  technicalDetails: boolean
): ConsolePreferencesContextValue {
  return {
    preferences: {
      collapsed: [],
      favorites: [],
      sidebarOpen: true,
      ...DEFAULT_SETTINGS,
      technicalDetails,
    },
    busy: false,
    change: () => {},
    set: () => {},
  }
}

afterEach(cleanup)

describe("removing an applied filter", () => {
  it("the × is a real button and removes exactly its filter", () => {
    const onChange = vi.fn()
    render(
      <DataTableFilters
        fields={fields}
        filters={[
          titleFilter,
          { field: "prominence", op: "eq", value: "known" },
        ]}
        onChange={onChange}
      />
    )

    const x = screen.getByRole("button", { name: "Remove title filter" })
    // The regression: an svg with role=button inside the trigger Button sits
    // under the Button's `[&_svg]:pointer-events-none` and can never be hit.
    // A native sibling <button> is hittable by construction.
    expect(x.tagName).toBe("BUTTON")
    expect(x.closest("[data-slot=popover-trigger]")).toBeNull()

    fireEvent.click(x)
    expect(onChange).toHaveBeenCalledExactlyOnceWith([
      { field: "prominence", op: "eq", value: "known" },
    ])
  })

  it("a prefix filter wears its trailing * in the control", () => {
    render(
      <DataTableFilters
        fields={fields}
        filters={[{ field: "title", op: "prefix", value: "geo" }]}
        onChange={vi.fn()}
      />
    )
    expect(screen.getByText("geo*")).toBeTruthy()
  })
})

describe("Clear all", () => {
  it("shows with one filter or more and clears the lot", () => {
    const onChange = vi.fn()
    render(
      <DataTableFilters
        fields={fields}
        filters={[titleFilter]}
        onChange={onChange}
      />
    )
    fireEvent.click(screen.getByRole("button", { name: "Clear all" }))
    expect(onChange).toHaveBeenCalledExactlyOnceWith([])
  })

  it("stays out of an empty toolbar", () => {
    render(<DataTableFilters fields={fields} filters={[]} onChange={vi.fn()} />)
    expect(screen.queryByRole("button", { name: "Clear all" })).toBeNull()
  })
})

/** A reference is filtered by PICKING its referents. The bar resolves the
 * field's pin (`acme.test/people/person`) against the registry, offers that
 * collection with the title a reader recognises, folds
 * several picks into one comma-joined value (the wire's `in`), and the
 * control reads the chosen records' titles rather than the ids it carries.
 * A pin that resolves to nothing keeps the text box. */
describe("a reference field", () => {
  const taskKind: KindInfo = {
    identity: "acme.test/tasks/task",
    name: "task",
    authority: "acme.test",
    package: "tasks",
    version: 1,
    source: "installed",
    description: "",
    definition: {
      properties: {
        assignee: { type: "reference", kind: "acme.test/people/person" },
        source: { type: "reference" },
      },
    },
  }
  const personKind: KindInfo = {
    identity: "acme.test/people/person",
    name: "person",
    authority: "acme.test",
    package: "people",
    version: 1,
    source: "installed",
    description: "",
    definition: { properties: { name: { type: "string" } } },
  }
  const referenceFields: DeclaredProperty[] = [
    {
      name: "assignee",
      kind: "reference",
      repeated: false,
      to: "acme.test/people/person",
    },
    { name: "source", kind: "reference", repeated: false },
  ]
  const person = (id: string, title: string): SubstrateRecord => ({
    id,
    kind: personKind.identity,
    properties: { title, name: title },
    labels: {},
    version: 1,
    createdAt: "2026-09-23T00:00:00Z",
    updatedAt: "2026-09-23T00:00:00Z",
  })
  const people = [
    person("ada", "Ada Lovelace"),
    person("grace", "Grace Hopper"),
  ]

  const fetchMock = vi.fn<typeof fetch>()

  /** The list read, answering the person collection whole, or the ids a
   * title read asks for. */
  function serve({ searchFails = false }: { searchFails?: boolean } = {}) {
    fetchMock.mockImplementation(async (input) => {
      const url = new URL(String(input), "http://test")
      const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
        ids?: string[]
        search?: string
      }
      if (filter.search && searchFails) {
        return new Response(
          JSON.stringify({ error: { message: "search index unavailable" } }),
          { status: 503 }
        )
      }
      // The server's search reaches past the page: Zed is only found there.
      const records = filter.ids
        ? people.filter((p) => filter.ids?.includes(p.id))
        : filter.search
          ? [...people, person("zed", "Zed Shaw")].filter((p) =>
              String(p.properties.title)
                .toLowerCase()
                .includes(filter.search!.replace(/\*$/, "").toLowerCase())
            )
          : people
      return new Response(
        JSON.stringify({ records, head: 0, generation: "g" }),
        { status: 200 }
      )
    })
    vi.stubGlobal("fetch", fetchMock)
  }

  function mount(
    filters: ActiveFilter[],
    onChange = vi.fn(),
    technical = false,
    searchFails = false
  ) {
    serve({ searchFails })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <ConsolePreferencesContext.Provider value={preferences(technical)}>
        <QueryClientProvider client={client}>
          <DataTableFilters
            fields={referenceFields}
            filters={filters}
            onChange={onChange}
            kinds={[taskKind, personKind]}
            labelOf={technical ? undefined : (name) => `The ${name}`}
          />
        </QueryClientProvider>
      </ConsolePreferencesContext.Provider>
    )
    return onChange
  }

  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("offers the pinned collection by title and applies the pick as the id", async () => {
    const onChange = mount([])
    fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
    fireEvent.click(await screen.findByText("The assignee"))
    // The pin names the people collection; the bar looked it up and read it.
    expect(await screen.findByPlaceholderText("Search people…")).toBeTruthy()
    fireEvent.click(await screen.findByText("Grace Hopper"))
    expect(onChange).toHaveBeenCalledExactlyOnceWith([
      { field: "assignee", op: "eq", value: "grace" },
    ])
  })

  it("adds a second pick to the same filter, comma-joined, as any of", async () => {
    const onChange = mount([{ field: "assignee", op: "eq", value: "grace" }])
    // The control itself reopens the picker.
    fireEvent.click(await screen.findByText("Grace Hopper"))
    fireEvent.click(await screen.findByText("Ada Lovelace"))
    expect(onChange).toHaveBeenCalledExactlyOnceWith([
      { field: "assignee", op: "eq", value: "grace,ada" },
    ])
  })

  it("reads the chosen records' titles in the control, not their ids", async () => {
    mount([{ field: "assignee", op: "eq", value: "ada,grace" }])
    const control = await screen.findByTitle("Ada Lovelace, Grace Hopper")
    expect(control.textContent).toBe("Ada Lovelace, Grace Hopper")
    // The title read is one batched list read by the ids, inside the kind.
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([input]) =>
          decodeURIComponent(String(input)).includes('"ids":["ada","grace"]')
        )
      ).toBe(true)
    )
  })

  it("keeps the text box for a reference pinned to no kind", async () => {
    mount([])
    fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
    fireEvent.click(await screen.findByText("The source"))
    expect(
      await screen.findByPlaceholderText("The source points at…")
    ).toBeTruthy()
    expect(screen.queryByPlaceholderText(/^Search /)).toBeNull()
  })

  it("names a property by its label alone, the datatype and target only in technical mode", async () => {
    mount([])
    fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
    const item = (await screen.findByText("The assignee")).closest(
      "[data-slot=command-item]"
    )
    expect(item?.textContent).toBe("The assignee")
    cleanup()

    mount([], vi.fn(), true)
    fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
    const tech = (await screen.findByText("assignee")).closest(
      "[data-slot=command-item]"
    )
    expect(tech?.textContent).toContain("reference → acme.test/people/person")
  })

  it("lists referents as a record mark with a visible check, the id only in technical mode", async () => {
    mount([{ field: "assignee", op: "eq", value: "grace" }])
    fireEvent.click(await screen.findByText("Grace Hopper"))
    const ada = (await screen.findByText("Ada Lovelace")).closest(
      "[data-slot=command-item]"
    )!
    expect(ada.querySelector("[data-slot=kind-glyph]")).toBeTruthy()
    expect(ada.textContent).not.toContain("ada")
    const rows = [...document.querySelectorAll("[data-slot=command-item]")]
    // The chosen row leads, and says so; the others show an empty box.
    expect(rows[0].textContent).toContain("Grace Hopper(chosen)")
    expect(ada.querySelector("[data-slot=picker-check] svg")).toBeNull()
    cleanup()

    mount([{ field: "assignee", op: "eq", value: "grace" }], vi.fn(), true)
    fireEvent.click(await screen.findByText("Grace Hopper"))
    const techAda = (await screen.findByText("Ada Lovelace")).closest(
      "[data-slot=command-item]"
    )!
    expect(techAda.textContent).toContain("ada")
  })

  it("says a refused search apart from the page's own matches, and retries it", async () => {
    mount([], vi.fn(), false, true)
    fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
    fireEvent.click(await screen.findByText("The assignee"))
    const input = await screen.findByPlaceholderText("Search people…")
    await screen.findByText("Ada Lovelace")
    // The page in hand still answers; the refused search is said beside it.
    fireEvent.change(input, { target: { value: "ada" } })
    expect((await screen.findByRole("alert")).textContent).toMatch(
      /search didn’t finish/
    )
    expect(screen.getByText("Ada Lovelace")).toBeTruthy()
    // With no match in hand, the failure is not "Nothing matches".
    fireEvent.change(input, { target: { value: "zed" } })
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toMatch(
        /search didn’t finish/
      )
    )
    expect(screen.queryByText("Nothing matches.")).toBeNull()
    // Try again asks the server once more, and its answer lands.
    serve()
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    expect(await screen.findByText("Zed Shaw")).toBeTruthy()
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("asks the server as the reader types, past the page in hand", async () => {
    mount([])
    fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
    fireEvent.click(await screen.findByText("The assignee"))
    const input = await screen.findByPlaceholderText("Search people…")
    await screen.findByText("Ada Lovelace")
    fireEvent.change(input, { target: { value: "zed" } })
    expect(await screen.findByText("Zed Shaw")).toBeTruthy()
    // The page's own rows that do not hold the text are gone meanwhile.
    expect(screen.queryByText("Ada Lovelace")).toBeNull()
    expect(
      fetchMock.mock.calls.some(([input]) =>
        decodeURIComponent(String(input)).includes('"search":"zed*"')
      )
    ).toBe(true)
  })
})

/** States, enum values and yes-or-no are picked from one ChoiceList in the
 * words the grid shows them in, and the applied control says those words
 * ("Status | Suggested"), never the stored value. A bar whose "states" only
 * borrow the shape (History's kinds and actors) keeps its stored values. */
describe("a declared set", () => {
  const setFields: DeclaredProperty[] = [
    {
      name: "status",
      kind: "state",
      repeated: false,
      states: ["proposed", "open", "done", "abandoned"],
      initial: "proposed",
    },
    {
      name: "priority",
      kind: "enum",
      repeated: false,
      values: [
        { value: "low", label: "" },
        { value: "high", label: "Urgent" },
      ],
    },
    { name: "flagged", kind: "bool", repeated: false },
  ]

  function mount(
    filters: ActiveFilter[],
    { technical = false, words = true } = {}
  ) {
    const onChange = vi.fn()
    render(
      <ConsolePreferencesContext.Provider value={preferences(technical)}>
        <DataTableFilters
          fields={setFields}
          filters={filters}
          onChange={onChange}
          labelOf={(name) => name[0].toUpperCase() + name.slice(1)}
          words={words}
        />
      </ConsolePreferencesContext.Provider>
    )
    return onChange
  }
  const items = () =>
    [...document.querySelectorAll("[data-slot=command-item]")].map(
      (r) => r.textContent
    )

  beforeAll(() => {
    Element.prototype.scrollIntoView ??= () => {}
  })

  it("lists a state's words and applies picks as its stored values", async () => {
    const onChange = mount([])
    fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
    fireEvent.click(await screen.findByText("Status"))
    expect(items()).toEqual(["Suggested", "Open", "Done", "Dropped"])
    fireEvent.click(screen.getByText("Suggested"))
    expect(onChange).toHaveBeenLastCalledWith([
      { field: "status", op: "eq", value: "proposed" },
    ])
  })

  it("says the words in the applied control", () => {
    mount([
      { field: "status", op: "eq", value: "proposed,open" },
      { field: "priority", op: "eq", value: "high" },
      { field: "flagged", op: "eq", value: "true" },
    ])
    expect(screen.getByTitle("Suggested, Open")).toBeTruthy()
    expect(screen.getByTitle("Urgent")).toBeTruthy()
    expect(screen.getByTitle("Yes")).toBeTruthy()
  })

  it("lists an enum's labels as tags instead of a text box, several at once", async () => {
    const onChange = mount([{ field: "priority", op: "eq", value: "low" }])
    fireEvent.click(screen.getByTitle("Low"))
    expect(await screen.findByRole("listbox")).toBeTruthy()
    expect(screen.queryByPlaceholderText(/Priority is/)).toBeNull()
    expect(items()).toEqual(["Low(chosen)", "Urgent"])
    fireEvent.click(screen.getByText("Urgent"))
    expect(onChange).toHaveBeenLastCalledWith([
      { field: "priority", op: "eq", value: "low,high" },
    ])
  })

  it("shows the stored value beside the words in technical mode", async () => {
    mount([], { technical: true })
    fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
    fireEvent.click(await screen.findByText("Priority"))
    expect(items()).toEqual(["Lowlow", "Urgenthigh"])
  })

  it("keeps stored values on a bar that borrows the state shape", async () => {
    mount([{ field: "status", op: "eq", value: "proposed" }], {
      words: false,
    })
    expect(screen.getByTitle("proposed")).toBeTruthy()
    fireEvent.click(screen.getByTitle("proposed"))
    await screen.findByRole("listbox")
    expect(items()).toEqual(["proposed(chosen)", "open", "done", "abandoned"])
  })
})
