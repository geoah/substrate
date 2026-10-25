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

import { SpendCapRow } from "./spend-cap-row"

const SETTING_PATH =
  "/api/v1/substrate.reamde.dev/core/setting/substrate.reamde.dev%2Fllm%2FspendCentsPerDay"

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status })
}

function setting(value: string) {
  return {
    kind: "substrate.reamde.dev/core/setting",
    id: "substrate.reamde.dev/llm/spendCentsPerDay",
    version: 1,
    properties: { displayName: "Daily agent spend cap", type: "int", value },
  }
}

const hoursAgo = (h: number) =>
  new Date(Date.now() - h * 3_600_000).toISOString()

const THREADS = [
  // Two agents' root runs in the window: both count.
  {
    kind: "substrate.reamde.dev/llm/thread",
    id: "t1",
    properties: {
      mode: "chat",
      status: "ok",
      finishedAt: hoursAgo(2),
      costUSD: 1.25,
    },
  },
  {
    kind: "substrate.reamde.dev/llm/thread",
    id: "t2",
    properties: {
      mode: "schedule",
      status: "ok",
      finishedAt: hoursAgo(5),
      costUSD: 0.5,
    },
  },
  // Out of the window.
  {
    kind: "substrate.reamde.dev/llm/thread",
    id: "t3",
    properties: {
      mode: "chat",
      status: "ok",
      finishedAt: hoursAgo(30),
      costUSD: 9,
    },
  },
  // Asked by another agent: inside its parent's cost already.
  {
    kind: "substrate.reamde.dev/llm/thread",
    id: "t4",
    properties: {
      mode: "subagent",
      status: "ok",
      finishedAt: hoursAgo(1),
      costUSD: 4,
    },
  },
]

describe("SpendCapRow", () => {
  const fetchMock = vi.fn<typeof fetch>()
  let stored: ReturnType<typeof setting> | undefined
  const puts: unknown[] = []

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "geoah", "tok-1")
    stored = undefined
    puts.length = 0
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      const path = String(url)
      if (path === SETTING_PATH && method === "GET") {
        return stored
          ? json(200, stored)
          : json(404, { error: { code: "not_found", message: "not found" } })
      }
      if (path === SETTING_PATH && method === "PUT") {
        const body = JSON.parse(String(init?.body))
        puts.push(body)
        stored = setting(body.properties.value)
        return json(200, stored)
      }
      if (path.startsWith("/api/v1/records")) {
        return json(200, { records: THREADS })
      }
      return json(404, { error: { code: "not_found", message: path } })
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  function renderRow() {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    return render(
      <QueryClientProvider client={client}>
        <SpendCapRow />
      </QueryClientProvider>
    )
  }

  it("shows no cap and every agent's root spend over the last 24 hours", async () => {
    renderRow()
    expect(await screen.findByText("No cap")).toBeTruthy()
    expect(screen.getByText("Spent in the last 24 hours")).toBeTruthy()
    expect(await screen.findByText("$1.75")).toBeTruthy()
    expect(screen.queryByText("At the cap")).toBeNull()
  })

  it("sets, changes and clears the cap without naming the record", async () => {
    renderRow()
    await screen.findByText("No cap")
    fireEvent.click(screen.getByRole("button", { name: "Set a cap" }))
    const input = screen.getByRole("textbox", { name: "Cap" })
    fireEvent.change(input, { target: { value: "1.50" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0]).toEqual({
      properties: {
        displayName: "Daily agent spend cap",
        type: "int",
        value: "150",
      },
    })
    expect(await screen.findByText("$1.50 in any 24 hours")).toBeTruthy()
    // $1.75 spent against a $1.50 cap.
    expect(await screen.findByText("At the cap")).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "Change" }))
    const again = screen.getByRole("textbox", { name: "Cap" })
    expect((again as HTMLInputElement).value).toBe("1.50")
    fireEvent.change(again, { target: { value: "20" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(puts).toHaveLength(2))
    expect(puts[1]).toMatchObject({ properties: { value: "2000" } })
    expect(await screen.findByText("$20.00 in any 24 hours")).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "Change" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove the cap" }))
    await waitFor(() => expect(puts).toHaveLength(3))
    expect(puts[2]).toMatchObject({ properties: { type: "int", value: "" } })
    expect(await screen.findByText("No cap")).toBeTruthy()
  })

  it("says why a negative or fractional cent cap is refused, and does not send it", async () => {
    renderRow()
    await screen.findByText("No cap")
    fireEvent.click(screen.getByRole("button", { name: "Set a cap" }))
    const input = screen.getByRole("textbox", { name: "Cap" })
    const save = screen.getByRole("button", { name: "Save" })

    fireEvent.change(input, { target: { value: "-5" } })
    expect(screen.getByText("A cap can’t be negative.")).toBeTruthy()
    expect((save as HTMLButtonElement).disabled).toBe(true)

    fireEvent.change(input, { target: { value: "1.005" } })
    expect(
      screen.getByText(
        "A cap is a whole number of cents: two decimals at most."
      )
    ).toBeTruthy()
    expect((save as HTMLButtonElement).disabled).toBe(true)
    expect(puts).toHaveLength(0)
  })
})
