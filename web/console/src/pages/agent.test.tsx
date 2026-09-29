// @vitest-environment jsdom
/** One agent's page, from fixture records: its runs as one read, newest
 * first, each with its status, duration, cost and a link into its thread;
 * what they spent over the last day, week and month, summed from the thread
 * rows; the runs that didn't finish grouped by reason; the limits it
 * declares, under the record page's labels; and the write policies whose
 * selector names it. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import type { ReactNode } from "react"
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest"

import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

const params = { id: "" }

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params: linkParams,
    search,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    search?: Record<string, string>
    children: ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a
      data-to={to}
      data-params={JSON.stringify(linkParams ?? {})}
      data-search={JSON.stringify(search ?? {})}
      {...rest}
    >
      {children}
    </a>
  ),
}))

vi.mock("@/router", () => ({
  agentRoute: { useParams: () => params },
}))

import { AgentPage } from "./agent"

const AGENT_ID = "ada.example.com/llm/helper"
const AGENT_REF = `substrate.reamde.dev/core/agent/${AGENT_ID}`
const AGENT_PATH =
  "/api/v1/substrate.reamde.dev/core/agent/ada.example.com%2Fllm%2Fhelper"
const KIND = "substrate.reamde.dev/core/kind"
const THREAD = "substrate.reamde.dev/llm/thread"
const POLICY = "substrate.reamde.dev/core/recordpatchpolicy"
const PROVIDER = "substrate.reamde.dev/llm/provider"
const PERSON = "samples.substrate.reamde.dev/people/person"
const HOUR = 3_600_000
const DAY = 24 * HOUR

beforeAll(() => {
  Element.prototype.scrollIntoView ??= () => {}
})

const ago = (ms: number) => new Date(Date.now() - ms).toISOString()

function rec(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 3,
    createdAt: ago(40 * DAY),
    updatedAt: ago(40 * DAY),
  }
}

const AGENT = rec("substrate.reamde.dev/core/agent", AGENT_ID, {
  description: "Keeps your people tidy.",
  provider: { ref: `${PROVIDER}/openai` },
  model: "gpt-5",
  tools: [
    {
      function: {
        ref: "substrate.reamde.dev/core/function/substrate.reamde.dev/core/query",
      },
    },
  ],
  permissions: {
    reads: { kinds: [{ ref: `${KIND}/*` }], budgets: { rows: 50 } },
  },
  budgets: { maxTurns: 16 },
})

/** The agent kind as the registry serves it: enough of `budgets` and
 * `permissions` for the limits to read their declared labels. */
const AGENT_KIND: KindInfo = {
  identity: "substrate.reamde.dev/core/agent",
  name: "agent",
  authority: "substrate.reamde.dev",
  package: "core",
  version: 21,
  source: "seeded",
  description: "",
  definition: {
    properties: {
      budgets: {
        type: "object",
        fields: {
          maxTurns: {
            type: "int",
            description: "completions the loop may spend",
          },
          maxToolCalls: {
            type: "int",
            description: "tool calls the loop may spend",
          },
          deadlineSeconds: {
            type: "int",
            description: "wall clock for the whole invocation",
          },
        },
      },
      permissions: {
        type: "object",
        fields: {
          reads: {
            type: "object",
            fields: {
              budgets: {
                type: "object",
                fields: {
                  calls: {
                    type: "int",
                    description: "how many reads one run may make",
                  },
                  rows: {
                    type: "int",
                    description: "how many records one run may read",
                  },
                },
              },
            },
          },
        },
      },
    },
  },
}

/** A thread row as the loop leaves it: its row last moved when it settled. */
function thread(
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  const row = rec(THREAD, id, { agent: { ref: AGENT_REF }, ...properties })
  const moved = properties.finishedAt ?? properties.startedAt
  return typeof moved === "string" ? { ...row, updatedAt: moved } : row
}

let THREADS: SubstrateRecord[] = []
/** Set when the fixture read has more rows past its page. */
let MORE: string | undefined

const POLICIES = [
  rec(POLICY, "ask-before-deleting-people", {
    action: "gate",
    selector: { agents: [AGENT_ID], kinds: [PERSON], ops: ["delete"] },
  }),
  rec(POLICY, "someone-elses", {
    action: "refuse",
    selector: { agents: ["ada.example.com/llm/other"], kinds: [PERSON] },
  }),
]

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status })
}

function listed(path: string): string[] {
  const url = new URL(path, "http://x")
  if (url.pathname !== "/api/v1/records") return []
  const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
    kinds?: string[]
  }
  return filter.kinds ?? []
}

function mount(technical = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const preferences: ConsolePreferencesContextValue = {
    preferences: {
      collapsed: [],
      favorites: [],
      sidebarOpen: true,
      ...DEFAULT_SETTINGS,
      technicalDetails: technical,
    },
    busy: false,
    change: () => {},
    set: () => {},
  }
  return render(
    <ConsolePreferencesContext.Provider value={preferences}>
      <QueryClientProvider client={client}>
        <AgentPage />
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

/** One row of the spend table, by its label, in one group. */
function spendRow(label: string, group?: string): string[] {
  const table = screen.getByRole("table", { name: "What it spent" })
  const bodies = [...table.querySelectorAll("tbody")]
  const body = group
    ? bodies.find((b) => b.textContent?.startsWith(group))
    : bodies[0]
  const row = [...(body?.querySelectorAll("tr") ?? [])].find(
    (r) => r.querySelector("th")?.textContent === label
  )
  return [...(row?.querySelectorAll("td") ?? [])].map(
    (td) => td.textContent ?? ""
  )
}

describe("AgentPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    params.id = AGENT_ID
    MORE = undefined
    THREADS = [
      thread("t-chat", {
        mode: "chat",
        status: "ok",
        startedAt: ago(2 * HOUR),
        finishedAt: ago(2 * HOUR - 4_000),
        totalTokens: 1_200,
        costUSD: 0.012,
      }),
      thread("t-sched-1", {
        mode: "schedule",
        status: "error",
        reason: "provider refused the key: 401",
        startedAt: ago(3 * DAY),
        finishedAt: ago(3 * DAY - 2_000),
        totalTokens: 800,
        costUSD: 0.03,
      }),
      thread("t-sched-2", {
        mode: "schedule",
        status: "error",
        reason: "provider refused the key: 401",
        startedAt: ago(20 * DAY),
        finishedAt: ago(20 * DAY - 2_000),
        totalTokens: 10_000,
        costUSD: 0.5,
      }),
      thread("t-asked", {
        mode: "subagent",
        parent: { ref: `${THREAD}/t-other` },
        status: "overbudget",
        reason: "maxTurns",
        startedAt: ago(1 * HOUR),
        finishedAt: ago(1 * HOUR - 90_000),
        totalTokens: 300,
        costUSD: 0.002,
      }),
    ]
    vi.stubGlobal("fetch", fetchMock)
    fetchMock.mockImplementation(async (url) => {
      const path = String(url)
      if (path === AGENT_PATH) return jsonResponse(200, AGENT)
      const kinds = listed(path)
      const page = (records: unknown[]) =>
        jsonResponse(200, { records, head: 1, generation: "g" })
      if (kinds.includes(THREAD))
        return jsonResponse(200, {
          records: THREADS,
          cursor: MORE,
          head: 1,
          generation: "g",
        })
      if (kinds.includes(POLICY)) return page(POLICIES)
      if (kinds.includes(PROVIDER))
        return page([
          rec(PROVIDER, "openai", { wire: "openai", apiKey: "••••" }),
        ])
      if (kinds.includes(KIND))
        return jsonResponse(200, { kinds: [AGENT_KIND] })
      return page([])
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("reads its runs in one read, lists them newest first, each with a link into its thread", async () => {
    mount()
    const table = await screen.findByRole("table", { name: "Recent runs" })
    const read = fetchMock.mock.calls
      .map(([u]) => new URL(String(u), "http://x"))
      .find((u) => listed(u.pathname + u.search).includes(THREAD))!
    expect(read.searchParams.get("first")).toBe("500")
    // Most recently active first, so an old chat that went on today is in.
    expect(read.searchParams.get("orderBy")).toBe("updatedAt:desc")
    expect(JSON.parse(read.searchParams.get("filter")!).properties).toEqual({
      agent: { eq: AGENT_REF },
    })

    const rows = within(table).getAllByRole("row").slice(1)
    expect(rows.map((r) => r.textContent)).toEqual([
      expect.stringContaining("Asked by another agent"),
      expect.stringContaining("A chat"),
      expect.stringContaining("On a schedule"),
      expect.stringContaining("On a schedule"),
    ])
    const chat = within(rows[1]).getByText("A chat")
    expect(chat.getAttribute("data-to")).toBe("/agents")
    expect(JSON.parse(chat.getAttribute("data-search")!)).toEqual({
      thread: "t-chat",
    })
    expect(within(rows[1]).getByText("Done")).toBeTruthy()
    expect(within(rows[1]).getByText("4.0s")).toBeTruthy()
    expect(within(rows[1]).getByText("$0.012")).toBeTruthy()
    // A run another agent asked for opens as its thread record.
    const asked = within(rows[0]).getByText("Asked by another agent")
    expect(asked.getAttribute("data-to")).toBe(
      "/data/$authority/$pkg/$name/$id"
    )
    expect(JSON.parse(asked.getAttribute("data-params")!)).toEqual({
      authority: "substrate.reamde.dev",
      pkg: "llm",
      name: "thread",
      id: "t-asked",
    })
  })

  it("totals what its runs cost in each period from the thread rows", async () => {
    mount()
    await screen.findByRole("table", { name: "What it spent" })
    // The chat (2 hours ago) and the first scheduled run (3 days ago) are the
    // two runs of the week; the month adds the one from 20 days ago.
    expect(spendRow("Runs", "Runs it started")).toEqual(["1", "2", "3"])
    expect(spendRow("Cost", "Runs it started")).toEqual([
      "$0.012",
      "$0.042",
      "$0.542",
    ])
    expect(spendRow("Tokens used", "Runs it started")).toEqual([
      "1.2k",
      "2k",
      "12k",
    ])
    // Every cost is the one a run recorded, and the page says so.
    expect(screen.getByText(/^Cost is what each run recorded/)).toBeTruthy()
    // The run another agent asked for is its own group, never added in.
    expect(spendRow("Cost", "When another agent asked it")).toEqual([
      "$0.002",
      "$0.002",
      "$0.002",
    ])
    expect(screen.queryByText(/at least/)).toBeNull()
  })

  it("groups the runs that didn't finish by why", async () => {
    mount()
    const list = await screen.findByRole("list", { name: "What went wrong" })
    const groups = within(list).getAllByRole("listitem")
    expect(groups).toHaveLength(2)
    expect(within(groups[0]).getByText("Failed")).toBeTruthy()
    expect(within(groups[0]).getByText("2 runs")).toBeTruthy()
    expect(
      within(groups[0]).getByText("provider refused the key: 401")
    ).toBeTruthy()
    // The link opens the newest run that failed this way, in the chat app.
    const open = within(groups[0]).getByText("Open that run")
    expect(open.getAttribute("data-to")).toBe("/agents")
    expect(JSON.parse(open.getAttribute("data-search") ?? "{}")).toEqual({
      thread: "t-sched-1",
    })
    expect(within(groups[1]).getByText("Ran out of budget")).toBeTruthy()
    expect(within(groups[1]).getByText("1 run")).toBeTruthy()
  })

  it("shows the limits it declares under the record page's labels, and the default where it is silent", async () => {
    mount()
    const dl = await waitFor(() => {
      const el = screen.getByLabelText("Limits")
      expect(el.textContent).toContain("Max turns")
      return el
    })
    const text = dl.textContent ?? ""
    expect(text).toContain("completions the loop may spend")
    expect(text).toMatch(/Max turns.*16/)
    expect(text).toMatch(/Max tool calls.*32the default/)
    expect(text).toMatch(/Rows.*50/)
    expect(text).toMatch(/Calls.*16the default/)
  })

  it("never shows a run that recorded no cost as free, and marks a span longer than a run may work", async () => {
    THREADS = [
      thread("t-unpriced", {
        mode: "chat",
        status: "ok",
        // Opened three hours ago and continued an hour ago.
        startedAt: ago(3 * HOUR),
        finishedAt: ago(HOUR),
        totalTokens: 5_000,
        costUSD: 0,
      }),
      thread("t-priced", {
        mode: "schedule",
        status: "ok",
        startedAt: ago(2 * HOUR),
        finishedAt: ago(2 * HOUR - 3_000),
        totalTokens: 100,
        costUSD: 0.01,
      }),
    ]
    mount(true)
    const table = await screen.findByRole("table", { name: "Recent runs" })
    // Newest start first: the priced run started an hour after the other.
    const [second, first] = within(table).getAllByRole("row").slice(1)
    // Two hours from start to last reply is not how long it worked: faint.
    const span = within(first).getByText("2 h")
    expect(span.getAttribute("data-slot")).toBe("took-span")
    expect(within(first).getByText("None recorded")).toBeTruthy()
    expect(within(second).getByText("3.0s")).toBeTruthy()
    // One of the day's two runs recorded no cost: its cost is a floor.
    expect(spendRow("Cost")[0]).toBe("at least $0.010")
    // Technical mode names each run by its full reference.
    expect(
      within(first).getByText(`${THREAD}/t-unpriced`, { exact: false })
    ).toBeTruthy()
  })

  it("shows the write policies whose selector names it, and not another agent's", async () => {
    mount()
    expect(
      await screen.findByText(/^Asks you before it can delete people$/i)
    ).toBeTruthy()
    expect(screen.queryByText(/^Can’t .* people$/i)).toBeNull()
  })

  it("says at least where the read stopped before a period began", async () => {
    THREADS = Array.from({ length: 500 }, (_, i) =>
      thread(`t-${i}`, {
        mode: "schedule",
        status: "ok",
        startedAt: ago(HOUR + (i * 10 + 5) * 60_000),
        finishedAt: ago(HOUR + (i * 10 + 5) * 60_000 - 1_000),
        costUSD: 0.001,
        ...(i === 3 ? { status: "error", reason: "timeout" } : {}),
      })
    )
    MORE = "next"
    mount()
    await screen.findByRole("table", { name: "What it spent" })
    // 500 runs ten minutes apart reach back about 3.5 days: the day is
    // whole, the week and the month are not.
    const runs = spendRow("Runs")
    expect(runs[0]).not.toMatch(/at least/)
    expect(runs[1]).toMatch(/^at least /)
    expect(runs[2]).toMatch(/^at least /)
    expect(
      screen.getByText(/Counted from the 500 runs active most recently/)
    ).toBeTruthy()
    // An older run past the read may have failed the same way.
    const failed = screen.getByRole("list", { name: "What went wrong" })
    expect(within(failed).getByText("at least 1 run")).toBeTruthy()
    cleanup()

    // The same 500 rows with nothing past them are every run it had.
    MORE = undefined
    mount()
    await screen.findByRole("table", { name: "What it spent" })
    expect(spendRow("Runs")).toEqual(["138", "500", "500"])
    expect(screen.queryByText(/at least/)).toBeNull()
  })
})
