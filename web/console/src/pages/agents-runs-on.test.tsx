// @vitest-environment jsdom
/** A conversation with an agent on `runsOn` asks for a key by what its next
 * turn runs on (decision record 0242): a new chat by the first entry that
 * can run, so a later entry with a key lets it start; a continued thread by
 * the row it is stamped with, whatever the other entries offer. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"

vi.mock("@/components/agent/conversation", () => ({
  Conversation: (props: { thread: string; keylessProvider?: string }) => (
    <div>
      <span data-testid="thread">{props.thread}</span>
      <span data-testid="keyless">{props.keylessProvider ?? "none"}</span>
    </div>
  ),
}))
vi.mock("@/components/agent/thread-list", () => ({ ThreadList: () => null }))
vi.mock("@/components/agent/agent-panel", () => ({ AgentPanel: () => null }))

import { AgentsPage } from "./agents"

const AGENT = "substrate.reamde.dev/core/agent"
const PROVIDER = "substrate.reamde.dev/llm/provider"
const MODEL = "substrate.reamde.dev/llm/model"
const THREAD = "substrate.reamde.dev/llm/thread"

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-11-01T10:00:00Z",
    updatedAt: "2026-11-01T10:00:00Z",
  }
}

const MODELS = [
  record(MODEL, "a:opus", {
    provider: { ref: `${PROVIDER}/anthropic` },
    model: "claude-opus-5-5",
  }),
  record(MODEL, "o:gpt-5", {
    provider: { ref: `${PROVIDER}/openai` },
    model: "gpt-5",
  }),
]

const THREADS = [
  record(THREAD, "t-1", {
    agent: { ref: `${AGENT}/helper` },
    mode: "chat",
    provider: "anthropic",
    model: "claude-opus-5-5",
  }),
]

let rows: SubstrateRecord[] = []

function kindsOf(url: string): string[] {
  const u = new URL(url, "http://x")
  if (u.pathname !== "/api/v1/records") return []
  const filter = JSON.parse(u.searchParams.get("filter") ?? "{}") as {
    kinds?: string[]
  }
  return filter.kinds ?? []
}

beforeEach(() => {
  rows = [
    record(PROVIDER, "anthropic", { label: "Anthropic" }),
    record(PROVIDER, "openai", { label: "OpenAI", apiKey: "••••" }),
  ]
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
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const url = String(input)
      if (url === "/api/v1/operator/defaults")
        return Response.json(
          { error: { code: "not_found", message: "no route" } },
          { status: 404 }
        )
      const kinds = kindsOf(url)
      const records = kinds.includes(AGENT)
        ? [
            record(AGENT, "helper", {
              runsOn: MODELS.map((m) => ({ ref: `${MODEL}/${m.id}` })),
            }),
          ]
        : kinds.includes(PROVIDER)
          ? rows
          : kinds.includes(MODEL)
            ? MODELS
            : kinds.includes(THREAD)
              ? THREADS
              : []
      return Response.json({ records })
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderPage(searchParams: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <NuqsTestingAdapter searchParams={searchParams} hasMemory>
        <AgentsPage />
      </NuqsTestingAdapter>
    </QueryClientProvider>
  )
}

describe("a conversation with an agent on runsOn", () => {
  it("lets a new chat start on a later entry with a key", async () => {
    renderPage("?agent=helper")
    // Once the records are read, the keyless first entry is passed over,
    // not asked for.
    await waitFor(() =>
      expect(
        vi
          .mocked(fetch)
          .mock.calls.some(([url]) => kindsOf(String(url)).includes(MODEL))
      ).toBe(true)
    )
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.getByTestId("keyless").textContent).toBe("none")
  })

  it("asks a new chat for the first entry's key where none can run", async () => {
    rows = [
      record(PROVIDER, "anthropic", { label: "Anthropic" }),
      record(PROVIDER, "openai", { label: "OpenAI" }),
    ]
    renderPage("?agent=helper")
    await waitFor(() =>
      expect(screen.getByTestId("keyless").textContent).toBe("anthropic")
    )
  })

  it("asks a continued thread for its own row's key, whatever a later entry offers", async () => {
    renderPage("?thread=t-1")
    await waitFor(() =>
      expect(screen.getByTestId("thread").textContent).toBe("t-1")
    )
    await waitFor(() =>
      expect(screen.getByTestId("keyless").textContent).toBe("anthropic")
    )
  })
})
