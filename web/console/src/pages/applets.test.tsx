// @vitest-environment jsdom
/** The Applets page: each applet a card that opens it in the shell or on its own,
 * and, with none, a way to ask Moss for the first. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { saveSession } from "@/lib/api/session"

import { AppletsPage } from "./applets"

function serve(records: unknown[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const url = new URL(String(input), "http://localhost")
      if (url.pathname === "/api/v1/records") {
        expect(JSON.parse(url.searchParams.get("filter") ?? "{}")).toEqual({
          kinds: ["substrate.reamde.dev/applets/applet"],
        })
        return Response.json({ records })
      }
      return Response.json({})
    })
  )
}

function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const rootRoute = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>
        <AppletsPage />
      </QueryClientProvider>
    ),
  })
  const stubs = ["/applets/$id", "/a/$id", "/agents"].map((path) =>
    createRoute({
      getParentRoute: () => rootRoute,
      path,
      component: () => null,
    })
  )
  const router = createRouter({
    routeTree: rootRoute.addChildren(stubs),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
}

beforeEach(() => saveSession("secret", "ada.example.com", "token-1"))
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("the Applets page", () => {
  it("lists each applet with a way into it, in the shell and on its own", async () => {
    serve([
      {
        id: "books",
        kind: "substrate.reamde.dev/applets/applet",
        version: 1,
        properties: {
          name: "Books",
          description: "What I'm reading.",
          icon: "book-open",
        },
        labels: {},
        createdAt: "2026-11-08T09:00:00Z",
        updatedAt: "2026-11-08T09:00:00Z",
      },
    ])
    mount()
    const open = await screen.findByRole("link", { name: /Books/ })
    expect(open.getAttribute("href")).toBe("/applets/books")
    expect(open.textContent).toContain("What I'm reading.")
    expect(
      screen
        .getByRole("link", { name: /Open full screen/ })
        .getAttribute("href")
    ).toBe("/a/books")
  })

  it("suggests asking Moss when there are none", async () => {
    serve([])
    mount()
    expect(await screen.findByText("No applets yet")).toBeTruthy()
    expect(screen.getByText(/make me an applet for my books/)).toBeTruthy()
    expect(
      screen.getByRole("link", { name: /Ask Moss/ }).getAttribute("href")
    ).toContain("/agents?prompt=")
  })
})
