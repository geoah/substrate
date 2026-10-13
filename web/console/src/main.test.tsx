// @vitest-environment jsdom
/** The entry point installs the stale-chunk reload on the page's window
 * before anything renders: a lazy chunk can fail as soon as the first page
 * asks for one. Rendering and the route tree are stubbed out; only the
 * wiring is under test. */

import { expect, it, vi } from "vitest"

const reloadOnStaleChunk = vi.hoisted(() => vi.fn())
vi.mock("@/lib/stale-chunks", () => ({ reloadOnStaleChunk }))
vi.mock("@/router", () => ({ router: { navigate: vi.fn() } }))
vi.mock("react-dom/client", () => ({
  createRoot: () => ({ render: vi.fn() }),
}))

it("reloads the page's window on a stale chunk", async () => {
  await import("./main")
  expect(reloadOnStaleChunk).toHaveBeenCalledWith(window)
})
