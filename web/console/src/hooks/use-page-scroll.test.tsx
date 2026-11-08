// @vitest-environment jsdom
/** The shell's page container across route changes, under a real router: a
 * link opens the next page at the top even when it draws at once (nothing
 * short for the browser to clamp the offset to), a tab or a filter keeps the
 * page where it was, and back returns to where the reader left. */

import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router"
import { act, cleanup, render, screen } from "@testing-library/react"
import { useRef } from "react"
import { afterEach, describe, expect, it } from "vitest"

import { usePageScroll } from "./use-page-scroll"

function Shell() {
  const ref = useRef<HTMLDivElement>(null)
  usePageScroll(ref)
  return (
    <div ref={ref} data-testid="page">
      <Outlet />
    </div>
  )
}

async function open(href: string) {
  const root = createRootRoute({ component: Shell })
  const router = createRouter({
    routeTree: root.addChildren(
      ["/home", "/history", "/settings"].map((path) =>
        createRoute({
          getParentRoute: () => root,
          path,
          component: () => <h1>{path}</h1>,
        })
      )
    ),
    history: createMemoryHistory({ initialEntries: [href] }),
  })
  render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
  await screen.findByRole("heading")
  const page = screen.getByTestId("page")
  return {
    page,
    go: (
      to: string,
      opts: { search?: Record<string, string>; hash?: string } = {}
    ) => act(() => router.navigate({ to, ...opts })),
    back: () =>
      act(async () => {
        const settled = new Promise<void>((resolve) => {
          const off = router.subscribe("onRendered", () => {
            off()
            resolve()
          })
        })
        router.history.back()
        await settled
      }),
  }
}

afterEach(cleanup)

describe("usePageScroll", () => {
  it("opens the next page at the top", async () => {
    const { page, go } = await open("/home")
    page.scrollTop = 900
    await go("/history")
    expect(screen.getByRole("heading").textContent).toBe("/history")
    expect(page.scrollTop).toBe(0)
  })

  it("keeps the page where it was when only the search params change", async () => {
    const { page, go } = await open("/home")
    page.scrollTop = 900
    await go("/home", { search: { tab: "definition" } })
    expect(page.scrollTop).toBe(900)
  })

  it("returns to where the reader left on back", async () => {
    const { page, go, back } = await open("/home")
    page.scrollTop = 900
    await go("/history")
    page.scrollTop = 300
    await go("/settings")
    expect(page.scrollTop).toBe(0)

    await back()
    expect(screen.getByRole("heading").textContent).toBe("/history")
    expect(page.scrollTop).toBe(300)
    await back()
    expect(page.scrollTop).toBe(900)
  })

  it("leaves an address with a hash to whoever scrolls to its element", async () => {
    const { page, go } = await open("/home")
    page.scrollTop = 900
    await go("/settings", { hash: "verify-domain" })
    expect(page.scrollTop).toBe(900)
  })
})
