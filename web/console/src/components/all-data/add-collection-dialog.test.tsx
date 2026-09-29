// @vitest-environment jsdom
/** Ask an agent hands the request to an agent that can declare a kind (the
 * one you last chatted with when it can) and has it sent, or says plainly
 * that none can. The Agents page it hands over to sends the question to that
 * agent and moves to the thread the run opens. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const navigate = vi.fn()

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({ children, to }: { children: ReactNode; to?: string }) => (
    <a href={to}>{children}</a>
  ),
}))

type ChatOpts = {
  agent: string
  thread?: string
  message: string
  onEvent: (event: AgentEvent) => void
}
const chats: ChatOpts[] = []

vi.mock("@/lib/api/agents", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/agents")>()
  return {
    ...actual,
    streamChat: (opts: ChatOpts) => {
      chats.push(opts)
      return { stop() {} }
    },
  }
})
// The Agents page's side columns read more than this needs.
vi.mock("@/components/agent/thread-list", () => ({ ThreadList: () => null }))
vi.mock("@/components/agent/agent-panel", () => ({ AgentPanel: () => null }))

import type { AgentEvent } from "@/lib/api/agents"
import { AgentsPage } from "@/pages/agents"
import { AddCollectionDialog } from "./add-collection-dialog"

const tool = (fn: string) => ({
  function: { ref: `substrate.reamde.dev/core/function/${fn}` },
})

function record(kind: string, id: string, properties: Record<string, unknown>) {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-09-25T10:00:00Z",
    updatedAt: "2026-09-25T10:00:00Z",
  }
}

const agent = (id: string, properties: Record<string, unknown>) =>
  record("substrate.reamde.dev/core/agent", id, {
    provider: { ref: "substrate.reamde.dev/llm/provider/openai" },
    ...properties,
  })

const thread = (id: string, mode: string, agentId: string) =>
  record("substrate.reamde.dev/llm/thread", id, {
    mode,
    agent: { ref: `substrate.reamde.dev/core/agent/${agentId}` },
  })

// Chat-capable, but it may only query: it cannot declare a kind.
const titler = agent("x.example.com/notes/titler", {
  tools: [tool("substrate.reamde.dev/core/query")],
  permissions: { reads: { kinds: ["*"] } },
})
// It may write kinds, but only by proposing, and an accepted proposal
// cannot land a kind record.
const proposer = agent("x.example.com/llm/proposer", {
  tools: [tool("substrate.reamde.dev/core/propose")],
  permissions: { writes: ["*"] },
})
const builder = agent("x.example.com/llm/builder", {
  tools: [tool("substrate.reamde.dev/core/write")],
  permissions: { writes: [{ ref: "substrate.reamde.dev/core/kind/*" }] },
})
const editor = agent("x.example.com/llm/editor", {
  tools: [tool("substrate.reamde.dev/core/write")],
  permissions: {
    writes: [
      { ref: "substrate.reamde.dev/core/kind/substrate.reamde.dev/core/kind" },
    ],
  },
})
const openai = record("substrate.reamde.dev/llm/provider", "openai", {
  apiKey: "********",
})

type Thread = ReturnType<typeof thread>

/** The threads a list read asks for: its `mode` condition and its page size,
 * applied the way the server applies them, newest first as given. */
function threadPage(url: URL, threads: Thread[]): Thread[] {
  const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
    properties?: { mode?: { eq?: string; in?: string[] } }
  }
  const mode = filter.properties?.mode
  const first = Number(url.searchParams.get("first") ?? 50)
  return threads
    .filter((t) => {
      const held = t.properties.mode as string
      if (mode?.eq !== undefined) return held === mode.eq
      if (mode?.in !== undefined) return mode.in.includes(held)
      return true
    })
    .slice(0, first)
}

/** Serves each list read by the kind it asks for, and a thread a run
 * minted, read on its own once the address names it. */
function serve({
  agents,
  threads = [],
  providers = [openai],
  minted,
}: {
  agents: unknown[]
  threads?: Thread[]
  providers?: unknown[]
  minted?: Thread
}) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const url = new URL(String(input), "http://console.test")
      if (minted && url.pathname.endsWith(`/llm/thread/${minted.id}`)) {
        return new Response(JSON.stringify(minted), { status: 200 })
      }
      const kinds = url.searchParams.get("filter") ?? ""
      const records = kinds.includes('"substrate.reamde.dev/core/agent"')
        ? agents
        : kinds.includes('"substrate.reamde.dev/llm/thread"')
          ? threadPage(url, threads)
          : kinds.includes('"substrate.reamde.dev/llm/provider"')
            ? providers
            : []
      return new Response(JSON.stringify({ records }), { status: 200 })
    })
  )
}

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function mount() {
  const onWayChange = vi.fn()
  render(
    <QueryClientProvider client={client()}>
      <AddCollectionDialog way="agent" onWayChange={onWayChange} />
    </QueryClientProvider>
  )
  return onWayChange
}

/** Types a request, presses Ask, and returns the address it handed over. */
async function ask(text: string): Promise<string> {
  const box = await screen.findByLabelText("What do you want to keep track of?")
  fireEvent.change(box, { target: { value: text } })
  fireEvent.click(screen.getByRole("button", { name: "Ask" }))
  const href = (navigate.mock.lastCall?.[0] as { href?: string } | undefined)
    ?.href
  if (!href) throw new Error("Ask navigated nowhere")
  return href
}

function openAgents(href: string) {
  const onUrlUpdate = vi.fn()
  render(
    <QueryClientProvider client={client()}>
      <NuqsTestingAdapter
        searchParams={href.slice(href.indexOf("?"))}
        hasMemory
        onUrlUpdate={onUrlUpdate}
      >
        <AgentsPage />
      </NuqsTestingAdapter>
    </QueryClientProvider>
  )
  return onUrlUpdate
}

describe("Add a collection: Ask an agent", () => {
  beforeEach(() => {
    navigate.mockReset()
    chats.length = 0
    vi.stubGlobal(
      "matchMedia",
      (query: string) =>
        ({
          matches: true,
          media: query,
          addEventListener: () => {},
          removeEventListener: () => {},
        }) as unknown as MediaQueryList
    )
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it("says so when none of the agents can set one up", async () => {
    serve({
      agents: [titler, proposer],
      threads: [thread("t-0", "chat", proposer.id)],
    })
    const onWayChange = mount()
    expect(
      await screen.findByText(
        "None of your agents can set up a collection yet. Start from a sample, or give an agent that permission."
      )
    ).toBeTruthy()
    expect(screen.queryByLabelText("What do you want to keep track of?")).toBe(
      null
    )
    expect(
      screen.getByRole("link", { name: "Go to Agents" }).getAttribute("href")
    ).toBe("/agents")
    fireEvent.click(screen.getByRole("button", { name: "Start from a sample" }))
    expect(onWayChange).toHaveBeenCalledWith("sample")
  })

  it("says the agents did not load rather than that none can", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              error: { code: "internal", message: "the database is down" },
            }),
            { status: 500 }
          )
      )
    )
    mount()
    expect(
      (await screen.findByRole("alert")).textContent?.includes(
        "the database is down"
      )
    ).toBe(true)
    expect(screen.queryByText(/None of your agents/)).toBe(null)
  })

  it("asks the agent that can, and has the question sent", async () => {
    serve({ agents: [titler, builder] })
    mount()
    await screen.findByLabelText("What do you want to keep track of?")
    expect(screen.getByText("Builder")).toBeTruthy()
    expect(await ask("My recipes")).toBe(
      "/agents?agent=x.example.com%2Fllm%2Fbuilder&prompt=My+recipes&send=1"
    )
  })

  it("passes over the agent you last chatted with when it cannot declare a kind", async () => {
    serve({
      agents: [titler, builder],
      threads: [thread("t-0", "chat", titler.id)],
    })
    mount()
    await screen.findByLabelText("What do you want to keep track of?")
    expect(screen.getByText("Builder")).toBeTruthy()
    expect(screen.queryByText("Titler")).toBe(null)
    expect(await ask("My recipes")).toContain(
      "agent=x.example.com%2Fllm%2Fbuilder"
    )
  })

  it("asks the agent you last chatted with when it can too, however many runs came after", async () => {
    // More triggered runs since the chat than the chat list's window holds.
    const runs = Array.from({ length: 120 }, (_, i) =>
      thread(`run-${i}`, "record", builder.id)
    )
    serve({
      agents: [builder, editor],
      threads: [...runs, thread("t-0", "chat", editor.id)],
    })
    mount()
    await screen.findByLabelText("What do you want to keep track of?")
    expect(screen.getByText("Editor")).toBeTruthy()
    expect(await ask("My recipes")).toContain(
      "agent=x.example.com%2Fllm%2Feditor"
    )
  })

  it("sends the question to that agent and opens the thread its run starts", async () => {
    serve({
      agents: [titler, builder],
      minted: thread("t-9", "chat", builder.id),
    })
    mount()
    const href = await ask("My recipes")
    cleanup()

    const onUrlUpdate = openAgents(href)
    await waitFor(() => expect(chats).toHaveLength(1))
    expect(chats[0].agent).toBe(builder.id)
    expect(chats[0].message).toBe("My recipes")
    // A new chat: the run opens the thread.
    expect(chats[0].thread).toBeUndefined()
    // Sent once: the address drops `send`, so a reload does not ask again.
    await waitFor(() =>
      expect(onUrlUpdate.mock.lastCall?.[0].queryString).toBe(
        "?agent=x.example.com/llm/builder&prompt=My+recipes"
      )
    )

    act(() => chats[0].onEvent({ kind: "thread", thread: "t-9" }))
    await waitFor(() =>
      expect(onUrlUpdate.mock.lastCall?.[0].queryString).toBe("?thread=t-9")
    )
    expect(chats).toHaveLength(1)
  })

  it("sends even when the agent's provider row is not there to say it has a key", async () => {
    serve({ agents: [builder], providers: [] })
    mount()
    const href = await ask("My recipes")
    cleanup()
    openAgents(href)
    await waitFor(() => expect(chats).toHaveLength(1))
    expect(chats[0].message).toBe("My recipes")
  })

  it("holds the question when the agent's provider has no key", async () => {
    serve({
      agents: [builder],
      providers: [record("substrate.reamde.dev/llm/provider", "openai", {})],
    })
    mount()
    const href = await ask("My recipes")
    cleanup()
    openAgents(href)
    expect(await screen.findByText(/This agent can’t answer yet/)).toBeTruthy()
    expect(chats).toHaveLength(0)
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(
      "My recipes"
    )
  })
})
