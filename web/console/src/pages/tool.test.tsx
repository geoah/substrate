// @vitest-environment jsdom
/** One tool's page, from fixture records: a person reads what it may do in
 * words, when it runs, who uses it and how its runs went; a sync waiting on
 * its provider says so and links to finishing the setup; a tool that can be
 * run directly offers a form built from its arguments, and running it calls
 * the function with exactly what was typed. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Toaster } from "@/components/ui/toast"
import type { SubstrateRecord } from "@/lib/api/types"

const params = { authority: "", pkg: "", name: "" }

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params: linkParams,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: React.ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-params={JSON.stringify(linkParams ?? {})} {...rest}>
      {children}
    </a>
  ),
}))

vi.mock("@/router", () => ({
  toolRoute: { useParams: () => params },
}))

import { ToolPage } from "./tool"

const KIND = "substrate.reamde.dev/core/kind"
const FN = "substrate.reamde.dev/core/function"
const GCAL = "providers.substrate.reamde.dev/google/synccalendar"
const SAVE = "ada.example.com/notes/savenote"

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
    version: 1,
    createdAt: "2026-09-25T10:00:00Z",
    updatedAt: "2026-09-25T10:00:00Z",
  }
}

const FUNCTIONS = [
  rec(FN, GCAL, {
    runtime: "python",
    description: "Sync a Google account's calendars. More detail.",
    timeout: "PT1M",
    source: "def main(input, host):\n    return {}\n",
    permissions: {
      reads: {
        kinds: [
          { ref: `${KIND}/providers.substrate.reamde.dev/google/calendar` },
        ],
      },
      writes: [
        { ref: `${KIND}/providers.substrate.reamde.dev/google/calendarevent` },
      ],
      network: ["www.googleapis.com"],
    },
  }),
  rec(FN, SAVE, {
    runtime: "python",
    description: "Save a note with its title and its counts.",
    permissions: { writes: [{ ref: `${KIND}/ada.example.com/notes/note` }] },
    arguments: [
      {
        name: "text",
        type: "string",
        required: true,
        description: "the note body",
      },
      { name: "words", type: "int", description: "the word count" },
    ],
    returns: [{ name: "saved", type: "string", description: "the note id" }],
  }),
]

const AGENTS = [
  rec("substrate.reamde.dev/core/agent", "ada.example.com/notes/notekeeper", {
    tools: [{ function: { ref: `${FN}/${SAVE}` } }],
  }),
]

const TRIGGERS = [
  rec("substrate.reamde.dev/core/trigger", "google-calendar-scheduled", {
    enabled: true,
    callable: { ref: `${FN}/${GCAL}` },
    source: { schedule: { recurrence: "FREQ=HOURLY" } },
  }),
  rec("substrate.reamde.dev/core/trigger", "google-calendar-on-connect", {
    enabled: true,
    callable: { ref: `${FN}/${GCAL}` },
    source: {
      record: {
        kinds: ["providers.substrate.reamde.dev/google/account"],
        ops: ["create", "update"],
      },
    },
  }),
]

const RUNS = [
  rec("substrate.reamde.dev/core/triggerrun", "run-1", {
    trigger: {
      ref: "substrate.reamde.dev/core/trigger/google-calendar-scheduled",
    },
    mode: "schedule",
    status: "ok",
    effects: { put: 14 },
    startedAt: "2026-09-25T09:00:00Z",
    finishedAt: "2026-09-25T09:00:02Z",
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

describe("ToolPage", () => {
  const fetchMock = vi.fn<typeof fetch>()
  let runs: SubstrateRecord[] = []
  let callStatus = 200

  beforeEach(() => {
    runs = []
    callStatus = 200
    fetchMock.mockImplementation(async (url) => {
      const path = String(url)
      if (path.endsWith("/sync/status")) return jsonResponse(200, { items: [] })
      if (path.includes("/function/") && path.endsWith("/call"))
        return callStatus === 200
          ? jsonResponse(200, { output: { saved: "n1" }, effects: 1 })
          : new Response("method not allowed", { status: callStatus })
      const kinds = listed(path)
      const page = (records: SubstrateRecord[]) =>
        jsonResponse(200, { records, head: 1, generation: "g" })
      if (kinds.includes(FN)) return page(FUNCTIONS)
      if (kinds.includes("substrate.reamde.dev/core/agent")) return page(AGENTS)
      if (kinds.includes("substrate.reamde.dev/core/trigger"))
        return page(TRIGGERS)
      if (kinds.includes("substrate.reamde.dev/core/triggerrun")) {
        const filter = JSON.parse(
          new URL(path, "http://x").searchParams.get("filter") ?? "{}"
        ) as { referencing?: { ref: string } }
        const target = filter.referencing?.ref
        return page(
          target
            ? runs.filter(
                (r) => (r.properties.trigger as { ref: string }).ref === target
              )
            : runs
        )
      }
      if (kinds.includes("substrate.reamde.dev/core/repository"))
        return page([
          rec("substrate.reamde.dev/core/repository", "r", {
            authority: "ada.example.com",
          }),
        ])
      return page([])
    })
    vi.stubGlobal("fetch", fetchMock)
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  function renderAt(ref: string) {
    const [authority, pkg, name] = ref.split("/")
    Object.assign(params, { authority, pkg, name })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    return render(
      <QueryClientProvider client={client}>
        <Toaster>
          <ToolPage />
        </Toaster>
      </QueryClientProvider>
    )
  }

  it("reads a waiting sync: permissions in words, schedule, the provider to finish", async () => {
    renderAt(GCAL)
    expect(
      await screen.findByRole("heading", { name: "Google Calendar sync" })
    ).toBeTruthy()
    expect(screen.getByText("From Google")).toBeTruthy()
    const note = await screen.findByRole("note")
    expect(within(note).getByText("Waiting for Google")).toBeTruthy()
    const finish = within(note).getByText("Finish setting up Google")
    expect(finish.getAttribute("data-to")).toBe("/providers/$authority/$pkg")
    expect(screen.getByText("Calendars")).toBeTruthy()
    expect(screen.getByText("Calendar events")).toBeTruthy()
    expect(screen.getByText("Only www.googleapis.com")).toBeTruthy()
    expect(screen.getByText("No other agents or tools")).toBeTruthy()
    expect(screen.getByText("Every hour")).toBeTruthy()
    expect(screen.getByText("When an account is first connected")).toBeTruthy()
    expect(screen.getByRole("button", { name: /Sync now/ })).toHaveProperty(
      "disabled",
      true
    )
    expect(screen.queryByRole("form", { name: "Try it" })).toBeNull()
    expect(await screen.findByText("It hasn’t run yet.")).toBeTruthy()
    // The declaration is technical: no source in everyday mode.
    expect(screen.queryByText(/def main/)).toBeNull()
  })

  it("lists the runs a trigger recorded, newest first, in words", async () => {
    runs = RUNS
    renderAt(GCAL)
    const table = await screen.findByRole("table", { name: "Recent runs" })
    expect(within(table).getByText("14 saved")).toBeTruthy()
    expect(within(table).getByText("Schedule")).toBeTruthy()
    expect(within(table).getByText("2.0s")).toBeTruthy()
    expect(within(table).getByText("Worked")).toBeTruthy()
  })

  it("offers Try it for an agent's tool and calls it with what was typed", async () => {
    renderAt(SAVE)
    expect(
      await screen.findByRole("heading", { name: "Save note" })
    ).toBeTruthy()
    expect(screen.getByText("Yours")).toBeTruthy()
    expect(screen.getByText("By an agent")).toBeTruthy()
    const usedBy = screen.getByText("Notekeeper").closest("a")
    expect(usedBy?.getAttribute("data-to")).toBe("/agents/$id")
    expect(screen.getByText("The note body")).toBeTruthy()

    const form = screen.getByRole("form", { name: "Try it" })
    fireEvent.click(within(form).getByRole("button", { name: /Run/ }))
    expect(await within(form).findByText("Needed")).toBeTruthy()

    fireEvent.change(within(form).getByLabelText(/^Text/), {
      target: { value: "hello there" },
    })
    fireEvent.change(within(form).getByLabelText(/^Words/), {
      target: { value: "2" },
    })
    fireEvent.click(within(form).getByRole("button", { name: /Run/ }))
    const result = await screen.findByRole("status")
    expect(within(result).getByText("Done.")).toBeTruthy()
    expect(within(result).getByText("It made 1 change.")).toBeTruthy()
    expect(within(result).getByText("n1")).toBeTruthy()

    const call = fetchMock.mock.calls.find(([u]) => String(u).endsWith("/call"))
    expect(String(call?.[0])).toBe(
      "/api/v1/substrate.reamde.dev/core/function/ada.example.com%2Fnotes%2Fsavenote/call"
    )
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      input: { text: "hello there", words: 2 },
    })
  })

  it("says an older substrate cannot run a tool, not the transport's words", async () => {
    callStatus = 405
    renderAt(SAVE)
    const form = await screen.findByRole("form", { name: "Try it" })
    fireEvent.change(within(form).getByLabelText(/^Text/), {
      target: { value: "hello there" },
    })
    fireEvent.click(within(form).getByRole("button", { name: /Run/ }))
    expect(
      await within(form).findByText(/older version that can’t run a tool/)
    ).toBeTruthy()
  })

  it("says so when no tool has that reference", async () => {
    renderAt("ada.example.com/notes/missing")
    expect(
      await screen.findByRole("heading", { name: "No such tool" })
    ).toBeTruthy()
  })
})
