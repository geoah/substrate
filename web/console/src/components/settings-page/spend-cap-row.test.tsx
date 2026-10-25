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

// Deleted since it finished: the engine still counts it.
const DELETED = [
  {
    kind: "substrate.reamde.dev/llm/thread",
    id: "t5",
    deletedAt: hoursAgo(1),
    properties: {
      mode: "chat",
      status: "ok",
      finishedAt: hoursAgo(3),
      costUSD: 0.25,
    },
  },
]

function isDeletedRead(path: string): boolean {
  const filter = new URL(path, "http://x").searchParams.get("filter")
  return filter !== null && JSON.parse(filter).deleted === true
}

describe("SpendCapRow", () => {
  const fetchMock = vi.fn<typeof fetch>()
  let stored: ReturnType<typeof setting> | undefined
  const puts: unknown[] = []
  let livePage: () => Response
  let deletedPage: () => Response
  let settingPage: (() => Response) | undefined

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "geoah", "tok-1")
    stored = undefined
    puts.length = 0
    livePage = () => json(200, { records: THREADS })
    deletedPage = () => json(200, { records: [] })
    settingPage = undefined
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      const path = String(url)
      if (path === SETTING_PATH && method === "GET") {
        if (settingPage) return settingPage()
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
        return isDeletedRead(path) ? deletedPage() : livePage()
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
    // The name is written only when the record is created, so a name the
    // owner gave it since stays.
    expect(puts[1]).toEqual({ properties: { type: "int", value: "2000" } })
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

  it("refuses a comma and a blank cap rather than guess or clear", async () => {
    stored = setting("2000")
    renderRow()
    fireEvent.click(await screen.findByRole("button", { name: "Change" }))
    const input = screen.getByRole("textbox", { name: "Cap" })
    const save = screen.getByRole("button", { name: "Save" })

    fireEvent.change(input, { target: { value: "1,50" } })
    expect(screen.getByRole("alert").textContent).toBe(
      "Type an amount in dollars, like 20 or 12.50."
    )
    expect((save as HTMLButtonElement).disabled).toBe(true)

    fireEvent.change(input, { target: { value: "$" } })
    expect((save as HTMLButtonElement).disabled).toBe(true)

    // Blank is not an error to shout about, but it is not a cap either.
    fireEvent.change(input, { target: { value: "" } })
    expect(screen.queryByRole("alert")).toBeNull()
    expect((save as HTMLButtonElement).disabled).toBe(true)
    fireEvent.submit(input.closest("form")!)
    expect(puts).toHaveLength(0)
  })

  it("reads a signed cap as the engine does", async () => {
    stored = setting("+2000")
    renderRow()
    expect(await screen.findByText("$20.00 in any 24 hours")).toBeTruthy()
  })

  it("says an unreadable cap holds every run, and offers to change it", async () => {
    stored = setting("12.50")
    renderRow()
    expect(await screen.findByText("Unreadable")).toBeTruthy()
    expect(screen.getByText("12.50")).toBeTruthy()
    expect(
      screen.getByText(
        "Every agent run is held until this is a whole number of cents."
      )
    ).toBeTruthy()
    expect(screen.queryByText("No cap")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Change" }))
    expect(screen.getByRole("button", { name: "Remove the cap" })).toBeTruthy()
    fireEvent.change(screen.getByRole("textbox", { name: "Cap" }), {
      target: { value: "12.50" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0]).toEqual({ properties: { type: "int", value: "1250" } })
  })

  it("is at the cap when the spend equals it, past float rounding", async () => {
    stored = setting("29")
    livePage = () =>
      json(200, {
        records: [
          {
            kind: "substrate.reamde.dev/llm/thread",
            id: "t1",
            properties: {
              mode: "chat",
              status: "ok",
              finishedAt: hoursAgo(1),
              costUSD: 0.29,
            },
          },
        ],
      })
    renderRow()
    expect(await screen.findByText("$0.290")).toBeTruthy()
    expect(await screen.findByText("At the cap")).toBeTruthy()
  })

  it("counts the runs deleted since they finished", async () => {
    deletedPage = () => json(200, { records: DELETED })
    renderRow()
    expect(await screen.findByText("$2.00")).toBeTruthy()
    expect(screen.queryByText("at least")).toBeNull()
  })

  // The two reads are separate snapshots: a run read live and deleted before
  // the deleted read is in both, and counts once.
  it("counts a run once when it is in both reads", async () => {
    stored = setting("1000")
    const run = {
      kind: "substrate.reamde.dev/llm/thread",
      id: "t6",
      updatedAt: hoursAgo(2),
      properties: {
        mode: "chat",
        status: "ok",
        finishedAt: hoursAgo(2),
        costUSD: 6,
      },
    }
    livePage = () => json(200, { records: [run] })
    deletedPage = () =>
      json(200, {
        records: [{ ...run, updatedAt: hoursAgo(1), deletedAt: hoursAgo(1) }],
      })
    renderRow()
    expect(await screen.findByText("$10.00 in any 24 hours")).toBeTruthy()
    expect(await screen.findByText("$6.00")).toBeTruthy()
    expect(screen.queryByText("At the cap")).toBeNull()
  })

  // The single read answers a deleted setting with its properties; the
  // engine reads it as no cap, and a put restores it with its name.
  it("reads a deleted cap as no cap, and sets it again without renaming it", async () => {
    stored = { ...setting("100"), deletedAt: hoursAgo(1) } as ReturnType<
      typeof setting
    >
    renderRow()
    expect(await screen.findByText("No cap")).toBeTruthy()
    expect(screen.queryByText("At the cap")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Set a cap" }))
    const input = screen.getByRole("textbox", { name: "Cap" })
    expect((input as HTMLInputElement).value).toBe("")
    fireEvent.change(input, { target: { value: "20" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0]).toEqual({ properties: { type: "int", value: "2000" } })
    expect(await screen.findByText("$20.00 in any 24 hours")).toBeTruthy()
  })

  it("gives the spend as a floor when the deleted runs don't load", async () => {
    deletedPage = () =>
      json(500, { error: { code: "internal", message: "boom" } })
    renderRow()
    expect(await screen.findByText("$1.75")).toBeTruthy()
    expect(screen.getByText("at least")).toBeTruthy()
    expect(
      screen.getByText(
        "The deleted runs didn’t load, so this counts only the runs still here."
      )
    ).toBeTruthy()
  })

  it("gives the spend as a floor when one read doesn't reach back a day", async () => {
    livePage = () =>
      json(200, {
        records: THREADS.slice(0, 1).map((t) => ({
          ...t,
          updatedAt: hoursAgo(2),
        })),
        cursor: "next",
      })
    renderRow()
    expect(await screen.findByText("$1.25")).toBeTruthy()
    expect(screen.getByText("at least")).toBeTruthy()
    expect(
      screen.getByText(
        "There are more runs than one read holds, so some that finished in the window aren’t counted here."
      )
    ).toBeTruthy()
  })

  it("says a run still going counts once it finishes", async () => {
    livePage = () =>
      json(200, {
        records: [
          ...THREADS,
          {
            kind: "substrate.reamde.dev/llm/thread",
            id: "t9",
            properties: { mode: "chat", status: "running" },
          },
        ],
      })
    renderRow()
    expect(
      await screen.findByText(
        "A run still going counts against the cap as it spends, and shows here once it finishes."
      )
    ).toBeTruthy()
  })

  it("says when the cap or the runs didn't load", async () => {
    livePage = () => json(500, { error: { code: "internal", message: "boom" } })
    settingPage = () =>
      json(500, { error: { code: "internal", message: "the cap broke" } })
    renderRow()
    expect(
      await screen.findByText("The cap didn’t load: the cap broke")
    ).toBeTruthy()
    expect(await screen.findByText("The runs didn’t load")).toBeTruthy()
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Set a cap" })
        .disabled
    ).toBe(true)
  })
})
