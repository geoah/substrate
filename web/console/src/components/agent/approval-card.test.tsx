// @vitest-environment jsdom
/** The approval card: a waiting call shows which agent asks, what would run
 * with exactly what, and Approve and Decline; approving sends the state
 * alone, against the version read. A decided call shows where it ended. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"

import { ApprovalCard } from "./approval-card"
import type { SubstrateRecord } from "@/lib/api/types"

const fetchMock = vi.fn<typeof fetch>()
beforeEach(() => vi.stubGlobal("fetch", fetchMock))
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

function approval(
  state: string,
  extra: Record<string, unknown> = {}
): SubstrateRecord {
  return {
    id: "a1",
    kind: "substrate.reamde.dev/llm/approval",
    properties: {
      state,
      tool: "gmail_send",
      agent: {
        ref: "substrate.reamde.dev/core/agent/substrate.reamde.dev/assistant/secretary",
      },
      function: {
        ref: "substrate.reamde.dev/core/function/providers.substrate.reamde.dev/google/sendmail",
      },
      arguments: {
        to: ["sam@example.com"],
        subject: "Dinner",
        body: "Friday at 8?",
      },
      ...extra,
    },
    labels: {},
    version: 4,
    createdAt: "2026-11-09T10:00:00Z",
    updatedAt: "2026-11-09T10:00:00Z",
  }
}

function serve(record: SubstrateRecord) {
  fetchMock.mockImplementation(async (input, init) => {
    const url = String(input)
    if (init?.method === "PATCH")
      return Response.json({ ...record, version: 5 })
    if (url.includes("/core/function/")) {
      return Response.json({
        id: "providers.substrate.reamde.dev/google/sendmail",
        kind: "substrate.reamde.dev/core/function",
        properties: {
          description: "Send mail as the owner; it cannot be unsent. More.",
        },
        labels: {},
        version: 1,
        createdAt: "2026-11-09T10:00:00Z",
        updatedAt: "2026-11-09T10:00:00Z",
      })
    }
    return Response.json(record)
  })
}

function renderCard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ApprovalCard id="a1" />
    </QueryClientProvider>
  )
}

it("shows what would run and approves with the state alone", async () => {
  serve(approval("pending"))
  renderCard()
  expect(await screen.findByText("Waiting for your approval")).toBeTruthy()
  expect(screen.getByText("gmail_send")).toBeTruthy()
  expect(screen.getByText(/Secretary wants to run/)).toBeTruthy()
  expect(
    await screen.findByText("Send mail as the owner; it cannot be unsent.")
  ).toBeTruthy()
  expect(screen.getByText("To")).toBeTruthy()
  expect(screen.getByText("sam@example.com")).toBeTruthy()
  expect(screen.getByText("Friday at 8?")).toBeTruthy()

  fireEvent.click(screen.getByRole("button", { name: "Approve" }))
  await waitFor(() =>
    expect(
      fetchMock.mock.calls.some(([, init]) => init?.method === "PATCH")
    ).toBe(true)
  )
  const [url, init] = fetchMock.mock.calls.find(
    ([, i]) => i?.method === "PATCH"
  )!
  expect(String(url)).toContain("/api/v1/substrate.reamde.dev/llm/approval/a1")
  expect(JSON.parse(String(init?.body))).toEqual({
    properties: { state: "approved" },
    ifVersion: 4,
  })
})

it("declines with the state alone", async () => {
  serve(approval("pending"))
  renderCard()
  fireEvent.click(await screen.findByRole("button", { name: "Decline" }))
  await waitFor(() =>
    expect(
      fetchMock.mock.calls.some(([, init]) => init?.method === "PATCH")
    ).toBe(true)
  )
  const [, init] = fetchMock.mock.calls.find(([, i]) => i?.method === "PATCH")!
  expect(JSON.parse(String(init?.body))).toEqual({
    properties: { state: "denied" },
    ifVersion: 4,
  })
})

it("shows where a decided call ended, and offers nothing", async () => {
  serve(approval("failed", { error: "google answered HTTP 403" }))
  renderCard()
  expect(await screen.findByText("Approved, but it didn’t work")).toBeTruthy()
  expect(screen.getByText("google answered HTTP 403")).toBeTruthy()
  expect(screen.queryByRole("button", { name: "Approve" })).toBeNull()
})
