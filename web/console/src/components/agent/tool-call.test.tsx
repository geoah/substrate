/** The tool card's one navigational promise: a settled `propose` did NOT change
 * the graph, it landed a row somebody has to decide, so the card carries the
 * proposal — its live state, and the way to the full review. Everything else
 * about the card (its payloads, its running state) is rendering; this is the
 * link a reader would otherwise have to go hunting the queue for. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
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

import type { SubstrateRecord } from "@/lib/api/types"
import type { ToolCallView } from "@/lib/api/transcript"
import { ToolCallCard } from "./tool-call"

function call(over: Partial<ToolCallView> = {}): ToolCallView {
  return {
    id: "c1",
    name: "propose",
    arguments: '{"kind":"crew.test.dev/crew/widget","target":"w-1"}',
    output: '{"id":"cr7abc4def6k"}',
    ok: true,
    ...over,
  }
}

/** The card resolves the request it links, so the tests seed the query cache
 * with the row — the card then renders its live state without a network. */
function renderCard(view: ToolCallView, request?: SubstrateRecord) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  if (request) {
    client.setQueryData(
      [
        "record",
        "substrate.reamde.dev",
        "core",
        "recordpatchrequest",
        request.id,
      ],
      request
    )
  }
  return render(
    <QueryClientProvider client={client}>
      <ToolCallCard call={view} />
    </QueryClientProvider>
  )
}

const request: SubstrateRecord = {
  id: "cr7abc4def6k",
  kind: "substrate.reamde.dev/core/recordpatchrequest",
  properties: {
    op: "patch",
    decision: "proposed",
    rationale: "tidy",
    diff: { properties: { name: "better" } },
  },
  labels: {},
  version: 1,
  createdAt: "2026-08-13T00:00:00Z",
  updatedAt: "2026-08-13T00:00:00Z",
}

afterEach(cleanup)

/** The router's Link is stubbed to an anchor carrying its route and params, so
 * the assertion is about WHERE the card points, not how the router renders. */
function reviewLink(container: HTMLElement): HTMLAnchorElement | null {
  return container.querySelector('a[data-to="/change-requests/$id"]')
}

describe("the tool card", () => {
  it("carries a settled propose's request: live state, decision, review link", () => {
    const { container } = renderCard(call(), request)
    // The live state off the resolved row: op and decision.
    expect(screen.getByText("proposed")).toBeTruthy()
    expect(screen.getByText("tidy")).toBeTruthy()
    // The change itself renders INLINE: the property and its proposed value,
    // not just a link to go find out.
    expect(screen.getByText("name")).toBeTruthy()
    expect(screen.getByText(/better/)).toBeTruthy()
    expect(screen.getByText("Accept")).toBeTruthy()
    expect(screen.getByText("Reject")).toBeTruthy()
    const link = reviewLink(container)
    expect(link?.textContent).toContain("Review the full change")
    expect(JSON.parse(link?.getAttribute("data-params") ?? "{}")).toEqual({
      id: "cr7abc4def6k",
    })
  })

  it("withholds the verdict buttons once the request is decided", () => {
    renderCard(call(), {
      ...request,
      properties: { ...request.properties, decision: "accepted" },
    })
    expect(screen.getByText("accepted")).toBeTruthy()
    expect(screen.queryByText("Accept")).toBeNull()
    expect(screen.queryByText("Reject")).toBeNull()
  })

  it("prefers the engine-stamped request id over the payload sniff", () => {
    const { container } = renderCard(
      call({
        name: "file",
        output: "created it",
        changes: [
          {
            seq: 4,
            op: "put",
            kind: "substrate.reamde.dev/core/recordpatchrequest",
            id: "cr7abc4def6k",
          },
        ],
      }),
      request
    )
    expect(reviewLink(container)).toBeTruthy()
  })

  it("offers no proposal while the call is still out, or when it failed", () => {
    const running = renderCard(call({ ok: undefined, output: undefined }))
    expect(reviewLink(running.container)).toBeNull()
    cleanup()

    const failed = renderCard(
      call({ ok: false, output: '{"error":"refused"}' })
    )
    expect(reviewLink(failed.container)).toBeNull()
  })

  it("offers no proposal for another tool, whatever its payload says", () => {
    const { container } = renderCard(call({ name: "query" }))
    expect(reviewLink(container)).toBeNull()
    // The card itself still renders: only the proposal is withheld.
    expect(screen.getByText("query")).toBeTruthy()
  })

  it("renders a mutate's stamped changes as op badge + record pill rows", () => {
    const { container } = renderCard(
      call({
        name: "mutate",
        output: '{"data":{"patch":{"id":"w1"}}}',
        changes: [
          {
            seq: 202,
            op: "patch",
            kind: "crew.test.dev/crew/widget",
            id: "w1",
          },
        ],
      })
    )
    // The op, as the change-request voice's badge.
    expect(screen.getByText("patch")).toBeTruthy()
    // The record, as the pill: one link straight to the moved record.
    const pill = container.querySelector(
      'a[data-to="/data/$authority/$pkg/$name/$id"]'
    )
    expect(JSON.parse(pill?.getAttribute("data-params") ?? "{}")).toEqual({
      authority: "crew.test.dev",
      pkg: "crew",
      name: "widget",
      id: "w1",
    })
    // The seq stays addressable for a reader who wants the exact entry.
    expect(screen.getByText("seq 202")).toBeTruthy()
  })
})
