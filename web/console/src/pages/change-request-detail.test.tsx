// @vitest-environment jsdom
/** The review page's contract, the parts a pure test cannot hold: a patch
 * shows what the record holds now beside what it would hold if applied, in the
 * record page's labels; a create shows what it would add; a delete says out
 * loud that it deletes and asks for a second press; the Apply PATCH carries
 * the REQUEST's version as `ifVersion` (the write path refuses a decision
 * without it). Everyday copy by default; the op, the versions, the policy,
 * the thread, a judge's verdict and the raw diff behind Technical details. */

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

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

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

const TASK_KIND = "samples.substrate.reamde.dev/tasks/task"
const REQUEST_PATH = "/api/v1/substrate.reamde.dev/core/recordpatchrequest/cr-1"
const TARGET_PATH = "/api/v1/samples.substrate.reamde.dev/tasks/task/task-1"
const THREAD_PATH = "/api/v1/substrate.reamde.dev/llm/thread/th-1"

const KINDS: KindInfo[] = [
  {
    identity: TASK_KIND,
    name: "task",
    authority: "samples.substrate.reamde.dev",
    package: "tasks",
    version: 1,
    source: "installed",
    description: "",
    definition: {
      properties: {
        summary: { type: "string", description: "what it is" },
        note: { type: "string" },
      },
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
    kind: "substrate.reamde.dev/core/recordpatchrequest",
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

const thread: SubstrateRecord = {
  id: "th-1",
  kind: "substrate.reamde.dev/llm/thread",
  properties: {
    agent: { ref: "substrate.reamde.dev/core/agent/crew.test.dev/crew/scribe" },
  },
  labels: {},
  version: 1,
  createdAt: "2026-08-14T00:00:00Z",
  updatedAt: "2026-08-14T00:00:00Z",
}

function renderPage(ui: ReactElement, technical = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <ConsolePreferencesContext.Provider
      value={{
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
      }}
    >
      <QueryClientProvider client={client}>{ui}</QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

/** The kinds a records-route URL lists, read off its `filter`: the list route
 * is one path for every kind, so a stub dispatches on this, not the path. */
function listedKinds(path: string): string[] {
  const url = new URL(path, "http://x")
  if (url.pathname !== "/api/v1/records") return []
  const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
    kinds?: string[]
  }
  return filter.kinds ?? []
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
      if (listedKinds(path).includes("substrate.reamde.dev/core/kind")) {
        return jsonResponse(200, { kinds: KINDS })
      }
      if (path === REQUEST_PATH) {
        return method === "PATCH"
          ? (opts.patch?.() ?? jsonResponse(200, cr))
          : jsonResponse(200, cr)
      }
      if (path === THREAD_PATH) return jsonResponse(200, thread)
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

  it("puts what the record holds now beside what it would hold if applied", async () => {
    serve(patchRequest, { target })
    renderPage(<ChangeRequestDetailPage />)

    await screen.findByText("The title moved in the source.")
    // The Now column waits on the live target read.
    expect(await screen.findByText("Old summary")).toBeTruthy()
    expect(screen.getByText("Now")).toBeTruthy()
    expect(screen.getByText("If applied")).toBeTruthy()
    // The record page's labels, never the keys or the op.
    expect(screen.getByText("Summary")).toBeTruthy()
    expect(screen.queryByText("patch")).toBeNull()
    expect(screen.queryByText("summary")).toBeNull()
    expect(screen.getByText("New summary")).toBeTruthy()
    // The null in the diff empties the property, and the row says so.
    expect(screen.getByText("goes away")).toBeTruthy()
    expect(screen.getByText("Cleared")).toBeTruthy()
  })

  it("names the agent whose chat suggested it, with a way back to the chat", async () => {
    serve(
      request({
        ...patchRequest,
        properties: {
          ...patchRequest.properties,
          thread: { ref: "substrate.reamde.dev/llm/thread/th-1" },
        },
      }),
      { target }
    )
    renderPage(<ChangeRequestDetailPage />)
    expect(await screen.findByText("Scribe")).toBeTruthy()
    expect(screen.getByText(/Suggested by/)).toBeTruthy()
    const chat = screen.getByText("Open the chat")
    expect(chat.getAttribute("data-to")).toBe("/agents")
  })

  it("applies with the REQUEST's version as ifVersion, in one press", async () => {
    serve(patchRequest, { target })
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText("Old summary")

    fireEvent.click(screen.getByRole("button", { name: "Apply" }))

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

  it("dismisses without asking", async () => {
    serve(patchRequest, { target })
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText("Old summary")
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    await waitFor(() => {
      const patch = fetchMock.mock.calls.find(
        ([, init]) => (init as RequestInit | undefined)?.method === "PATCH"
      )
      expect(JSON.parse((patch![1] as RequestInit).body as string)).toEqual({
        properties: { decision: "rejected" },
        ifVersion: 4,
      })
    })
  })

  it("says the suggestion moved when the decision comes back a conflict", async () => {
    serve(patchRequest, {
      target,
      patch: () =>
        jsonResponse(409, {
          error: { code: "conflict", message: "version 4 is stale" },
        }),
    })
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText("Old summary")
    fireEvent.click(screen.getByRole("button", { name: "Apply" }))
    await screen.findByText(/Nothing was applied: the suggestion or the record/)
  })

  it("warns when the target has moved past the stamped targetVersion", async () => {
    serve(patchRequest, { target: { ...target, version: 9 } })
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText(/The record changed after this was suggested/)
    // The versions are technical.
    expect(screen.queryByText(/targetVersion/)).toBeNull()
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
    renderPage(<ChangeRequestDetailPage />, true)
    await screen.findByText("This change couldn’t be applied.")
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
              assignee: "samples.substrate.reamde.dev/people/person/p1",
            },
          },
        },
      })
    )
    renderPage(<ChangeRequestDetailPage />)

    await screen.findByText("What it adds")
    expect(screen.getByText("New task: Write it down")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Add it" })).toBeTruthy()
    // A pointer is a proposed value like any other, on its own property row.
    expect(screen.getByText("Assignee")).toBeTruthy()
    expect(
      screen.getByText("samples.substrate.reamde.dev/people/person/p1")
    ).toBeTruthy()
    // Nothing exists yet, so there is no Now column to compare with.
    expect(screen.queryByText("Now")).toBeNull()
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

    await screen.findByText(/Applying deletes/)
    expect(screen.getByText(/History keeps what it was/)).toBeTruthy()
    // What the record holds now, once it is read.
    expect(await screen.findByText("goes away")).toBeTruthy()
    // A second press, never one.
    fireEvent.click(screen.getByRole("button", { name: "Delete it" }))
    expect(await screen.findByText("Press again to delete it.")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Yes, delete it" }))
    await waitFor(() => {
      const patch = fetchMock.mock.calls.find(
        ([, init]) => (init as RequestInit | undefined)?.method === "PATCH"
      )
      expect(patch).toBeTruthy()
    })
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

    await screen.findByText(/Nothing was changed/)
    // The badge and the sentence both say Dismissed.
    expect(screen.getAllByText("Dismissed").length).toBeGreaterThan(0)
    expect(screen.getByText("You")).toBeTruthy()
    expect(screen.getByText("What was suggested")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Apply" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull()
  })

  it("refuses to guess at an op it does not know", async () => {
    serve(request({ properties: { op: "merge" } }))
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText(/can’t tell what applying this would do/)
    expect(screen.getByText("A change the console can’t read")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Apply" })).toBeNull()
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

    await screen.findByText("Finalizers it adds")
    expect(screen.getByText("owner/hold")).toBeTruthy()
    expect(screen.getByText("Finalizers it removes")).toBeTruthy()
    expect(screen.getByText("app/lock")).toBeTruthy()
    // No property is named, and that is not the same as applying nothing.
    expect(await screen.findByText(/It changes no property/)).toBeTruthy()
    expect(screen.queryByText(/would do nothing/)).toBeNull()
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
    renderPage(<ChangeRequestDetailPage />, true)

    await screen.findByText(/The record changed after this was suggested/)
    expect(screen.getByText(/the diff’s own ifVersion/)).toBeTruthy()
    expect(screen.getByText("Diff ifVersion")).toBeTruthy()
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

    await screen.findByText(/carries parts that can’t be applied/)
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

    await screen.findByText(/Part of this suggestion is stored in a shape/)
    // The raw value, kept verbatim beside the key it was stored under.
    expect(screen.getByText("What couldn’t be read")).toBeTruthy()
    expect(screen.getByText("properties")).toBeTruthy()
    expect(screen.getByText("[]")).toBeTruthy()
    expect(screen.queryByText(/changes nothing/)).toBeNull()
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
    const pill = await waitFor(() => {
      const link = document.querySelector('a[data-params*="task-42"]')
      if (!link) throw new Error("no pill for task-42")
      return link
    })
    expect(pill.getAttribute("data-params")).toBe(
      JSON.stringify({
        authority: "samples.substrate.reamde.dev",
        pkg: "tasks",
        name: "task",
        id: "task-42",
      })
    )
    // The link data the reference carries beside it stays visible.
    expect(screen.getByText("Note: waits on it")).toBeTruthy()
    expect(screen.queryByText(/\{"ref"/)).toBeNull()
    // An object that is not a reference reads as its JSON.
    expect(screen.getByText(/"shape": "opaque"/)).toBeTruthy()
  })

  it("names the diff keys the substrate's strict decode would refuse", async () => {
    serve(request({ properties: { diff: { saved: true } } }), { target })
    renderPage(<ChangeRequestDetailPage />)
    await screen.findByText(/carries parts that can’t be applied/)
    expect(screen.getByText("saved")).toBeTruthy()
  })

  it("puts the ids, the policy, the thread, the verdict and the diff behind the switch", async () => {
    const gated = request({
      ...patchRequest,
      properties: {
        ...patchRequest.properties,
        policy: { ref: "substrate.reamde.dev/core/recordpatchpolicy/gate-1" },
        policyRevision: 2,
        thread: { ref: "substrate.reamde.dev/llm/thread/th-1" },
      },
      annotations: {
        "policy/verdict": { verdict: "ask", confidence: 0.7 },
      },
    })
    serve(gated, { target })
    const { unmount } = renderPage(<ChangeRequestDetailPage />)
    await screen.findByText("Old summary")
    expect(screen.queryByText("Technical details")).toBeNull()
    expect(screen.queryByText(/recordpatchpolicy/)).toBeNull()
    unmount()

    renderPage(<ChangeRequestDetailPage />, true)
    await screen.findByText("Technical details")
    expect(
      screen.getByText("substrate.reamde.dev/core/recordpatchrequest/cr-1")
    ).toBeTruthy()
    expect(
      screen.getByText("substrate.reamde.dev/core/recordpatchpolicy/gate-1")
    ).toBeTruthy()
    expect(screen.getByText("at revision 2")).toBeTruthy()
    expect(
      screen.getByText("substrate.reamde.dev/llm/thread/th-1")
    ).toBeTruthy()
    expect(screen.getByText("A judge said ask (70% sure)")).toBeTruthy()
    expect(screen.getByText(/"New summary"/)).toBeTruthy()
  })
})
