// @vitest-environment jsdom
/** Unsaved ink holds a move to another page until the person answers: Keep
 * stays put, Discard goes on. With nothing unsaved the move is not held. */

import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router"
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { useDiscardGuard } from "./discard-guard"

function Drawing({ unsaved }: { unsaved: boolean }) {
  const { guard } = useDiscardGuard(unsaved, {
    title: "Discard this note?",
    consequence: "Nothing has been saved, so the drawing goes.",
  })
  return (
    <>
      <p>drawing</p>
      {guard}
    </>
  )
}

function renderAt(unsaved: boolean) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => <Drawing unsaved={unsaved} />,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/elsewhere",
      component: () => <p>elsewhere</p>,
    }),
  ])
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  // The app registers ITS router type globally; this is another router of
  // the same shape.
  render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
  return router
}

afterEach(cleanup)

describe("useDiscardGuard", () => {
  it("holds a move away from unsaved ink until it is answered", async () => {
    const router = renderAt(true)
    await screen.findByText("drawing")

    // The move waits on the answer, so nothing here waits on the move.
    act(() => router.history.push("/elsewhere"))
    const ask = await screen.findByRole("dialog", {
      name: "Discard this note?",
    })
    fireEvent.click(within(ask).getByRole("button", { name: "Cancel" }))
    expect(await screen.findByText("drawing")).toBeTruthy()
    expect(router.state.location.pathname).toBe("/")

    act(() => router.history.push("/elsewhere"))
    const again = await screen.findByRole("dialog", {
      name: "Discard this note?",
    })
    fireEvent.click(within(again).getByRole("button", { name: "Discard" }))
    expect(await screen.findByText("elsewhere")).toBeTruthy()
  })

  it("lets a move go with nothing unsaved", async () => {
    const router = renderAt(false)
    await screen.findByText("drawing")
    act(() => router.history.push("/elsewhere"))
    expect(await screen.findByText("elsewhere")).toBeTruthy()
    expect(screen.queryByRole("dialog")).toBeNull()
  })
})
