// @vitest-environment jsdom
/** The suggestion card's server paths, each pinned to the kind-name segment
 * (decision 0033): the live-target read behind the before → after words and
 * the decision patch Apply and Dismiss send. The mock answers ONLY at those
 * paths, so a component that routed by anything else would render neither
 * side. The card offers no standing rule: an allow cannot outrank the gate
 * that held the write, so "Always allow this" stays out until the engine can
 * say an exception. A delete takes a second press. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params: linkParams,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-params={JSON.stringify(linkParams ?? {})} {...rest}>
      {children}
    </a>
  ),
}))

import { ProposalCard } from "./proposal-card"

const TASK_KIND = "samples.substrate.reamde.dev/tasks/task"
const REQUEST_PATH = "/api/v1/substrate.reamde.dev/core/recordpatchrequest/cr-1"
const TARGET_PATH = "/api/v1/samples.substrate.reamde.dev/tasks/task/task-1"

const KINDS: KindInfo[] = [
  {
    identity: TASK_KIND,
    name: "task",
    authority: "samples.substrate.reamde.dev",
    package: "tasks",
    version: 1,
    source: "installed",
    description: "",
    definition: {
      properties: {
        summary: { type: "string" },
        priority: {
          type: "enum",
          values: [
            { value: "high", label: "High" },
            { value: "urgent", label: "Urgent" },
          ],
        },
      },
    },
  },
]

function record(over: Partial<SubstrateRecord>): SubstrateRecord {
  return {
    id: "",
    kind: "",
    properties: {},
    labels: {},
    version: 1,
    createdAt: "2026-08-14T00:00:00Z",
    updatedAt: "2026-08-14T00:00:00Z",
    ...over,
  }
}

const gatedRequest = record({
  id: "cr-1",
  kind: "substrate.reamde.dev/core/recordpatchrequest",
  version: 4,
  properties: {
    rationale: "The summary moved in the source.",
    target: { ref: `${TASK_KIND}/task-1` },
    diff: { properties: { summary: "New summary", priority: "urgent" } },
    // Served shape: a reference reads back as `{ref}`, which is what marks
    // the request as held by a policy.
    policy: { ref: "substrate.reamde.dev/core/recordpatchpolicy/gate-1" },
    thread: { ref: "substrate.reamde.dev/llm/thread/th-1" },
  },
})

const target = record({
  id: "task-1",
  kind: TASK_KIND,
  version: 3,
  properties: { summary: "Old summary", priority: "high" },
})

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

function renderCard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ProposalCard id="cr-1" />
    </QueryClientProvider>
  )
}

/** The kinds a records-route URL lists, read off its `filter`: the list route
 * is one path for every kind, so a stub dispatches on this, not the path. */
function listedKinds(path: string): string[] {
  const url = new URL(path, "http://x")
  if (url.pathname !== "/api/v1/records") return []
  const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
    kinds?: string[]
  }
  return filter.kinds ?? []
}

describe("ProposalCard", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    fetchMock.mockImplementation(async (url, init) => {
      const method = (init as RequestInit | undefined)?.method ?? "GET"
      const path = String(url)
      if (listedKinds(path).includes("substrate.reamde.dev/core/kind")) {
        return jsonResponse(200, { kinds: KINDS })
      }
      if (path === REQUEST_PATH) return jsonResponse(200, gatedRequest)
      if (path === TARGET_PATH) return jsonResponse(200, target)
      if (path === REQUEST_PATH && method === "PATCH") {
        return jsonResponse(200, gatedRequest)
      }
      return jsonResponse(404, {
        error: { code: "not_found", message: `no route for ${path}` },
      })
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("says the change in the record page's words, reading the live target", async () => {
    renderCard()

    // Before → after, the before being the live target read.
    expect(await screen.findByText("Old summary")).toBeTruthy()
    expect(screen.getByText("New summary")).toBeTruthy()
    expect(screen.getByText("Summary")).toBeTruthy()
    // An enum reads by its authored label, never the stored word.
    expect(await screen.findByText("Urgent")).toBeTruthy()
    expect(screen.getByText("High")).toBeTruthy()
    expect(screen.queryByText("urgent")).toBeNull()
    expect(screen.getByText("The summary moved in the source.")).toBeTruthy()
    expect(fetchMock.mock.calls.map(([url]) => String(url))).toContain(
      TARGET_PATH
    )
    // Review opens the page; the standing rule is not offered.
    expect(
      screen.getByText("Review").closest('a[data-to="/change-requests/$id"]')
    ).toBeTruthy()
    expect(screen.queryByText("Edit first")).toBeNull()
    expect(screen.queryByText("Always allow this")).toBeNull()
  })

  it("applies and dismisses with the CAS'd decision patch", async () => {
    renderCard()
    fireEvent.click(await screen.findByRole("button", { name: "Dismiss" }))
    await waitFor(() => {
      const patch = fetchMock.mock.calls.find(
        ([, init]) => (init as RequestInit | undefined)?.method === "PATCH"
      )
      expect(String(patch![0])).toBe(REQUEST_PATH)
      expect(JSON.parse((patch![1] as RequestInit).body as string)).toEqual({
        properties: { decision: "rejected" },
        ifVersion: 4,
      })
    })
  })

  it("deletes only on a second press, saying what it costs", async () => {
    const deleting = {
      ...gatedRequest,
      properties: {
        op: "delete",
        target: { ref: `${TASK_KIND}/task-1` },
        rationale: "A duplicate.",
      },
    }
    fetchMock.mockImplementation(async (url) => {
      const path = String(url)
      if (listedKinds(path).includes("substrate.reamde.dev/core/kind")) {
        return jsonResponse(200, { kinds: KINDS })
      }
      if (path === REQUEST_PATH) return jsonResponse(200, deleting)
      return jsonResponse(404, { error: { code: "not_found", message: "" } })
    })
    renderCard()
    const button = await screen.findByRole("button", { name: "Delete it" })
    expect(screen.getByText(/History keeps what it was/)).toBeTruthy()
    fireEvent.click(button)
    // Asked, not sent.
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Delete this record?")
    expect(
      fetchMock.mock.calls.some(
        ([, init]) => (init as RequestInit | undefined)?.method === "PATCH"
      )
    ).toBe(false)
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete it" }))
    await waitFor(() => {
      const patch = fetchMock.mock.calls.find(
        ([, init]) => (init as RequestInit | undefined)?.method === "PATCH"
      )
      expect(JSON.parse((patch![1] as RequestInit).body as string)).toEqual({
        properties: { decision: "accepted" },
        ifVersion: 4,
      })
    })
  })

  it("names a new record by the values a create proposes", async () => {
    fetchMock.mockImplementation(async (url) => {
      if (String(url) === REQUEST_PATH) {
        return jsonResponse(
          200,
          record({
            id: "cr-1",
            kind: "substrate.reamde.dev/core/recordpatchrequest",
            properties: {
              op: "create",
              targetKind: TASK_KIND,
              targetId: "statuspage",
              diff: {
                properties: {
                  summary: "Own the status page",
                  priority: "high",
                },
              },
            },
          })
        )
      }
      return jsonResponse(404, { error: { code: "not_found", message: "" } })
    })
    renderCard()
    expect(await screen.findByText("New task")).toBeTruthy()
    expect(screen.getByText("Own the status page")).toBeTruthy()
    expect(screen.getByText("Priority")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Add it" })).toBeTruthy()
  })
})
