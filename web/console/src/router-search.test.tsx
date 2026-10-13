// @vitest-environment jsdom
/** The router JSON-parses every search value, so `?q=2024` reaches a route's
 * validateSearch as the number 2024. The /search and provider validators kept
 * only strings: they dropped the number, the router rewrote the address
 * without it, and a search for a year or a digit-only account id was gone
 * after a reload. The old `/agents/$id` redirect skipped a digit-only thread
 * the same way. Each mounted case runs the console's own validators under
 * browser history, reads the keys the way the pages do (nuqs), and reloads
 * the address the router settled on. */

import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router"
import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { parseAsString, useQueryStates } from "nuqs"
import { afterEach, describe, expect, it } from "vitest"

import {
  agentRoute,
  agentsRoute,
  providerRoute,
  router,
  searchRoute,
} from "@/router"

function Probe({ keys }: { keys: string[] }) {
  const [values] = useQueryStates(
    Object.fromEntries(keys.map((k) => [k, parseAsString]))
  )
  return <p data-testid="probe">{JSON.stringify(values)}</p>
}

/** A router over the three routes, each carrying the console route's own
 * validateSearch, opened at `href`. */
function open(href: string) {
  window.history.replaceState(null, "", href)
  const root = createRootRoute({ component: Outlet })
  const probe = createRouter({
    routeTree: root.addChildren([
      createRoute({
        getParentRoute: () => root,
        path: "/search",
        validateSearch: searchRoute.options.validateSearch,
        component: () => <Probe keys={["q", "mode", "kind"]} />,
      }),
      createRoute({
        getParentRoute: () => root,
        path: "/providers/$authority/$pkg",
        validateSearch: providerRoute.options.validateSearch,
        component: () => <Probe keys={["account"]} />,
      }),
      createRoute({
        getParentRoute: () => root,
        path: "/agents",
        validateSearch: agentsRoute.options.validateSearch,
        component: () => <Probe keys={["thread", "agent", "prompt", "send"]} />,
      }),
    ]),
    history: createBrowserHistory(),
    InnerWrap: router.options.InnerWrap,
  })
  render(<RouterProvider router={probe} />)
  return probe
}

/** Open `href`, wait for the router to settle the address, then open the
 * settled address again, as a reload does: the page must read `want` both
 * times. */
async function keeps(href: string, want: Record<string, string>) {
  const first = open(href)
  await waitFor(() => {
    expect(first.state.status).toBe("idle")
    expect(JSON.parse(screen.getByTestId("probe").textContent)).toMatchObject(
      want
    )
  })
  // The address the router left behind still carries every value, in
  // whichever spelling the router chose for it.
  const left = first.options.parseSearch(window.location.search)
  expect(
    Object.fromEntries(
      Object.entries(left).map(([k, v]) => [
        k,
        typeof v === "string" ? v : JSON.stringify(v),
      ])
    )
  ).toMatchObject(want)
  const settled = window.location.pathname + window.location.search
  cleanup()

  const reloaded = open(settled)
  await waitFor(() => {
    expect(reloaded.state.status).toBe("idle")
    expect(JSON.parse(screen.getByTestId("probe").textContent)).toMatchObject(
      want
    )
  })
}

afterEach(cleanup)

describe("search params the router parses as JSON", () => {
  it("keeps a digit-only search", async () => {
    await keeps("/search?q=2024", { q: "2024" })
  })

  it("keeps a search for the word true", async () => {
    await keeps("/search?q=true&mode=fts", { q: "true", mode: "fts" })
  })

  it("keeps a digit-only provider account", async () => {
    await keeps("/providers/a/b?account=12345", { account: "12345" })
  })

  it("carries a digit-only thread from the old per-agent address", () => {
    const beforeLoad = agentRoute.options.beforeLoad as (ctx: unknown) => void
    let thrown: unknown
    try {
      beforeLoad({ params: { id: "a1" }, search: { thread: 42 } })
    } catch (err) {
      thrown = err
    }
    expect((thrown as { options?: unknown }).options).toMatchObject({
      to: "/agents",
      search: { thread: "42" },
    })
  })
})

/** Regression guards for the /agents validator, which exists to type links
 * into the chat. The page reads these keys through nuqs, which passes the
 * raw values through when a route has no validator, so the cases also pass
 * without one. A strings-only validator loses `?send=1`, a digit-only thread
 * and a prompt written as JSON, and they fail on what the page reads. */
describe("the /agents validator", () => {
  it("keeps the agents chat's send flag beside its prompt", async () => {
    await keeps("/agents?agent=a1&prompt=hello&send=1", {
      agent: "a1",
      prompt: "hello",
      send: "1",
    })
  })

  it("keeps a prompt written as JSON", async () => {
    await keeps(
      "/agents?agent=a1&prompt=%7B%22task%22%3A%22recipes%22%7D&send=1",
      { prompt: '{"task":"recipes"}', send: "1" }
    )
  })

  it("keeps a digit-only thread", async () => {
    await keeps("/agents?thread=42", { thread: "42" })
  })
})
