// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { clearSession, saveSession } from "@/lib/api/session"

import { AgentSpendCap } from "./spend-cap"

const MOSS = "substrate.reamde.dev/assistant/moss"
const MOSS_REF = `substrate.reamde.dev/core/agent/${MOSS}`
const BUDGET_PATH = "/api/v1/substrate.reamde.dev/llm/agentbudget/"

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status })
}

function budget(id: string, cents: number) {
  return {
    kind: "substrate.reamde.dev/llm/agentbudget",
    id,
    version: 1,
    labels: {},
    createdAt: "",
    updatedAt: "",
    properties: { agent: { ref: MOSS_REF }, spendCentsPerDay: cents },
  }
}

describe("AgentSpendCap", () => {
  const fetchMock = vi.fn<typeof fetch>()
  let stored: ReturnType<typeof budget> | null
  let failNext: boolean
  const reads: string[] = []
  const writes: { method: string; path: string; body?: unknown }[] = []

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "geoah", "tok-1")
    stored = null
    failNext = false
    reads.length = 0
    writes.length = 0
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      const path = String(url)
      const body = init?.body ? JSON.parse(String(init.body)) : undefined
      if (path.startsWith("/api/v1/records") && method === "GET") {
        reads.push(path)
        return json(200, { records: stored ? [stored] : [] })
      }
      if (failNext) {
        failNext = false
        writes.push({ method, path, body })
        return json(500, { error: { code: "internal", message: "boom" } })
      }
      if (path === "/api/v1/records" && method === "POST") {
        writes.push({ method, path, body })
        stored = budget("minted", body.properties.spendCentsPerDay)
        return json(200, stored)
      }
      if (path.startsWith(BUDGET_PATH)) {
        const id = decodeURIComponent(path.slice(BUDGET_PATH.length))
        writes.push({ method, path: id, body })
        if (method === "PATCH" && stored) {
          stored.properties.spendCentsPerDay = body.properties.spendCentsPerDay
          return json(200, stored)
        }
        if (method === "DELETE") {
          stored = null
          return new Response(null, { status: 204 })
        }
      }
      return json(404, { error: { code: "not_found", message: path } })
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  function renderCap() {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    return render(
      <QueryClientProvider client={client}>
        <AgentSpendCap
          agentId={MOSS}
          name="Moss"
          declared={300}
          spend={{ usd: 2, complete: true, running: 0 }}
        />
      </QueryClientProvider>
    )
  }

  it("reads the one override naming the agent, by its reference", async () => {
    renderCap()
    await screen.findByText("$3.00 in any 24 hours")
    const filter = JSON.parse(
      new URL(reads[0], "http://x").searchParams.get("filter")!
    )
    expect(filter.kinds).toEqual(["substrate.reamde.dev/llm/agentbudget"])
    expect(filter.properties).toEqual({ agent: { eq: MOSS_REF } })
  })

  it("holds the agent to its declared cap until you set your own", async () => {
    renderCap()
    expect(await screen.findByText("$3.00 in any 24 hours")).toBeTruthy()
    expect(
      screen.getByText("Declared by the agent. A cap you set replaces it.")
    ).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "Set a cap" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Cap" }), {
      target: { value: "10" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0]).toEqual({
      method: "POST",
      path: "/api/v1/records",
      body: {
        kind: "substrate.reamde.dev/llm/agentbudget",
        properties: { agent: { ref: MOSS_REF }, spendCentsPerDay: 1000 },
      },
    })
    expect(await screen.findByText("$10.00 in any 24 hours")).toBeTruthy()
    expect(
      screen.getByText("Set by you, in place of the $3.00 it declares.")
    ).toBeTruthy()
  })

  it("changes your override in place and removes it", async () => {
    stored = budget("moss", 150)
    renderCap()
    expect(await screen.findByText("$1.50 in any 24 hours")).toBeTruthy()
    // $2 spent against the $1.50 that holds.
    expect(screen.getByText("At the cap")).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "Change" }))
    const input = screen.getByRole("textbox", { name: "Cap" })
    expect((input as HTMLInputElement).value).toBe("1.50")
    fireEvent.change(input, { target: { value: "5" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0]).toEqual({
      method: "PATCH",
      path: "moss",
      body: { properties: { spendCentsPerDay: 500 } },
    })
    expect(await screen.findByText("$5.00 in any 24 hours")).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "Change" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove your cap" }))
    await waitFor(() => expect(writes).toHaveLength(2))
    expect(writes[1]).toEqual({
      method: "DELETE",
      path: "moss",
      body: undefined,
    })
    expect(await screen.findByText("$3.00 in any 24 hours")).toBeTruthy()
  })

  it("reads the override again after a write that failed", async () => {
    stored = budget("moss", 150)
    renderCap()
    await screen.findByText("$1.50 in any 24 hours")
    const before = reads.length
    failNext = true
    fireEvent.click(screen.getByRole("button", { name: "Change" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Cap" }), {
      target: { value: "5" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(reads.length).toBeGreaterThan(before))
    // The editor stays open with what was typed, to try again.
    expect(
      screen.getByRole<HTMLInputElement>("textbox", { name: "Cap" }).value
    ).toBe("5")
  })
})
