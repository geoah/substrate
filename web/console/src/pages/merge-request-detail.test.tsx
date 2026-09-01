/** The side-by-side's rendering contract: the pair's values are read off two
 * live records and the cell has no declaration to consult, so a value that
 * carries the served reference shape renders as the referent's pill rather
 * than as literal `{"ref":"…"}` text (issue #332). */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import type { ReactElement, ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Toaster } from "@/components/ui/toast"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

const params = { id: "mr-1" }

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params: linkParams,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-params={JSON.stringify(linkParams ?? {})} {...rest}>
      {children}
    </a>
  ),
}))

vi.mock("@/router", () => ({
  mergeRequestDetailRoute: { useParams: () => params },
}))

import { MergeRequestDetailPage } from "./merge-request-detail"

const PERSON_KIND = "people.substrate.reamde.dev/person"
const TASK_KIND = "tasks.substrate.reamde.dev/task"
const MR_PATH = "/api/v1/core.substrate.reamde.dev/recordmergerequest/mr-1"

const KINDS: KindInfo[] = [
  {
    identity: PERSON_KIND,
    name: "person",
    authority: "people.substrate.reamde.dev",
    version: 1,
    plural: "person",
    source: "installed",
    definition: {
      properties: {
        name: { type: "string" },
        employer: { type: "reference", to: TASK_KIND },
        note: { type: "json" },
      },
    },
  },
  {
    identity: TASK_KIND,
    name: "task",
    authority: "tasks.substrate.reamde.dev",
    version: 1,
    plural: "task",
    source: "installed",
    definition: { properties: { summary: { type: "string" } } },
  },
]

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

function person(id: string, properties: Record<string, unknown>) {
  return {
    id,
    kind: PERSON_KIND,
    properties,
    labels: {},
    version: 2,
    createdAt: "2026-08-13T00:00:00Z",
    updatedAt: "2026-08-13T00:00:00Z",
  } satisfies SubstrateRecord
}

const mergeRequest: SubstrateRecord = {
  id: "mr-1",
  kind: "core.substrate.reamde.dev/recordmergerequest",
  properties: {
    decision: "proposed",
    rationale: "Same email on both.",
    // The pair rides `winner`/`loser` references, as served.
    winner: { ref: `${PERSON_KIND}/p1` },
    loser: { ref: `${PERSON_KIND}/p2` },
  },
  labels: {},
  version: 3,
  createdAt: "2026-08-14T00:00:00Z",
  updatedAt: "2026-08-14T00:00:00Z",
}

// The winner points at a task; the loser carries an ordinary JSON object under
// the same key, so one render pass covers both arms of the detection.
const winner = person("p1", {
  name: "Alex",
  employer: { ref: `${TASK_KIND}/task-42`, since: "2021" },
})
const loser = person("p2", {
  name: "Alex",
  note: { shape: "opaque", n: 2 },
})

function renderPage(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Toaster>{ui}</Toaster>
    </QueryClientProvider>
  )
}

describe("MergeRequestDetailPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    fetchMock.mockImplementation(async (url) => {
      const path = String(url)
      if (path.startsWith("/api/v1/core.substrate.reamde.dev/kind")) {
        return jsonResponse(200, { kinds: KINDS })
      }
      if (path === MR_PATH) return jsonResponse(200, mergeRequest)
      // The two sides, whichever collection segment the page addresses them
      // through.
      if (path.endsWith("/p1")) return jsonResponse(200, winner)
      if (path.endsWith("/p2")) return jsonResponse(200, loser)
      return jsonResponse(200, { records: [] })
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("renders a reference value in the side-by-side as the referent's pill", async () => {
    renderPage(<MergeRequestDetailPage />)

    // The pill, routed at the referent, not the literal `{"ref":"…"}` text.
    const pill = await screen.findByText("task-42")
    expect(pill.closest("a")?.getAttribute("data-params")).toBe(
      JSON.stringify({
        authority: "tasks.substrate.reamde.dev",
        name: "task",
        id: "task-42",
      })
    )
    // The link data the reference carries stays beside it.
    expect(screen.getByText("since: 2021")).toBeTruthy()
    expect(screen.queryByText(/\{"ref"/)).toBeNull()
    // An object that is not a reference is still summarized as its keys.
    expect(screen.getByText("{shape, n}")).toBeTruthy()
  })
})
