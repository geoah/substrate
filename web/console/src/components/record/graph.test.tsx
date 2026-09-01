/** The Graph tab's layout contract: direction is said once per section
 * (Outgoing/Incoming), the current record heads the tree, groups carry the
 * shared kind and the count, and every target is a RecordPill — not a bare
 * link with the kind repeated on every row. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

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
// pure render. One page, no cursor: two comments point at the task.
vi.mock("@/lib/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/http")>()
  return {
    ...actual,
    request: vi.fn((_method: string, path: string) => {
      if (path.includes("/incoming")) {
        return Promise.resolve({
          incoming: [
            {
              property: "task",
              from: {
                id: "c1",
                kind: "notes.substrate.reamde.dev/comment",
                title: "First",
              },
            },
            {
              property: "task",
              from: {
                id: "c2",
                kind: "notes.substrate.reamde.dev/comment",
                title: "Second",
              },
            },
          ],
          total: 2,
        })
      }
      throw new Error(`unexpected request: ${path}`)
    }),
  }
})

import { GraphRail } from "./graph"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

const task: KindInfo = {
  identity: "tasks.substrate.reamde.dev/task",
  name: "task",
  authority: "tasks.substrate.reamde.dev",
  version: 1,
  plural: "tasks",
  source: "installed",
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
  identity: "people.substrate.reamde.dev/person",
  name: "person",
  authority: "people.substrate.reamde.dev",
  version: 1,
  plural: "people",
  source: "installed",
}

const record: SubstrateRecord = {
  id: "t1",
  kind: "tasks.substrate.reamde.dev/task",
  // `title` is the server-derived heading `recordTitle` reads.
  properties: {
    summary: "Ship the console",
    title: "Ship the console",
    // The pointers ARE properties now: the graph reads them off the record
    // with no query, and a reference carries no title, so the pill renders
    // the referent's id until the record is opened.
    assignee: [
      "people.substrate.reamde.dev/person/p1",
      "people.substrate.reamde.dev/person/p2",
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
        authority="tasks.substrate.reamde.dev"
        plural="task"
        record={record}
        kinds={[task, person]}
      />
    </QueryClientProvider>
  )
}

afterEach(cleanup)

describe("GraphRail", () => {
  it("heads the tree with the current record, and says each direction once", async () => {
    const { container } = renderRail()
    await waitFor(() => {
      expect(container.textContent).toContain("Incoming")
    })
    const text = container.textContent ?? ""
    expect(text).toContain("Ship the console")
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

  it("renders every target as a RecordPill that routes to the record", async () => {
    const { container } = renderRail()
    const first = [...container.querySelectorAll("a")].find(
      (a) => a.textContent === "p1"
    )
    expect(first?.className).toContain("rounded-full")
    expect(first?.getAttribute("href")).toBe(
      "/data/people.substrate.reamde.dev/person/p1"
    )
    await waitFor(() => {
      expect(container.textContent).toContain("Incoming")
    })
  })

  it("says a group's shared kind once, with its count, never per row", async () => {
    const { container } = renderRail()
    await waitFor(() => {
      expect(container.textContent).toContain("Incoming")
    })
    const text = container.textContent ?? ""
    // Two assignees, one kind: "person" appears on the group label alone.
    expect(text.match(/person/g)).toHaveLength(1)
    expect(text).toContain("assignee")
    expect(text).toContain("2")
    // The fan-in group is named from this record's side, with its own count.
    expect(text).toContain("task of comment")
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
          { ref: "people.substrate.reamde.dev/person/p1", role: "owner" },
        ],
      },
    }
    const { container } = render(
      <QueryClientProvider client={client}>
        <GraphRail
          authority="tasks.substrate.reamde.dev"
          plural="task"
          record={linked}
          kinds={[task, person]}
        />
      </QueryClientProvider>
    )
    await waitFor(() => {
      expect(container.textContent).toContain("Incoming")
    })
    const pill = [...container.querySelectorAll("a")].find(
      (a) => a.textContent === "p1"
    )
    expect(pill?.getAttribute("href")).toBe(
      "/data/people.substrate.reamde.dev/person/p1"
    )
  })
})
