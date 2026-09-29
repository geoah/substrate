// @vitest-environment jsdom
/** A linked page of a collection stays that page: opening a nested collection
 * at `?page=2` before the registry has loaded must not fall back to page one
 * when the kind's metadata arrives and the view turns into a tree. Only a
 * reader's own change to the view renumbers it. The head names the
 * collection for a reader and keeps the kind reference for technical mode.
 * Saved views and the star are held through the real preference record, and
 * a grouped page through its heads, counts, folds and page notes. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { NuqsTestingAdapter, type UrlUpdateEvent } from "nuqs/adapters/testing"
import type { ReactNode } from "react"
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest"

import { ConsolePreferencesProvider } from "@/components/console-preferences"
import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { KindInfo } from "@/lib/api/types"
import {
  DEFAULT_SETTINGS,
  type ConsoleAction,
  type ConsolePreferences,
} from "@/lib/console-preferences"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

vi.mock("@/lib/api/changes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/changes")>()),
  watchChanges: () => ({ stop: () => {} }),
}))

vi.mock("@/router", () => ({
  kindBrowseRoute: {
    useParams: () => ({
      authority: "acme.example.com",
      pkg: "people",
      name: "team",
    }),
  },
}))

const TEAM = "acme.example.com/people/team"
const team: KindInfo = {
  identity: TEAM,
  name: "team",
  authority: "acme.example.com",
  package: "people",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    authority: "acme.example.com",
    package: "people",
    properties: {
      name: { type: "string" },
      parent: { type: "reference", kind: TEAM },
      size: { type: "enum", values: ["small", "large"] },
    },
  },
}

let resolveRegistry: (kinds: KindInfo[]) => void = () => {}
const offsets: (number | undefined)[] = []
const orders: (string | undefined)[] = []
// What the records read answers, and every filter a count was asked for.
let pageRecords: unknown[] = []
let pageCursor: string | undefined = "next"
const countFilters: unknown[] = []
// What a count answers, by the filter it was asked for.
const EVERY_COUNT = () => 400
let countOf: (filter: unknown) => number = EVERY_COUNT

vi.mock("@/lib/api/kinds", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/kinds")>()
  return {
    ...actual,
    kindsQueryOptions: {
      queryKey: ["registry", "kinds"],
      queryFn: () =>
        new Promise<KindInfo[]>((resolve) => {
          resolveRegistry = resolve
        }),
    },
  }
})

vi.mock("@/lib/api/records", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/records")>()
  return {
    ...actual,
    recordsQueryOptions: (
      p: Parameters<typeof actual.recordsQueryOptions>[0]
    ) => ({
      ...actual.recordsQueryOptions(p),
      queryFn: () => {
        offsets.push(p.offset)
        orders.push(p.orderBy)
        return Promise.resolve({ records: pageRecords, cursor: pageCursor })
      },
    }),
    recordCountQueryOptions: (
      ...args: Parameters<typeof actual.recordCountQueryOptions>
    ) => ({
      ...actual.recordCountQueryOptions(...args),
      queryFn: () => {
        countFilters.push(args[3])
        return Promise.resolve({ value: countOf(args[3]), capped: false })
      },
    }),
  }
})

import { KindBrowsePage } from "./kind-browse"

afterEach(() => {
  cleanup()
  offsets.length = 0
  orders.length = 0
  pageRecords = []
  pageCursor = "next"
  countFilters.length = 0
  countOf = EVERY_COUNT
  localStorage.clear()
})

describe("KindBrowsePage", () => {
  it("keeps a linked ?page= when a cold registry turns the view into a tree", async () => {
    const updates: UrlUpdateEvent[] = []
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <NuqsTestingAdapter
          searchParams="?page=2"
          onUrlUpdate={(e) => updates.push(e)}
        >
          <KindBrowsePage />
        </NuqsTestingAdapter>
      </QueryClientProvider>
    )

    await act(async () => resolveRegistry([team]))
    await waitFor(() => expect(offsets).toContain(50))

    expect(updates.map((u) => u.searchParams.get("page"))).not.toContain(null)
    expect(offsets).not.toContain(0)
  })
})

describe("the collection head", () => {
  async function renderHead(technical: boolean) {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const view = render(
      <ConsolePreferencesContext.Provider
        value={{
          preferences: {
            collapsed: [],
            favorites: [],
            sidebarOpen: true,
            ...DEFAULT_SETTINGS,
            technicalDetails: technical,
          },
          busy: false,
          change: () => {},
          set: () => {},
        }}
      >
        <QueryClientProvider client={client}>
          <NuqsTestingAdapter>
            <KindBrowsePage />
          </NuqsTestingAdapter>
        </QueryClientProvider>
      </ConsolePreferencesContext.Provider>
    )
    await waitFor(async () => {
      await act(async () => resolveRegistry([team]))
      expect(view.container.querySelector("[data-slot=page-header]")).not.toBe(
        null
      )
    })
    return view.container.querySelector("[data-slot=page-header]")!
  }

  it("names the collection without its reference for everyday readers", async () => {
    const head = await renderHead(false)
    expect(head.textContent).toContain("Teams")
    expect(head.textContent).not.toContain("acme.example.com")
    expect(head.querySelector("[aria-label='Copy the kind reference']")).toBe(
      null
    )
  })

  it("shows the reference and its copy button in technical mode", async () => {
    const head = await renderHead(true)
    expect(head.textContent).toContain("acme.example.com/people/team")
    expect(
      head.querySelector("[aria-label='Copy the kind reference']")
    ).not.toBe(null)
  })
})

describe("views, grouping and the star", () => {
  async function renderPage(
    searchParams: string,
    over: Partial<ConsolePreferences> = {}
  ) {
    const actions: ConsoleAction[] = []
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const view = render(
      <ConsolePreferencesContext.Provider
        value={{
          preferences: {
            collapsed: [],
            favorites: [],
            sidebarOpen: true,
            ...DEFAULT_SETTINGS,
            ...over,
          },
          busy: false,
          change: (a) => actions.push(a),
          set: () => {},
        }}
      >
        <QueryClientProvider client={client}>
          <NuqsTestingAdapter searchParams={searchParams}>
            <KindBrowsePage />
          </NuqsTestingAdapter>
        </QueryClientProvider>
      </ConsolePreferencesContext.Provider>
    )
    await waitFor(async () => {
      await act(async () => resolveRegistry([team]))
      expect(view.container.querySelector("[data-slot=page-header]")).not.toBe(
        null
      )
    })
    return actions
  }

  it("stars the collection from its header", async () => {
    const actions = await renderPage("")
    const star = screen.getByRole("button", { name: "Add Teams to favorites" })
    expect(star.getAttribute("aria-pressed")).toBe("false")
    fireEvent.click(star)
    expect(actions).toEqual([{ type: "favorite", key: TEAM, starred: true }])
  })

  // Codex P2: under a tree a child sits in its top-level row's group, so a
  // count of the records holding the value would not be what the head draws.
  const record = (id: string, size: string, parent?: string) => ({
    id,
    kind: TEAM,
    version: 1,
    createdAt: "2026-09-27T00:00:00Z",
    updatedAt: "2026-09-27T00:00:00Z",
    properties: {
      name: id,
      size,
      ...(parent ? { parent: { ref: `${TEAM}/${parent}` } } : {}),
    },
  })
  const sizeCounted = () =>
    countFilters.some(
      (f) =>
        (f as { properties?: Record<string, unknown> })?.properties?.size !==
        undefined
    )

  it("counts no group under the tree, where a row takes its root's group", async () => {
    pageRecords = [record("root", "small"), record("child", "large", "root")]
    await renderPage("?group=size")
    await waitFor(() => expect(orders).toContain("size:asc,updatedAt:desc"))
    await waitFor(() =>
      expect(screen.getAllByText(/small/i).length).toBeGreaterThan(0)
    )
    expect(sizeCounted()).toBe(false)
  })

  it("counts each group on a flat page", async () => {
    pageRecords = [record("a", "small"), record("b", "large")]
    await renderPage("?group=size&nest=false")
    await waitFor(() => expect(sizeCounted()).toBe(true))
  })

  it("orders the wire by the grouped property first", async () => {
    await renderPage("?group=size")
    await waitFor(() => expect(orders).toContain("size:asc,updatedAt:desc"))
    expect(
      screen.getByRole("button", { name: /Grouped by/ }).textContent
    ).toContain("size")
  })

  it("lights the saved view the page shows", async () => {
    await renderPage("?group=size", {
      views: [
        { id: "v1", collection: TEAM, name: "By size", group: "size" },
        { id: "v2", collection: "other.example.com/x/y", name: "Elsewhere" },
      ],
    })
    const views = screen.getByRole("group", { name: "Views" })
    const pressed = [...views.querySelectorAll("[aria-pressed=true]")].map(
      (b) => b.textContent
    )
    expect(pressed).toEqual(["By size"])
    expect(views.textContent).not.toContain("Elsewhere")
  })

  it("keeps the picked view chosen when another saved view shows the same", async () => {
    await renderPage("?group=size", {
      views: [
        { id: "v1", collection: TEAM, name: "By size", group: "size" },
        { id: "v2", collection: TEAM, name: "Sized", group: "size" },
      ],
    })
    const views = screen.getByRole("group", { name: "Views" })
    const pressedNames = () =>
      [...views.querySelectorAll("[aria-pressed=true]")].map(
        (b) => b.textContent
      )
    expect(pressedNames()).toEqual(["By size"])
    fireEvent.click(screen.getByRole("button", { name: "Sized" }))
    await waitFor(() => expect(pressedNames()).toEqual(["Sized"]))
    expect(
      screen.getByRole("button", { name: "More for the “Sized” view" })
    ).toBeTruthy()
  })
})

/** The same page under the real preference provider, over a stubbed wire:
 * what a saved view, a rename, a delete and the star write is the
 * `consolepreference` record itself, the one the sidebar reads. */
describe("through the preference record", () => {
  const PREFERENCE: KindInfo = {
    identity: "substrate.reamde.dev/core/consolepreference",
    name: "consolepreference",
    authority: "substrate.reamde.dev",
    package: "core",
    version: 3,
    source: "builtin",
    description: "",
    definition: {
      authority: "substrate.reamde.dev",
      package: "core",
      properties: {
        collapsed: { type: "string", repeated: true },
        favorites: { type: "string", repeated: true },
        views: { type: "object", repeated: true },
      },
    },
  }

  let stored: Record<string, unknown> = {}
  let version = 1
  const writes: Record<string, unknown>[] = []

  beforeAll(() => {
    // `SidebarProvider`, inside the preference provider, asks whether the
    // viewport is a phone; jsdom has no media queries.
    window.matchMedia = (media: string) =>
      ({
        media,
        matches: false,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }) as MediaQueryList
  })

  beforeEach(() => {
    stored = {}
    version = 1
    writes.length = 0
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (!String(url).endsWith("/consolepreference/navigation"))
          return new Response("{}", { status: 404 })
        if (init?.method === "PUT") {
          const body = JSON.parse(String(init.body))
          writes.push(body.properties)
          stored = body.properties
          version++
        }
        return new Response(
          JSON.stringify({
            id: "navigation",
            kind: PREFERENCE.identity,
            version,
            properties: stored,
          }),
          { status: 200 }
        )
      })
    )
  })

  afterEach(() => vi.unstubAllGlobals())

  async function renderPage(searchParams: string) {
    const urls: URLSearchParams[] = []
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const view = render(
      <QueryClientProvider client={client}>
        <ConsolePreferencesProvider>
          <NuqsTestingAdapter
            hasMemory
            searchParams={searchParams}
            onUrlUpdate={(e) => urls.push(e.searchParams)}
          >
            <KindBrowsePage />
          </NuqsTestingAdapter>
        </ConsolePreferencesProvider>
      </QueryClientProvider>
    )
    await waitFor(async () => {
      await act(async () => resolveRegistry([team, PREFERENCE]))
      expect(view.container.querySelector("[data-slot=page-header]")).not.toBe(
        null
      )
    })
    return { url: () => urls[urls.length - 1] }
  }

  const sized = (id: string, size: string) => ({
    id,
    kind: TEAM,
    version: 1,
    createdAt: "2026-09-27T00:00:00Z",
    updatedAt: "2026-09-27T00:00:00Z",
    properties: { name: id, size },
  })
  const headers = () =>
    screen.getAllByRole("columnheader").map((th) => th.textContent?.trim())
  const tabs = () => screen.getByRole("group", { name: "Views" })
  const pressedTabs = () =>
    [...tabs().querySelectorAll("[aria-pressed=true]")].map(
      (b) => b.textContent
    )
  async function press(name: string | RegExp) {
    const button = await screen.findByRole("button", { name })
    await waitFor(() =>
      expect((button as HTMLButtonElement).disabled).toBe(false)
    )
    fireEvent.click(button)
  }
  async function moveColumn(label: string, direction: "up" | "down") {
    await press(/Configure columns/)
    fireEvent.click(
      await screen.findByRole("button", { name: `Move ${label} ${direction}` })
    )
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    })
  }

  it("saves the view to the record, and picking it again restores filter, sort and columns", async () => {
    pageRecords = [sized("a", "small"), sized("b", "small")]
    const page = await renderPage(
      "?filter=size~eq~small&sort=size:asc&nest=false"
    )
    await waitFor(() =>
      expect(headers()).toEqual(["Name", "Size", "Name", "Updated"])
    )
    await moveColumn("Updated", "up")
    const savedHeaders = ["Name", "Size", "Updated", "Name"]
    await waitFor(() => expect(headers()).toEqual(savedHeaders))

    await press("Save view")
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Small ones" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(writes).toHaveLength(1))
    const [view] = writes[0].views as Record<string, unknown>[]
    expect(view).toEqual({
      id: expect.any(String),
      collection: TEAM,
      name: "Small ones",
      filter: ["size~eq~small"],
      sort: "size:asc",
      columns: expect.arrayContaining(["prop:name", "updatedAt"]),
      hidden: [],
      nest: false,
    })
    const columns = view.columns as string[]
    expect(columns.indexOf("updatedAt")).toBeLessThan(
      columns.indexOf("prop:name")
    )
    await waitFor(() => expect(pressedTabs()).toEqual(["Small ones"]))

    // All is the collection as it opens: no filter, the default order.
    fireEvent.click(screen.getByRole("button", { name: "All" }))
    await waitFor(() => expect(pressedTabs()).toEqual(["All"]))
    expect(page.url().get("filter")).toBeNull()
    expect(page.url().get("sort")).toBeNull()
    // The columns are the reader's own, so All leaves them; move one back.
    await moveColumn("Updated", "down")
    await waitFor(() =>
      expect(headers()).toEqual(["Name", "Size", "Name", "Updated"])
    )

    fireEvent.click(screen.getByRole("button", { name: "Small ones" }))
    await waitFor(() => expect(pressedTabs()).toEqual(["Small ones"]))
    expect(page.url().get("filter")).toBe("size~eq~small")
    expect(page.url().get("sort")).toBe("size:asc")
    expect(page.url().get("nest")).toBe("false")
    await waitFor(() => expect(headers()).toEqual(savedHeaders))

    // Another browser: nothing but the record. The view is there, and
    // picking it shows the same filter, sort, nesting and columns.
    cleanup()
    localStorage.clear()
    const other = await renderPage("")
    await waitFor(() => expect(pressedTabs()).toEqual(["All"]))
    expect(headers()).toEqual(["Name", "Size", "Name", "Updated"])
    await press("Small ones")
    await waitFor(() => expect(pressedTabs()).toEqual(["Small ones"]))
    expect(other.url().get("filter")).toBe("size~eq~small")
    expect(other.url().get("sort")).toBe("size:asc")
    expect(other.url().get("nest")).toBe("false")
    await waitFor(() => expect(headers()).toEqual(savedHeaders))
  })

  it("renames and deletes a saved view on the record", async () => {
    stored = {
      favorites: [],
      views: [
        {
          id: "v1",
          collection: TEAM,
          name: "Small ones",
          filter: ["size~eq~small"],
        },
      ],
    }
    await renderPage("?filter=size~eq~small")
    await waitFor(() => expect(pressedTabs()).toEqual(["Small ones"]))

    await press("More for the “Small ones” view")
    fireEvent.click(await screen.findByRole("menuitem", { name: "Rename…" }))
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Little" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Rename" }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].views).toEqual([
      {
        id: "v1",
        collection: TEAM,
        name: "Little",
        filter: ["size~eq~small"],
      },
    ])
    await waitFor(() => expect(pressedTabs()).toEqual(["Little"]))

    await press("More for the “Little” view")
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete…" }))
    fireEvent.click(screen.getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(writes).toHaveLength(2))
    expect(writes[1].views).toEqual([])
    await waitFor(() => expect(tabs().textContent).not.toContain("Little"))
  })

  it("saves changes to a view as the page shows them, a cleared filter included", async () => {
    stored = {
      favorites: [],
      views: [
        {
          id: "v1",
          collection: TEAM,
          name: "Small ones",
          filter: ["size~eq~small"],
          sort: "size:asc",
        },
      ],
    }
    const page = await renderPage("?filter=size~eq~small&sort=size:asc")
    await press("Small ones")
    // No row matches, so the empty grid offers to clear the filter.
    await press("Clear filters")
    await waitFor(() => expect(pressedTabs()).toEqual(["Small ones, changed"]))
    expect(page.url().get("sort")).toBe("size:asc")

    await press("More for the “Small ones” view")
    fireEvent.click(
      await screen.findByRole("menuitem", {
        name: "Save changes to this view…",
      })
    )
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(writes).toHaveLength(1))
    const [view] = writes[0].views as Record<string, unknown>[]
    expect(view).toEqual({
      id: "v1",
      collection: TEAM,
      name: "Small ones",
      sort: "size:asc",
      columns: expect.any(Array),
      hidden: [],
    })
    await waitFor(() => expect(pressedTabs()).toEqual(["Small ones"]))
  })

  it("stars and unstars the collection in the favorites the sidebar lists", async () => {
    await renderPage("")
    await press("Add Teams to favorites")
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].favorites).toEqual([TEAM])
    const star = await screen.findByRole("button", {
      name: "Remove Teams from favorites",
    })
    expect(star.getAttribute("aria-pressed")).toBe("true")
    await press("Remove Teams from favorites")
    await waitFor(() => expect(writes).toHaveLength(2))
    expect(writes[1].favorites).toEqual([])
  })

  it("presses the star for a collection the sidebar starred", async () => {
    stored = { favorites: [TEAM] }
    await renderPage("")
    const star = await screen.findByRole("button", {
      name: "Remove Teams from favorites",
    })
    expect(star.getAttribute("aria-pressed")).toBe("true")
  })
})

describe("a grouped page", () => {
  const sized = (id: string, size?: string) => ({
    id,
    kind: TEAM,
    version: 1,
    createdAt: "2026-09-27T00:00:00Z",
    updatedAt: "2026-09-27T00:00:00Z",
    properties: { title: id, name: id, ...(size ? { size } : {}) },
  })

  async function renderGrouped(searchParams: string) {
    // Each group's whole size, by the value the count narrows to: 2 small,
    // 30 large, 5 with no size, 400 in all.
    countOf = (filter) => {
      const cond = (
        filter as
          | { properties?: Record<string, { eq?: string; exists?: boolean }> }
          | undefined
      )?.properties?.size
      if (!cond) return 400
      if (cond.exists === false) return 5
      return cond.eq === "small" ? 2 : 30
    }
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <NuqsTestingAdapter hasMemory searchParams={searchParams}>
          <KindBrowsePage />
        </NuqsTestingAdapter>
      </QueryClientProvider>
    )
    await act(async () => resolveRegistry([team]))
  }

  const heads = () =>
    [...document.querySelectorAll("th[scope=rowgroup]")].map((th) =>
      th.textContent?.replace(/\s+/g, " ").trim()
    )
  const rowNames = () =>
    [...document.querySelectorAll("tbody tr td:first-child")].map((td) =>
      td.textContent?.trim()
    )

  it("heads one fold per value on the page with the whole group's count", async () => {
    pageRecords = [
      sized("a", "small"),
      sized("b", "small"),
      sized("c", "large"),
      sized("d", "large"),
    ]
    await renderGrouped("?group=size&nest=false")
    await waitFor(() =>
      expect(heads()).toEqual(["Small2", "Large30continues on the next page"])
    )
    expect(rowNames()).toEqual(["a", "b", "c", "d"])

    const fold = screen.getByRole("button", { name: "Hide Size: Small" })
    expect(fold.getAttribute("aria-expanded")).toBe("true")
    fireEvent.click(fold)
    const unfold = screen.getByRole("button", { name: "Show Size: Small" })
    expect(unfold.getAttribute("aria-expanded")).toBe("false")
    expect(rowNames()).toEqual(["c", "d"])
    fireEvent.click(unfold)
    expect(rowNames()).toEqual(["a", "b", "c", "d"])
  })

  it("says in the Group menu that a page heads only its own records' groups", async () => {
    pageRecords = [sized("a", "small")]
    await renderGrouped("?group=size&nest=false")
    fireEvent.click(await screen.findByRole("button", { name: /Grouped by/ }))
    expect(
      await screen.findByText(
        "A page shows the groups of the records on it. Each count is the whole group."
      )
    ).toBeTruthy()
  })

  it("says where a group runs on from and to across pages", async () => {
    // A middle page one group fills: it may have begun or ended here.
    pageRecords = [sized("c", "large"), sized("d", "large")]
    await renderGrouped("?group=size&nest=false&page=2")
    await waitFor(() =>
      expect(heads()).toEqual(["Large3028 more on other pages"])
    )
    cleanup()

    pageRecords = [sized("c", "large"), sized("e")]
    await renderGrouped("?group=size&nest=false&page=2")
    await waitFor(() =>
      expect(heads()).toEqual([
        "Large30continued from the previous page",
        "No size5continues on the next page",
      ])
    )
    cleanup()

    // The last page: nothing runs on.
    pageCursor = undefined
    pageRecords = [sized("e"), sized("f")]
    await renderGrouped("?group=size&nest=false&page=3")
    await waitFor(() =>
      expect(heads()).toEqual(["No size5continued from the previous page"])
    )
  })
})
