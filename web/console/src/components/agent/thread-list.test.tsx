// @vitest-environment jsdom
/** The chats column: agents sit above the chats, so a long history never
 * buries them; picking one narrows the chats; the chats show the most recent
 * page with "Show more"; the agents that only work for other agents are
 * listed under their own caption; technical mode prints each thread's tally. */

import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { ChatRow } from "@/lib/agent-chat"
import type { SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { ThreadList } from "./thread-list"
import { CHAT_PAGE, visibleChats } from "./visible-chats"

function record(id: string, properties: Record<string, unknown> = {}) {
  return {
    id,
    kind: "substrate.reamde.dev/core/agent",
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-09-20T10:00:00Z",
    updatedAt: "2026-09-20T10:00:00Z",
  } as unknown as SubstrateRecord
}

const HELPER = "ada.example.com/llm/helper"
const NOTES = "ada.example.com/notes/notekeeper"
const JUDGE = "ada.example.com/llm/judge"

function row(i: number, agentId: string, title = `Chat ${i}`): ChatRow {
  const at = new Date(Date.UTC(2026, 8, 20, 12, 0, 0) - i * 3600_000)
  return {
    thread: {
      ...record(`t${i}`, { startedAt: at.toISOString() }),
      kind: "substrate.reamde.dev/llm/thread",
    },
    title,
    agentId,
  }
}

// 30 chats with the helper, 3 with the note keeper, newest first.
const rows = [
  ...Array.from({ length: 30 }, (_, i) => row(i, HELPER)),
  row(40, NOTES, "Weekly notes"),
  row(41, NOTES, "Groceries"),
  row(42, NOTES, "Trip ideas"),
]

describe("visibleChats", () => {
  it("cuts at the limit and says how many it hid", () => {
    const v = visibleChats(rows, { agent: "", query: "", limit: CHAT_PAGE })
    expect(v.chats).toHaveLength(CHAT_PAGE)
    expect(v.chats[0].thread.id).toBe("t0")
    expect(v.matched).toBe(33)
    expect(v.more).toBe(13)
  })

  it("narrows to one agent's chats, then to the search", () => {
    const one = visibleChats(rows, { agent: NOTES, query: "", limit: 20 })
    expect(one.chats.map((r) => r.title)).toEqual([
      "Weekly notes",
      "Groceries",
      "Trip ideas",
    ])
    expect(one.more).toBe(0)
    const found = visibleChats(rows, {
      agent: NOTES,
      query: " TRIP ",
      limit: 20,
    })
    expect(found.chats.map((r) => r.title)).toEqual(["Trip ideas"])
    expect(
      visibleChats(rows, { agent: HELPER, query: "trip", limit: 20 }).matched
    ).toBe(0)
  })
})

function renderList(props: Partial<Parameters<typeof ThreadList>[0]> = {}) {
  const onAgent = vi.fn()
  const onNewChat = vi.fn()
  const onSelect = vi.fn()
  const rootRoute = createRootRoute({
    component: () => (
      <ThreadList
        rows={rows}
        loading={false}
        agents={[
          record(HELPER),
          record(NOTES),
          record(JUDGE, { hiddenFromChat: true }),
        ]}
        selected=""
        agent=""
        onAgent={onAgent}
        onSelect={onSelect}
        onNewChat={onNewChat}
        {...props}
      />
    ),
  })
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/data/$authority/$pkg/$name/$id",
      component: () => null,
    }),
  ])
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
  return { onAgent, onNewChat, onSelect }
}

afterEach(cleanup)

describe("ThreadList", () => {
  it("lists the agents above the chats, one line each", async () => {
    renderList()
    const agents = await screen.findByRole("navigation", { name: "Agents" })
    const chats = screen.getByRole("navigation", { name: "Chats" })
    expect(
      agents.compareDocumentPosition(chats) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    const all = within(agents).getByRole("button", { name: /All agents/ })
    expect(all.getAttribute("aria-current")).toBe("true")
    expect(all.textContent).toContain("33")
    expect(
      within(agents).getByRole("button", { name: /Helper/ }).textContent
    ).toContain("30")
  })

  it("shows the most recent chats and more on request", async () => {
    renderList()
    const chats = await screen.findByRole("navigation", { name: "Chats" })
    expect(
      within(chats).getAllByRole("button", { name: /^Chat \d+/ })
    ).toHaveLength(CHAT_PAGE)
    fireEvent.click(within(chats).getByRole("button", { name: /Show more/ }))
    expect(
      within(chats).getAllByRole("button", { name: /^Chat \d+/ })
    ).toHaveLength(30)
    expect(
      within(chats).queryByRole("button", { name: /Show more/ })
    ).toBeNull()
  })

  it("narrows to the picked agent, resets to all, and starts its chats", async () => {
    const { onAgent, onNewChat } = renderList({ agent: NOTES })
    const agents = await screen.findByRole("navigation", { name: "Agents" })
    fireEvent.click(within(agents).getByRole("button", { name: /Helper/ }))
    expect(onAgent).toHaveBeenLastCalledWith(HELPER)

    expect(screen.getByRole("heading").textContent).toBe(
      "Chats with Notekeeper"
    )
    const chats = screen.getByRole("navigation", { name: "Chats" })
    expect(
      within(chats)
        .getAllByRole("button")
        .map((b) => b.textContent)
        .filter((t) => !t?.startsWith("Show"))
    ).toHaveLength(3)

    fireEvent.click(screen.getByRole("button", { name: /New chat/ }))
    expect(onNewChat).toHaveBeenLastCalledWith(NOTES)
    fireEvent.click(
      screen.getByRole("button", { name: "Show every agent’s chats" })
    )
    expect(onAgent).toHaveBeenLastCalledWith("")
  })

  it("lists the agents that run on their own under a caption", async () => {
    renderList()
    const agents = await screen.findByRole("navigation", { name: "Agents" })
    expect(within(agents).getByText("Runs on its own")).toBeTruthy()
    expect(
      within(agents).queryByRole("button", { name: /Runs on its own/ })
    ).toBeNull()
    expect(
      within(agents).getByRole("link", { name: /Judge/ }).getAttribute("href")
    ).toBe(`/data/substrate.reamde.dev/core/agent/${encodeURIComponent(JUDGE)}`)
  })

  it("never dates a chat in the future", async () => {
    const ahead = new Date(Date.now() + 8 * 3600_000).toISOString()
    renderList({
      rows: [
        {
          thread: {
            ...record("tf", { startedAt: ahead }),
            kind: "substrate.reamde.dev/llm/thread",
          },
          title: "Skewed",
          agentId: HELPER,
        },
      ],
    })
    const chats = await screen.findByRole("navigation", { name: "Chats" })
    const chat = within(chats).getByRole("button", { name: /Skewed/ })
    expect(chat.textContent).toContain("just now")
    expect(chat.textContent).not.toMatch(/in \d/)
  })
})

describe("ThreadList in technical mode", () => {
  it("prints each thread's stored status and tally", async () => {
    renderTechnical()
    const chats = await screen.findByRole("navigation", { name: "Chats" })
    const chat = within(chats).getByRole("button", { name: /Costed/ })
    expect(chat.textContent).toContain("ok")
    expect(chat.textContent).toContain("2 turns")
    expect(chat.textContent).toContain("1,500 tokens")
    expect(chat.textContent).toContain("$0.0042")
  })
})

function renderTechnical() {
  const rootRoute = createRootRoute({
    component: () => (
      <ConsolePreferencesContext.Provider
        value={{
          preferences: {
            collapsed: [],
            favorites: [],
            sidebarOpen: true,
            ...DEFAULT_SETTINGS,
            technicalDetails: true,
          },
          busy: false,
          change: () => {},
          set: () => {},
        }}
      >
        <ThreadList
          rows={[
            {
              thread: {
                ...record("tc", {
                  startedAt: "2026-09-20T10:00:00Z",
                  status: "ok",
                  turns: 2,
                  totalTokens: 1500,
                  costUSD: 0.0042,
                }),
                kind: "substrate.reamde.dev/llm/thread",
              },
              title: "Costed",
              agentId: HELPER,
            },
          ]}
          loading={false}
          agents={[record(HELPER)]}
          selected=""
          agent=""
          onAgent={() => {}}
          onSelect={() => {}}
          onNewChat={() => {}}
        />
      </ConsolePreferencesContext.Provider>
    ),
  })
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
}
