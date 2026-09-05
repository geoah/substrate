/** The Properties tab (issue #38): a clicked row's data must read field by
 * field (prose as a block, a reference as a link, a secret as its sentinel),
 * with the declared shape visible even where the record holds nothing, and
 * nothing the record holds hidden. */

import { cleanup, render } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
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

import { PropertiesRail } from "./properties"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

const task: KindInfo = {
  identity: "samples.substrate.reamde.dev/tasks/task",
  name: "task",
  authority: "samples.substrate.reamde.dev",
  package: "tasks",
  version: 1,
  plural: "tasks",
  source: "installed",
  definition: {
    properties: {
      summary: { type: "string", description: "what the task is" },
      notes: { type: "text" },
      estimate: { type: "int" },
      dueBy: { type: "datetime" },
      phase: { type: "state", states: ["open", "done"], initial: "open" },
      priority: {
        type: "string",
        values: [
          { value: "p1", label: "Urgent" },
          { value: "p2", label: "" },
        ],
      },
      apiKey: { type: "secret" },
      tags: { type: "string", repeated: true },
      payload: { type: "json" },
      parent: {
        type: "reference",
        kind: "samples.substrate.reamde.dev/tasks/task",
      },
      // A reference carrying LINK DATA: its value is `{ref, role}`, not the
      // bare path, and the link's own properties are data the record holds.
      assignee: {
        type: "reference",
        kind: "samples.substrate.reamde.dev/people/person",
        mustExist: true,
        properties: { role: { type: "string" } },
      },
      reviewers: {
        type: "reference",
        kind: "samples.substrate.reamde.dev/people/person",
        repeated: true,
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
  plural: "people",
  source: "installed",
}

const record: SubstrateRecord = {
  id: "t1",
  kind: "samples.substrate.reamde.dev/tasks/task",
  properties: {
    summary: "Ship the console",
    notes: "first line\nsecond line",
    estimate: 3,
    dueBy: "2026-08-20T09:30:00Z",
    phase: "open",
    priority: "p1",
    apiKey: "<redacted>",
    tags: ["a", "b"],
    payload: { retries: 2 },
    parent: "samples.substrate.reamde.dev/tasks/task/t0",
    assignee: { ref: "samples.substrate.reamde.dev/people/person/p1" },
    reviewers: [
      "samples.substrate.reamde.dev/people/person/p2",
      "samples.substrate.reamde.dev/people/person/p3",
    ],
    legacy: "still here",
  },
  labels: {},
  version: 3,
  createdAt: "2026-08-14T10:00:00Z",
  updatedAt: "2026-08-14T10:00:00Z",
}

/** `null` means "the registry lacks the kind" — an explicit `undefined` would
 * fall back to the default parameter and hand the kind over anyway. */
function renderRail(
  rec: SubstrateRecord = record,
  kind: KindInfo | null = task
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <PropertiesRail
        record={rec}
        kind={kind ?? undefined}
        kinds={[task, person]}
      />
    </QueryClientProvider>
  )
}

afterEach(cleanup)

describe("PropertiesRail", () => {
  it("shows every declared property with its name, type and value", () => {
    const { container } = renderRail()
    const text = container.textContent ?? ""
    expect(text).toContain("summary")
    expect(text).toContain("Ship the console")
    expect(text).toContain("string")
    expect(text).toContain("estimate")
    expect(text).toContain("3")
  })

  it("keeps prose whole: a text value is a pre-wrapped block", () => {
    const { container } = renderRail()
    const block = [
      ...container.querySelectorAll<HTMLElement>(".whitespace-pre-wrap"),
    ].find((el) => el.textContent?.includes("first line"))
    expect(block).toBeTruthy()
    expect(block?.textContent).toBe("first line\nsecond line")
  })

  it("says a declared property is not set instead of dropping the row", () => {
    const bare: SubstrateRecord = {
      ...record,
      properties: { summary: "just this" },
    }
    const { container } = renderRail(bare)
    const text = container.textContent ?? ""
    expect(text).toContain("notes")
    expect(text).toContain("not set")
  })

  it("links a reference to its referent's detail page, as a RecordPill", () => {
    const { container } = renderRail()
    const link = [...container.querySelectorAll("a")].find(
      (a) => a.textContent === "t0"
    )
    expect(link?.getAttribute("href")).toBe(
      "/data/samples.substrate.reamde.dev/tasks/task/t0"
    )
    // The one way a record is referenced from elsewhere — not an ad-hoc link.
    expect(link?.className).toContain("rounded-full")
  })

  it("lists a repeated property one item per line", () => {
    const { container } = renderRail()
    const items = [...container.querySelectorAll("li")].map(
      (li) => li.textContent
    )
    expect(items).toContain("a")
    expect(items).toContain("b")
  })

  it("renders a datetime in the console's stamp with the wire value on hover", () => {
    const { container } = renderRail()
    const stamp = container.querySelector('[title="2026-08-20T09:30:00Z"]')
    expect(stamp?.textContent).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/)
  })

  it("shows an enum by its authored label", () => {
    const { container } = renderRail()
    expect(container.textContent).toContain("Urgent")
  })

  it("renders a state as its badge", () => {
    const { container } = renderRail()
    expect(container.textContent).toContain("open")
  })

  it("says a secret is redacted and never shows more", () => {
    const { container } = renderRail()
    expect(container.textContent).toContain("<redacted>")
  })

  it("pretty-prints json", () => {
    const { container } = renderRail()
    const pre = [...container.querySelectorAll("pre")].find((el) =>
      el.textContent?.includes("retries")
    )
    expect(pre?.textContent).toContain('"retries": 2')
  })

  it("shows a value the kind never declared, marked undeclared", () => {
    const { container } = renderRail()
    const text = container.textContent ?? ""
    expect(text).toContain("legacy")
    expect(text).toContain("still here")
    expect(text).toContain("undeclared")
  })

  it("tells a stored empty string from an absent value", () => {
    const emptied: SubstrateRecord = {
      ...record,
      properties: { summary: "" },
    }
    const { container } = renderRail(emptied)
    const text = container.textContent ?? ""
    expect(text).toContain('""')
    // notes is truly absent and still reads as such
    expect(text).toContain("not set")
  })

  it("renders a reference carrying link data as its pill plus the link's own properties", () => {
    const linked: SubstrateRecord = {
      ...record,
      properties: {
        ...record.properties,
        assignee: {
          ref: "samples.substrate.reamde.dev/people/person/p1",
          role: "reviewer",
        },
      },
    }
    const { container } = renderRail(linked)
    // The pointer still reads as the pill every other surface uses...
    const link = [...container.querySelectorAll("a")].find(
      (a) => a.textContent === "p1"
    )
    expect(link?.getAttribute("href")).toBe(
      "/data/samples.substrate.reamde.dev/people/person/p1"
    )
    expect(link?.className).toContain("rounded-full")
    // ...and the link data rides beside it rather than being dropped.
    expect(container.textContent).toContain("role: reviewer")
  })

  it("does not print a link-data reference as a JSON blob", () => {
    const { container } = renderRail()
    const pre = [...container.querySelectorAll("pre")].find((el) =>
      el.textContent?.includes("ref")
    )
    expect(pre).toBeUndefined()
  })

  it("lists a repeated reference as a list of pills", () => {
    const { container } = renderRail()
    const hrefs = [...container.querySelectorAll("li a")].map((a) =>
      a.getAttribute("href")
    )
    expect(hrefs).toContain(
      "/data/samples.substrate.reamde.dev/people/person/p2"
    )
    expect(hrefs).toContain(
      "/data/samples.substrate.reamde.dev/people/person/p3"
    )
  })

  it("sizes a property's name at least as large as its value, and heavier", () => {
    const { container } = renderRail()
    const name = [...container.querySelectorAll("span")].find(
      (el) => el.textContent === "summary"
    )
    // The header outranks its body: same text-sm, font-medium against normal.
    expect(name?.className).toContain("text-sm")
    expect(name?.className).toContain("font-medium")
  })

  it("still shows the data when the registry lacks the kind", () => {
    const { container } = renderRail(record, null)
    const text = container.textContent ?? ""
    expect(text).toContain("Ship the console")
    expect(text).not.toContain("undeclared")
  })

  it("says plainly when there is nothing to show", () => {
    const empty: SubstrateRecord = {
      ...record,
      properties: {},
    }
    const { container } = renderRail(empty, {
      ...task,
      definition: { properties: {} },
    })
    expect(container.textContent).toContain("No data")
  })
})
