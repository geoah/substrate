// @vitest-environment jsdom
/** The Graph tab's layout contract: direction is said once per section
 * (Outgoing/Incoming references), the current record heads the tree, groups carry the
 * shared kind and the count, and every target is a RecordPill — not a bare
 * link with the kind repeated on every row. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  render,
  waitFor,
  screen,
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const graphWire = vi.hoisted(() => ({ mapped: false }))
beforeEach(() => {
  graphWire.mapped = false
})

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: React.ReactNode
  }) => (
    <a
      href={Object.entries(params ?? {}).reduce(
        (path, [key, value]) => path.replace(`$${key}`, value),
        to
      )}
      {...rest}
    >
      {children}
    </a>
  ),
}))

// The fan-in is the graph's one query; everything else about the layout is
// pure render. One page, no cursor: two comments point at the task, and
// `matches` says from which property each does.
vi.mock("@/lib/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/http")>()
  return {
    ...actual,
    request: vi.fn((_method: string, path: string) => {
      const url = new URL(path, "http://x")
      const filter = JSON.parse(url.searchParams.get("filter") ?? "{}")
      if (url.pathname === "/api/v1/records" && filter.referencing) {
        const comment = "notes.substrate.reamde.dev/notes/comment"
        const row = (id: string, title: string) => ({
          id,
          kind: comment,
          properties: { title, task: `${filter.referencing.ref}` },
          labels: {},
          version: 1,
          createdAt: "2026-08-14T10:00:00Z",
          updatedAt: "2026-08-14T10:00:00Z",
        })
        return Promise.resolve({
          records: [row("c1", "First"), row("c2", "Second")],
          head: 2,
          generation: "g1",
          matches: {
            [`${comment}/c1`]: [{ property: "task" }],
            [`${comment}/c2`]: [{ property: "task" }],
          },
        })
      }
      if (filter.kinds?.includes("substrate.reamde.dev/core/recordmapping"))
        return Promise.resolve({
          records: graphWire.mapped
            ? [
                {
                  id: "example.com/tasks/commenttask",
                  properties: {
                    from: {
                      ref: "substrate.reamde.dev/core/kind/notes.substrate.reamde.dev/notes/comment",
                    },
                    to: {
                      ref: "substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/tasks/task",
                    },
                    property: "task",
                  },
                },
              ]
            : [],
          head: 0,
          generation: "g1",
        })
      throw new Error(`unexpected request: ${path}`)
    }),
  }
})

import { GraphRail } from "./graph"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

const task: KindInfo = {
  identity: "samples.substrate.reamde.dev/tasks/task",
  name: "task",
  authority: "samples.substrate.reamde.dev",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    properties: {
      summary: { type: "string" },
      assignee: {
        type: "reference",
        kind: "person",
        repeated: true,
        mustExist: true,
      },
    },
  },
}

const person: KindInfo = {
  identity: "samples.substrate.reamde.dev/people/person",
  name: "person",
  authority: "samples.substrate.reamde.dev",
  package: "people",
  version: 1,
  source: "installed",
  description: "",
  definition: {},
}

const record: SubstrateRecord = {
  id: "t1",
  kind: "samples.substrate.reamde.dev/tasks/task",
  // `title` is the server-derived heading `recordTitle` reads.
  properties: {
    summary: "Ship the console",
    title: "Ship the console",
    // The pointers ARE properties now: the graph reads them off the record
    // with no query, and a reference carries no title, so the pill renders
    // the referent's id until the record is opened.
    assignee: [
      "samples.substrate.reamde.dev/people/person/p1",
      "samples.substrate.reamde.dev/people/person/p2",
    ],
  },
  labels: {},
  version: 1,
  createdAt: "2026-08-14T10:00:00Z",
  updatedAt: "2026-08-14T10:00:00Z",
}

function renderRail() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <GraphRail
        authority="samples.substrate.reamde.dev"
        pkg="tasks"
        name="task"
        record={record}
        kinds={[task, person]}
      />
    </QueryClientProvider>
  )
}

afterEach(cleanup)

describe("GraphRail", () => {
  it("shows three distinct reference sections", async () => {
    const { container } = renderRail()
    await waitFor(() => {
      expect(container.textContent).toContain("Reference property: task")
    })
    const text = container.textContent ?? ""
    expect(text).toContain("Mapped and merged sources")
    expect(text).toContain("Outgoing")
    // Direction lives on the section header alone — no per-row arrows left
    // to mistake for one another.
    expect(
      container.querySelectorAll("svg.lucide-arrow-up-right")
    ).toHaveLength(1)
    expect(
      container.querySelectorAll("svg.lucide-arrow-down-left")
    ).toHaveLength(1)
  })

  it("renders every target as a link to the record", async () => {
    const { container } = renderRail()
    const first = [...container.querySelectorAll("a")].find(
      (a) => a.textContent === "p1"
    )
    expect(first?.className).toContain("text-primary")
    expect(first?.getAttribute("href")).toBe(
      "/data/samples.substrate.reamde.dev/people/person/p1"
    )
    await waitFor(() => {
      expect(container.textContent).toContain("Reference property: task")
    })
  })

  it("says a group's shared kind once, with its count, never per row", async () => {
    const { container } = renderRail()
    await waitFor(() => {
      expect(container.textContent).toContain("Reference property: task")
    })
    const text = container.textContent ?? ""
    // Two assignees, one kind: "person" appears on the group label alone.
    expect(text).toContain("samples.substrate.reamde.dev/people/person")
    expect(text).toContain("assignee")
    expect(text).toContain("2")
    // The fan-in group is named from this record's side, with its own count.
    expect(text).toContain("Reference property: task")
    expect(text).toContain("notes.substrate.reamde.dev/notes/comment")
    expect(text).not.toContain("task of comment")
  })

  it("separates declared mapping sources from ordinary incoming references", async () => {
    graphWire.mapped = true
    renderRail()
    const sources = screen
      .getByRole("heading", { name: "Mapped and merged sources" })
      .closest("section")!
    const incoming = screen
      .getByRole("heading", { name: "Incoming references" })
      .closest("section")!
    await waitFor(() =>
      expect(
        within(sources).getByText("notes.substrate.reamde.dev/notes/comment")
      ).toBeTruthy()
    )
    expect(
      within(incoming).queryByText("notes.substrate.reamde.dev/notes/comment")
    ).toBeNull()
    expect(within(sources).getByText("2 records")).toBeTruthy()
  })

  it("reads a reference carrying link data by the path under `ref`", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const linked: SubstrateRecord = {
      ...record,
      properties: {
        ...record.properties,
        assignee: [
          {
            ref: "samples.substrate.reamde.dev/people/person/p1",
            role: "owner",
          },
        ],
      },
    }
    const { container } = render(
      <QueryClientProvider client={client}>
        <GraphRail
          authority="samples.substrate.reamde.dev"
          pkg="tasks"
          name="task"
          record={linked}
          kinds={[task, person]}
        />
      </QueryClientProvider>
    )
    await waitFor(() => {
      expect(container.textContent).toContain("Reference property: task")
    })
    const pill = [...container.querySelectorAll("a")].find(
      (a) => a.textContent === "p1"
    )
    expect(pill?.getAttribute("href")).toBe(
      "/data/samples.substrate.reamde.dev/people/person/p1"
    )
  })
})
