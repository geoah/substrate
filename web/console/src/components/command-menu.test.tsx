// @vitest-environment jsdom
/** ⌘K: what is typed finds records first, collections and pages by the
 * start of their words, and the door to the Search page last. */

import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
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

import type { KindInfo } from "@/lib/api/types"
import { CommandMenu } from "./command-menu"

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

const KINDS = [
  kind("ada.example.com/tasks/task"),
  kind("providers.substrate.reamde.dev/google/drivefile"),
]

const searches: string[] = []

beforeEach(() => {
  searches.length = 0
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const url = new URL(String(input), "http://localhost")
      const q = url.searchParams.get("q")
      if (q) {
        searches.push(q)
        return Response.json({
          records: [
            {
              id: "t1",
              kind: "ada.example.com/tasks/task",
              version: 1,
              properties: {
                name: "Prepare travel for Lisbon",
                title: "Prepare travel for Lisbon",
              },
            },
          ],
          scores: {},
          pending: 0,
        })
      }
      // the kind registry, in its flat shape
      return Response.json({ records: KINDS })
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderMenu() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const rootRoute = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>
        <CommandMenu open onOpenChange={() => {}} />
      </QueryClientProvider>
    ),
  })
  const routeTree = rootRoute.addChildren(
    [
      "/data",
      "/agents",
      "/tools",
      "/providers",
      "/history",
      "/settings",
      "/search",
      "/data/$authority/$pkg/$name",
      "/data/$authority/$pkg/$name/$id",
    ].map((path) =>
      createRoute({
        getParentRoute: () => rootRoute,
        path,
        component: () => null,
      })
    )
  )
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  return render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
}

async function type(text: string) {
  fireEvent.change(await screen.findByPlaceholderText("Search or jump to…"), {
    target: { value: text },
  })
}

const headings = () =>
  [...document.querySelectorAll("[cmdk-group-heading]")].map(
    (h) => h.textContent
  )

describe("CommandMenu", () => {
  it("offers the records a words search finds, above collections and pages", async () => {
    renderMenu()
    await type("lisb")
    const records = await screen.findByRole("group", { name: "Records" })
    expect(
      await within(records).findByText("Prepare travel for Lisbon")
    ).toBeTruthy()
    expect(within(records).getByText("Tasks")).toBeTruthy()
    // the word being typed is asked for as a prefix
    expect(searches).toEqual(["lisb*"])
    expect(headings()).toEqual(["Records", "Search"])
    // Enter opens the best record, not the row that was first before it came
    await waitFor(() =>
      expect(
        screen
          .getByRole("option", { selected: true })
          .textContent?.includes("Prepare travel for Lisbon")
      ).toBe(true)
    )
    expect(screen.getByText(/Search your records for “lisb”/)).toBeTruthy()
  })

  it("matches collections and pages by the start of their words, never scattered letters", async () => {
    renderMenu()
    await screen.findByRole("group", { name: "From Google" })
    await type("dri")
    await waitFor(() =>
      expect(headings()).toEqual(["Records", "Collections", "Search"])
    )
    expect(screen.getByRole("option", { name: /Drive files/ })).toBeTruthy()
    await type("data")
    await waitFor(() => expect(headings()).toContain("Pages"))
    expect(screen.getByRole("option", { name: "All data" })).toBeTruthy()
    // "lisb" is not in "Drive files", however its letters are spread
    await type("lisb")
    await waitFor(() => expect(headings()).not.toContain("Collections"))
  })

  it("keeps the door to the Search page last", async () => {
    renderMenu()
    await type("tas")
    await screen.findByRole("group", { name: "Records" })
    const options = screen.getAllByRole("option")
    expect(options.at(-1)?.textContent).toContain(
      "Search your records for “tas”"
    )
  })
})
