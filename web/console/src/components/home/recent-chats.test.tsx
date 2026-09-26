// @vitest-environment jsdom
/** Home's recent chats: each conversation titled by what opened it, with the
 * agent and when, and a row opens that chat. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { RecentChats } from "./recent-chats"
import {
  conversationsQueryOptions,
  openingMessagesQueryOptions,
} from "@/lib/api/agents"
import type { Page, SubstrateRecord } from "@/lib/api/types"

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.reject(new Error("offline")))
  )
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const LLM = "substrate.reamde.dev/llm"

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  const at = new Date(Date.now() - 5 * 60_000).toISOString()
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: at,
    updatedAt: at,
  }
}

function page(records: SubstrateRecord[]): Page {
  return { records, head: 1, generation: "g" }
}

function renderChats(threads: SubstrateRecord[], messages: SubstrateRecord[]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(conversationsQueryOptions(3).queryKey, page(threads))
  client.setQueryData(
    openingMessagesQueryOptions(threads.map((t) => t.id)).queryKey,
    page(messages)
  )
  const rootRoute = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>
        <RecentChats />
      </QueryClientProvider>
    ),
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/agents",
        component: () => null,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  return render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
}

describe("RecentChats", () => {
  it("titles a chat by its opening message and opens it", async () => {
    renderChats(
      [
        record(`${LLM}/thread`, "th1", {
          mode: "chat",
          agent: {
            ref: "substrate.reamde.dev/core/agent/ada.example.com/notes/titler",
          },
        }),
      ],
      [
        record(`${LLM}/message`, "m1", {
          role: "user",
          thread: { ref: `${LLM}/thread/th1` },
          content: "Plan the Lisbon offsite agenda",
        }),
      ]
    )
    const title = await screen.findByText("Plan the Lisbon offsite agenda")
    const link = title.closest("a")
    expect(link?.getAttribute("href")).toBe("/agents?thread=th1")
    expect(link?.textContent).toContain("with Titler")
    expect(link?.textContent).toContain("5m ago")
  })

  it("says so when there are no chats yet", async () => {
    renderChats([], [])
    expect(await screen.findByText(/No chats yet/)).toBeTruthy()
  })
})
