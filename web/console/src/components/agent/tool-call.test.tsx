// @vitest-environment jsdom
/** The tool line: what the call did in words, whether it worked, and what it
 * LANDED. A settled `propose` did not change anything — it landed a row
 * somebody has to decide — so the line carries the suggestion card with its
 * live state and its decisions. Technical mode names the function and shows
 * the payloads. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
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

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
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
function renderCard(
  view: ToolCallView,
  request?: SubstrateRecord,
  technical = false
) {
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
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
          technicalDetails: technical,
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>
        <ToolCallCard call={view} />
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
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

describe("the tool line", () => {
  it("carries a settled propose's suggestion: the change in words and its decisions", () => {
    const { container } = renderCard(call(), request)
    expect(screen.getByText("Suggested a change")).toBeTruthy()
    expect(screen.getByLabelText("Done")).toBeTruthy()
    // The change itself, in words: the property's label and its new value.
    expect(screen.getByText("tidy")).toBeTruthy()
    expect(screen.getByText("Name")).toBeTruthy()
    expect(screen.getByText("better")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Apply" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Dismiss" })).toBeTruthy()
    // Edit first is the full review of this very request.
    const link = reviewLink(container)
    expect(link?.textContent).toContain("Edit first")
    expect(JSON.parse(link?.getAttribute("data-params") ?? "{}")).toEqual({
      id: "cr7abc4def6k",
    })
  })

  it("withholds the decisions once the suggestion is decided", () => {
    const { container } = renderCard(call(), {
      ...request,
      properties: { ...request.properties, decision: "accepted" },
    })
    expect(screen.getByText("Accepted")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Apply" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull()
    expect(reviewLink(container)?.textContent).toBe("See the change")
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

  it("offers no suggestion while the call is still out, or when it failed", () => {
    const running = renderCard(call({ ok: undefined, output: undefined }))
    expect(reviewLink(running.container)).toBeNull()
    cleanup()

    const failed = renderCard(
      call({ ok: false, output: '{"error":"refused"}' })
    )
    expect(reviewLink(failed.container)).toBeNull()
    expect(screen.getByText("Didn’t work")).toBeTruthy()
    // Opened, it says why in words.
    fireEvent.click(screen.getByRole("button", { name: /Suggested a change/ }))
    expect(screen.getByText("It didn’t work: refused")).toBeTruthy()
  })

  it("offers no suggestion for another tool, whatever its payload says", () => {
    const { container } = renderCard(
      call({ name: "query", arguments: '{"q":"handover"}' })
    )
    expect(reviewLink(container)).toBeNull()
    expect(screen.getByText("Searched for “handover”")).toBeTruthy()
  })

  it("says what a write changed, the record as its mark", () => {
    const { container } = renderCard(
      call({
        name: "write",
        arguments:
          '{"op":"patch","kind":"crew.test.dev/crew/widget","id":"w1","input":{"properties":{"name":"w"}}}',
        output: '{"record":{"id":"w1","kind":"crew.test.dev/crew/widget"}}',
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
    expect(screen.getByText("Changed a widget")).toBeTruthy()
    expect(screen.getByText("Changed")).toBeTruthy()
    const mark = container.querySelector(
      'a[data-to="/data/$authority/$pkg/$name/$id"]'
    )
    expect(JSON.parse(mark?.getAttribute("data-params") ?? "{}")).toEqual({
      authority: "crew.test.dev",
      pkg: "crew",
      name: "widget",
      id: "w1",
    })
    // The seq is technical.
    expect(screen.queryByText(/seq 202/)).toBeNull()
  })

  it("names the function and shows the payloads in technical mode", () => {
    renderCard(
      call({
        name: "write",
        arguments: '{"op":"patch"}',
        output: '{"ok":1}',
        changes: [
          {
            seq: 202,
            op: "patch",
            kind: "crew.test.dev/crew/widget",
            id: "w1",
          },
        ],
      }),
      undefined,
      true
    )
    expect(screen.getByText("substrate.reamde.dev/core/write")).toBeTruthy()
    expect(screen.getByText("patch · seq 202")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: /Made a change/ }))
    expect(screen.getByText("Request")).toBeTruthy()
    expect(screen.getByText("Response")).toBeTruthy()
  })
})
