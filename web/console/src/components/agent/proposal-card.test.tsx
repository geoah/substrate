/** The gated card's three server paths, each pinned to the kind-name segment
 * (decision 0033 retired plurals from routing): the thread read that names the
 * proposer, the live-target read behind the before → after preview, and the
 * standing rule the accept-and-allow flow mints. The mock answers ONLY at the
 * singular paths, and the task kind carries a real plural (`tasks`) so a
 * component that routed by `.plural` would render neither side. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
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
const THREAD_PATH = "/api/v1/substrate.reamde.dev/core/llmthread/th-1"
const TARGET_PATH = "/api/v1/samples.substrate.reamde.dev/tasks/task/task-1"
const POLICY_PATH =
  "/api/v1/substrate.reamde.dev/core/recordpatchpolicy/allow-cr-1"

const KINDS: KindInfo[] = [
  {
    identity: TASK_KIND,
    name: "task",
    authority: "samples.substrate.reamde.dev",
    package: "tasks",
    version: 1,
    plural: "tasks",
    source: "installed",
    definition: { properties: { summary: { type: "string" } } },
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
    diff: { properties: { summary: "New summary" } },
    policy: "substrate.reamde.dev/core/recordpatchpolicy/gate-1",
    thread: "substrate.reamde.dev/core/llmthread/th-1",
  },
})

const thread = record({
  id: "th-1",
  kind: "substrate.reamde.dev/core/llmthread",
  properties: { agent: "substrate.reamde.dev/core/agent/scribe" },
})

const target = record({
  id: "task-1",
  kind: TASK_KIND,
  version: 3,
  properties: { summary: "Old summary" },
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

describe("ProposalCard", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    fetchMock.mockImplementation(async (url, init) => {
      const method = (init as RequestInit | undefined)?.method ?? "GET"
      const path = String(url)
      if (path.startsWith("/api/v1/substrate.reamde.dev/core/kind")) {
        return jsonResponse(200, { kinds: KINDS })
      }
      if (path === REQUEST_PATH) return jsonResponse(200, gatedRequest)
      if (path === THREAD_PATH) return jsonResponse(200, thread)
      if (path === TARGET_PATH) return jsonResponse(200, target)
      if (path === POLICY_PATH && method === "PUT") {
        return jsonResponse(200, record({ id: "allow-cr-1" }))
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

  it("reads the thread and the live target at their kind-name paths", async () => {
    renderCard()

    // The preview's before column is the live target read.
    expect(await screen.findByText("Old summary")).toBeTruthy()
    expect(screen.getByText("New summary")).toBeTruthy()
    // The remedy button exists only once the thread named the proposer.
    await screen.findByRole("button", { name: "Accept + always allow" })
    const got = fetchMock.mock.calls.map(([url]) => String(url))
    expect(got).toContain(THREAD_PATH)
    expect(got).toContain(TARGET_PATH)
  })

  it("mints the standing rule at the recordpatchpolicy segment", async () => {
    renderCard()
    fireEvent.click(
      await screen.findByRole("button", { name: "Accept + always allow" })
    )
    // The rule shown is the rule minted: this proposer, this kind, no wildcard.
    expect(await screen.findByText(/"scribe"/)).toBeTruthy()

    fireEvent.click(
      screen.getByRole("button", { name: "Mint the rule and accept" })
    )

    await waitFor(() => {
      const put = fetchMock.mock.calls.find(
        ([, init]) => (init as RequestInit | undefined)?.method === "PUT"
      )
      expect(put).toBeTruthy()
      expect(String(put![0])).toBe(POLICY_PATH)
      const patch = fetchMock.mock.calls.find(
        ([, init]) => (init as RequestInit | undefined)?.method === "PATCH"
      )
      expect(patch).toBeTruthy()
      expect(String(patch![0])).toBe(REQUEST_PATH)
      expect(JSON.parse((patch![1] as RequestInit).body as string)).toEqual({
        properties: { decision: "accepted" },
        ifVersion: 4,
      })
    })
  })
})
