/** The tool card's one navigational promise: a settled `propose` did NOT change
 * the graph, it landed a row somebody has to decide, so the card carries the
 * way to that row. Everything else about the card (its payloads, its running
 * state) is rendering; this is the link a reader would otherwise have to go
 * hunting the queue for. */

import { cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-params={JSON.stringify(params ?? {})} {...rest}>
      {children}
    </a>
  ),
}))

import type { ToolCallView } from "@/lib/api/transcript"
import { ToolCallCard } from "./tool-call"

function call(over: Partial<ToolCallView> = {}): ToolCallView {
  return {
    id: "c1",
    name: "propose",
    arguments: '{"kind":"crew.test.dev/widget","target":"w-1"}',
    output: '{"id":"cr7abc4def6k"}',
    ok: true,
    ...over,
  }
}

afterEach(cleanup)

/** The router's Link is stubbed to an anchor carrying its route and params, so
 * the assertion is about WHERE the card points, not how the router renders. */
function reviewLink(container: HTMLElement): HTMLAnchorElement | null {
  return container.querySelector("a[data-to]")
}

describe("the tool card", () => {
  it("links a settled propose at the change request it landed", () => {
    const { container } = render(<ToolCallCard call={call()} />)
    const link = reviewLink(container)
    expect(link?.textContent).toContain("Review the proposed change")
    expect(link?.getAttribute("data-to")).toBe("/change-requests/$id")
    expect(JSON.parse(link?.getAttribute("data-params") ?? "{}")).toEqual({
      id: "cr7abc4def6k",
    })
  })

  it("offers no link while the call is still out, or when it failed", () => {
    const running = render(
      <ToolCallCard call={call({ ok: undefined, output: undefined })} />
    )
    expect(reviewLink(running.container)).toBeNull()
    cleanup()

    const failed = render(
      <ToolCallCard call={call({ ok: false, output: '{"error":"refused"}' })} />
    )
    expect(reviewLink(failed.container)).toBeNull()
  })

  it("offers no link for another tool, whatever its payload says", () => {
    const { container } = render(
      <ToolCallCard call={call({ name: "query" })} />
    )
    expect(reviewLink(container)).toBeNull()
    // The card itself still renders: only the link is withheld.
    expect(screen.getByText("query")).toBeTruthy()
  })
})
