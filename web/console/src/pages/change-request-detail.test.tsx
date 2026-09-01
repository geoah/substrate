/** The review page's contract, the parts a pure test cannot hold: a patch
 * shows the target's live value beside the proposed one, a create previews the
 * record it would mint, a delete says out loud that it deletes, and the accept
 * PATCH carries the REQUEST's version as `ifVersion` (the write path refuses a
 * decision without it, so a page that forgot it would offer a button that never
 * works). The stale-target and conflict paths are here too, because they are
 * what a reviewer needs to see before pressing anything. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import type { ReactElement, ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Toaster } from "@/components/ui/toast"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

const params = { id: "cr-1" }

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
  changeRequestDetailRoute: { useParams: () => params },
}))

import { ChangeRequestDetailPage } from "./change-request-detail"

const TASK_KIND = "tasks.substrate.reamde.dev/task"
const REQUEST_PATH = "/api/v1/core.substrate.reamde.dev/recordpatchrequest/cr-1"
const TARGET_PATH = "/api/v1/tasks.substrate.reamde.dev/task/task-1"

const KINDS: KindInfo[] = [
  {
    identity: TASK_KIND,
    name: "task",
    authority: "tasks.substrate.reamde.dev",
    version: 1,
    plural: "tasks",
    source: "installed",
    definition: {
      properties: { summary: { type: "string", description: "what it is" } },
    },
  },
]

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

function request(over: Partial<SubstrateRecord>): SubstrateRecord {
  return {
    id: "cr-1",
    kind: "core.substrate.reamde.dev/recordpatchrequest",
    properties: {},
    labels: {},
    version: 4,
    createdAt: "2026-08-14T00:00:00Z",
    updatedAt: "2026-08-14T00:00:00Z",
    propertyMeta: {
      diff: { manager: "learner.substrate", updatedAt: "2026-08-14T00:00:00Z" },
    },
    ...over,
  }
}

const patchRequest = request({
  properties: {
    rationale: "The title moved in the source.",
    targetVersion: 3,
    diff: { properties: { summary: "New summary", note: null } },
    // The `target` REFERENCE, as served: the referent's whole record path
    // under `ref`.
    target: { ref: `${TASK_KIND}/task-1` },
  },
})

const target: SubstrateRecord = {
  id: "task-1",
  kind: TASK_KIND,
  properties: { summary: "Old summary", note: "goes away" },
  labels: {},
  version: 3,
  createdAt: "2026-08-13T00:00:00Z",
  updatedAt: "2026-08-13T00:00:00Z",
  propertyMeta: {
    summary: { manager: "owner", updatedAt: "2026-08-13T00:00:00Z" },
  },
}

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

describe("ChangeRequestDetailPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  function serve(
    cr: SubstrateRecord,
    opts: { target?: SubstrateRecord; patch?: () => Response } = {}
  ) {
    fetchMock.mockImplementation(async (url, init) => {
      const method = (init as RequestInit | undefined)?.method ?? "GET"
      const path = String(url)
      if (path.startsWith("/api/v1/core.substrate.reamde.dev/kind")) {
        return jsonResponse(200, { kinds: KINDS })
      }
      if (path === REQUEST_PATH) {
        return method === "PATCH"
          ? (opts.patch?.() ?? jsonResponse(200, cr))
          : jsonResponse(200, cr)
      }
      if (path === TARGET_PATH) {
        return opts.target
          ? jsonResponse(200, opts.target)
          : jsonResponse(404, { error: { code: "not_found", message: "gone" } })
      }
      return jsonResponse(200, { records: [] })
    })
  }

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    params.id = "cr-1"
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("shows a patch field by field, with what the accept does to each row", async () => {
    serve(patchRequest, { target })
    renderPage(<ChangeRequestDetailPage />)

    await screen.findByText("The title moved in the source.")
    expect(screen.getByText("patch")).toBeTruthy()
    // The before column waits on the live target read.
    expect(await screen.findByText("Old summary")).toBeTruthy()
    expect(screen.getByText("New summary")).toBeTruthy()
    // The null in the diff deletes the key, and the row says so.
    expect(screen.getByText("removed")).toBeTruthy()
    expect(screen.getByText("removes")).toBeTruthy()
    expect(screen.getByText("overwrites")).toBeTruthy()
    // Whose value the accept overwrites.
    expect(screen.getByText("owner")).toBeTruthy()
  })

  it("accepts with the REQUEST's version as ifVersion", async () => {
    serve(patchRequest, { target })
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText("The title moved in the source.")

    fireEvent.click(screen.getByRole("button", { name: /Accept/ }))
    const confirm = await screen.findByRole("button", {
      name: "Accept and apply",
    })
    fireEvent.click(confirm)

    await waitFor(() => {
      const patch = fetchMock.mock.calls.find(
        ([, init]) => (init as RequestInit | undefined)?.method === "PATCH"
      )
      expect(patch).toBeTruthy()
      expect(JSON.parse((patch![1] as RequestInit).body as string)).toEqual({
        properties: { decision: "accepted" },
        ifVersion: 4,
      })
    })
  })

  it("says the request moved when the decision comes back a conflict", async () => {
    serve(patchRequest, {
      target,
      patch: () =>
        jsonResponse(409, {
          error: { code: "conflict", message: "version 4 is stale" },
        }),
    })
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText("The title moved in the source.")

    fireEvent.click(screen.getByRole("button", { name: /Accept/ }))
    fireEvent.click(
      await screen.findByRole("button", { name: "Accept and apply" })
    )

    await screen.findByText("The request moved, or the apply was refused")
  })

  it("warns when the target has moved past the stamped targetVersion", async () => {
    serve(patchRequest, { target: { ...target, version: 9 } })
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText(/The target has moved/)
    expect(screen.getByText(/the stamped targetVersion/)).toBeTruthy()
  })

  it("surfaces the substrate/conflict annotation a refused apply left", async () => {
    serve(
      request({
        ...patchRequest,
        annotations: {
          "substrate/conflict": { reason: "applyDiff on cr-1: stale" },
        },
      }),
      { target }
    )
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText("The substrate refused to apply this change.")
    expect(screen.getByText("applyDiff on cr-1: stale")).toBeTruthy()
  })

  it("previews the record a create would mint, its pointers among the values", async () => {
    serve(
      request({
        properties: {
          op: "create",
          targetKind: TASK_KIND,
          targetId: "task-9",
          // `diff` is a `json` property, so nothing normalizes the values
          // inside it: they are the write this request proposes, where the
          // bare path is legal shorthand.
          diff: {
            properties: {
              summary: "Write it down",
              assignee: "people.substrate.reamde.dev/person/p1",
            },
          },
        },
      })
    )
    renderPage(<ChangeRequestDetailPage />)

    await screen.findByText(/Accepting mints/)
    expect(screen.getByText("create")).toBeTruthy()
    expect(screen.getByText("Write it down")).toBeTruthy()
    // A pointer is a proposed value like any other, on its own property row.
    expect(screen.getByText("assignee")).toBeTruthy()
    expect(
      screen.getByText("people.substrate.reamde.dev/person/p1")
    ).toBeTruthy()
    // Nothing exists yet, so there is no before column to compare with.
    expect(screen.queryByText("the accept")).toBeNull()
  })

  it("is unmistakable about a delete, and summarizes what would go", async () => {
    serve(
      request({
        properties: {
          op: "delete",
          targetVersion: 3,
          target: { ref: `${TASK_KIND}/task-1` },
        },
      }),
      { target }
    )
    renderPage(<ChangeRequestDetailPage />)

    await screen.findByText(/Accepting DELETES/)
    expect(screen.getByText("delete")).toBeTruthy()
    expect(screen.getByText(/The record is tombstoned/)).toBeTruthy()
    // The summary of the record the accept would take away, once it is read.
    expect(await screen.findByText("goes away")).toBeTruthy()
  })

  it("renders a decided request read-only, with the decision and the decider", async () => {
    serve(
      request({
        ...patchRequest,
        properties: {
          ...patchRequest.properties,
          decision: "rejected",
          decidedAt: "2026-08-14T01:00:00Z",
        },
        propertyMeta: {
          diff: {
            manager: "learner.substrate",
            updatedAt: "2026-08-14T00:00:00Z",
          },
          decidedAt: { manager: "console", updatedAt: "2026-08-14T01:00:00Z" },
        },
      }),
      { target }
    )
    renderPage(<ChangeRequestDetailPage />)

    await screen.findByText(/nothing was applied/)
    expect(screen.getByText("rejected")).toBeTruthy()
    expect(screen.getByText("console")).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Accept/ })).toBeNull()
    expect(screen.queryByRole("button", { name: /Reject/ })).toBeNull()
  })

  it("refuses to guess at an op it does not know", async () => {
    serve(request({ properties: { op: "merge" } }))
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText(/names an op the console does not know/)
    expect(screen.getByText("unknown op")).toBeTruthy()
  })

  it("renders a finalizer-only patch as work, not as 'applies nothing'", async () => {
    serve(
      request({
        properties: {
          targetVersion: 3,
          diff: {
            addFinalizers: ["owner/hold"],
            removeFinalizers: ["app/lock"],
          },
          target: { ref: `${TASK_KIND}/task-1` },
        },
      }),
      { target }
    )
    renderPage(<ChangeRequestDetailPage />)

    await screen.findByText("finalizers it adds")
    expect(screen.getByText("owner/hold")).toBeTruthy()
    expect(screen.getByText("finalizers it removes")).toBeTruthy()
    expect(screen.getByText("app/lock")).toBeTruthy()
    // No property is named, and that is not the same as applying nothing.
    expect(await screen.findByText(/No property is named/)).toBeTruthy()
    expect(screen.queryByText(/applies nothing/)).toBeNull()
  })

  it("compares against the diff's own ifVersion, which overrides the stamp", async () => {
    serve(
      request({
        ...patchRequest,
        properties: {
          ...patchRequest.properties,
          diff: {
            properties: { summary: "New summary" },
            ifVersion: 7,
          },
        },
      }),
      // The stamped targetVersion (3) agrees with the target, the diff's own
      // ifVersion (7) does not: the accept checks 7, so the page must warn.
      { target }
    )
    renderPage(<ChangeRequestDetailPage />)

    await screen.findByText(/The target has moved/)
    expect(
      screen.getByText(/the diff's own ifVersion, which overrides/)
    ).toBeTruthy()
    expect(
      screen.getByText("the version it checks the target against")
    ).toBeTruthy()
  })

  it("names `edges` as a key the decoder refuses, on either op", async () => {
    // The key is gone from PutInput and PatchInput alike, so a diff still
    // writing one fails the accept whole rather than being ignored.
    serve(
      request({
        properties: {
          op: "create",
          targetKind: TASK_KIND,
          targetId: "task-9",
          diff: {
            properties: { summary: "Write it down" },
            edges: [{ rel: "assignee", to: { id: "p1" } }],
          },
        },
      })
    )
    renderPage(<ChangeRequestDetailPage />)

    await screen.findByText(/the substrate's decoder refuses/)
    expect(screen.getByText("edges")).toBeTruthy()
  })

  it("says a malformed wrapper is unreadable instead of showing an empty diff", async () => {
    serve(
      request({
        properties: {
          targetVersion: 3,
          diff: { properties: [] },
          target: { ref: `${TASK_KIND}/task-1` },
        },
      }),
      { target }
    )
    renderPage(<ChangeRequestDetailPage />)

    await screen.findByText(/stored in a shape the substrate's decoder refuses/)
    // The raw value, kept verbatim beside the key it was stored under.
    expect(
      screen.getByText("stored values the substrate's decoder cannot read")
    ).toBeTruthy()
    expect(screen.getByText("properties")).toBeTruthy()
    expect(screen.getByText("[]")).toBeTruthy()
    expect(screen.queryByText(/names nothing at all/)).toBeNull()
  })

  it("renders a reference value in the diff as the referent's pill", async () => {
    serve(
      request({
        properties: {
          targetVersion: 3,
          target: { ref: `${TASK_KIND}/task-1` },
          // `diff` is a `json` property, so the value is stored exactly as the
          // proposer wrote it: an agent copying a served reference stores the
          // `{ref, …}` object (issue #332).
          diff: {
            properties: {
              blocks: { ref: `${TASK_KIND}/task-42`, note: "waits on it" },
              payload: { shape: "opaque", n: 2 },
            },
          },
        },
      }),
      { target }
    )
    renderPage(<ChangeRequestDetailPage />)

    // The pill, routed at the referent, not the literal `{"ref":"…"}` text.
    const pill = await screen.findByText("task-42")
    expect(pill.closest("a")?.getAttribute("data-params")).toBe(
      JSON.stringify({
        authority: "tasks.substrate.reamde.dev",
        name: "task",
        id: "task-42",
      })
    )
    // The link data the reference carries beside it stays visible.
    expect(screen.getByText("note: waits on it")).toBeTruthy()
    expect(screen.queryByText(/\{"ref"/)).toBeNull()
    // An object that is not a reference is still summarized as its keys.
    expect(screen.getByText("{shape, n}")).toBeTruthy()
  })

  it("names the diff keys the substrate's strict decode would refuse", async () => {
    serve(request({ properties: { diff: { saved: true } } }), { target })
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText(/names keys the substrate's decoder refuses/)
    expect(screen.getByText("saved")).toBeTruthy()
  })
})
