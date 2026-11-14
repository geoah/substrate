// @vitest-environment jsdom
/** The samples an "Add …" entry offers, over two publishers' samples of one
 * package word that ship the same kind: both land at `<home>/tasks`, so the
 * copy this repository holds is told apart by its origin stamp. The one it
 * came from reads as added; the other is a row of its own, named by its
 * shipped id, whose Add imports it. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Toaster } from "@/components/ui/toast"
import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { BundleStatus, CatalogItem } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: React.ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-params={JSON.stringify(params ?? {})} {...rest}>
      {children}
    </a>
  ),
}))

import { collectionSamples } from "./sample-picks"
import { SampleList } from "./sample-list"

const HOME = "ada.example.com"
/** Where a sample lands: the repository's own namespace, whatever its name
 * (decision record 0259). */
const LOCAL = "~"
const CATALOG_PATH = "/api/v1/catalog"

function tasks(authority: string, installed: boolean) {
  return {
    id: `${authority}/tasks`,
    name: "tasks",
    authority,
    package: "tasks",
    description: "",
    version: 1,
    keptBy: "repository",
    installed,
    closure: {
      kinds: [`${authority}/tasks/task`],
      traits: null,
      functions: null,
      agents: null,
      mappings: null,
      records: null,
      triggers: null,
    },
  } satisfies CatalogItem
}

// Each case runs in both catalog orders: a lookup by the landed id alone
// hands the copy to whichever entry it meets last, or first.
const OTHER = tasks("a.example.com", false)
const MINE = tasks("z.example.com", true)

const COPY: BundleStatus = {
  id: `${LOCAL}/tasks`,
  name: "tasks",
  authority: LOCAL,
  package: "tasks",
  installed: true,
  enabled: true,
  version: 1,
  origin: MINE.id,
  accounts: 0,
  functions: 0,
  kinds: 1,
  liveRecords: 0,
}

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200 })
}

const fetchMock = vi.fn<typeof fetch>()

function serve(catalog: CatalogItem[]) {
  fetchMock.mockImplementation(async (input, init) => {
    const url = new URL(String(input), "http://console.test")
    const method = init?.method ?? "GET"
    if (url.pathname === "/api/v1/substrate.reamde.dev/core/bundle/status")
      return json({ items: [COPY] })
    if (url.pathname === CATALOG_PATH) return json({ items: catalog })
    if (url.pathname.startsWith(`${CATALOG_PATH}/`)) {
      const id = decodeURIComponent(url.pathname.split("/")[4])
      if (method === "POST")
        return json({ ...COPY, origin: id, id: `${LOCAL}/tasks` })
      return json(catalog.find((c) => c.id === id))
    }
    const filter = url.searchParams.get("filter") ?? ""
    if (filter.includes("substrate.reamde.dev/core/repository"))
      return json({
        records: [
          {
            id: "r_1",
            kind: "substrate.reamde.dev/core/repository",
            properties: { name: "ada", authority: HOME },
          },
        ],
      })
    if (filter.includes("substrate.reamde.dev/core/kind"))
      return json({ kinds: [] })
    return json({ records: [], items: [] })
  })
}

function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
          technicalDetails: false,
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>
        <Toaster>
          <SampleList
            pick={collectionSamples}
            member={(id) => <span data-slot="member">{id}</span>}
            empty="No samples."
          />
        </Toaster>
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

/** The list item that names one shipped sample. */
async function rowFrom(id: string): Promise<HTMLElement> {
  const source = await screen.findByText(id)
  return source.closest("li") as HTMLElement
}

describe("SampleList", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it.each([
    ["before", [OTHER, MINE]],
    ["after", [MINE, OTHER]],
  ])(
    "lists both samples when the other one comes %s, and marks the copy's own as added",
    async (_, catalog) => {
      serve(catalog)
      mount()
      const mine = await rowFrom(MINE.id)
      const other = await rowFrom(OTHER.id)
      expect(mine).not.toBe(other)
      expect(screen.getAllByText("Tasks")).toHaveLength(2)
      for (const row of [mine, other]) {
        expect(within(row).getByText(`${LOCAL}/tasks/task`)).toBeTruthy()
      }
      // Added links to the copy's package page, where it is updated and
      // removed (decision record 0238).
      const added = within(mine).getByText("Added").closest("a")!
      expect(added.dataset.to).toBe("/data/$authority/$pkg")
      expect(JSON.parse(added.dataset.params!)).toEqual({
        authority: LOCAL,
        pkg: "tasks",
      })
      expect(within(mine).queryByRole("button")).toBeNull()
      expect(within(other).queryByText("Added")).toBeNull()
      fireEvent.click(within(other).getByRole("button", { name: "Add" }))
      await waitFor(() =>
        expect(
          fetchMock.mock.calls
            .filter(([, init]) => init?.method === "POST")
            .map(([url]) => String(url))
        ).toEqual([`${CATALOG_PATH}/${encodeURIComponent(OTHER.id)}/add`])
      )
    }
  )
})
