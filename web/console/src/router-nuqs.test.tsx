// @vitest-environment jsdom
/** nuqs's TanStack Router adapter has to render inside the router: since
 * nuqs 2.10 its history spy reads the router from context, and mounted
 * around RouterProvider it got null and the whole console was a blank page
 * (`Cannot read properties of null (reading 'history')`). The console's
 * router carries the adapter as its InnerWrap; this renders a page that
 * reads a query parameter through that same wrap. */

import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
import { render, screen } from "@testing-library/react"
import { useQueryState } from "nuqs"
import { describe, expect, it } from "vitest"

import { router } from "@/router"

function Probe() {
  const [q] = useQueryState("q")
  return <p>q is {q ?? "unset"}</p>
}

describe("the console router's nuqs adapter", () => {
  it("is the router's InnerWrap, inside the router's context", () => {
    expect(router.options.InnerWrap).toBeTypeOf("function")
  })

  it("lets a page read a query parameter without a blank page", async () => {
    const probe = createRouter({
      routeTree: createRootRoute({ component: Probe }),
      history: createMemoryHistory({ initialEntries: ["/?q=hello"] }),
      InnerWrap: router.options.InnerWrap,
    })
    render(<RouterProvider router={probe} />)
    expect(await screen.findByText("q is hello")).toBeTruthy()
  })
})
