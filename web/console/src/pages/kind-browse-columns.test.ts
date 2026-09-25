// @vitest-environment jsdom

import { createElement, type ReactNode } from "react"
import { cleanup, render } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import { expandableReferences } from "@/lib/definition"
import type { ReferenceTitles } from "@/lib/reference-titles"
import {
  buildColumns,
  columnIdOf,
  defaultHiddenColumns,
  propertyColumnId,
  sortPropertyOf,
} from "./kind-browse-columns"

describe("column id ↔ wire property mapping", () => {
  it("namespaces declared properties apart from system columns", () => {
    // pullrequest declares its own `updatedAt`; the system column must
    // not collide with it (live finding, 2026-08-05).
    expect(propertyColumnId("updatedAt")).toBe("prop:updatedAt")
    expect(sortPropertyOf("prop:number")).toBe("number")
    expect(sortPropertyOf("updatedAt")).toBe("updatedAt")
  })

  it("routes a wire sort property back to the owning column", () => {
    expect(columnIdOf("updatedAt")).toBe("updatedAt")
    expect(columnIdOf("title")).toBe("title")
    expect(columnIdOf("prominence")).toBe("prop:prominence")
  })
})

function kind(identity: string): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "builtin",
    description: "",
    definition: { properties: {} },
  }
}

describe("the columns a kind opens without", () => {
  // A function's declaration is mostly machinery, and three of its properties
  // are now IN its title (it titles itself with its full reference), so the
  // browse table opened nine columns wide saying the same thing twice.
  it("hides a core function's machinery and keeps what a reader reads", () => {
    const hidden = defaultHiddenColumns(
      kind("substrate.reamde.dev/core/function")
    )
    for (const name of [
      "authority",
      "package",
      "version",
      "source",
      "arguments",
      "effect",
      "confirmation",
    ]) {
      expect(hidden).toContain(propertyColumnId(name))
    }
    for (const name of ["description", "runtime", "timeout"]) {
      expect(hidden).not.toContain(propertyColumnId(name))
    }
  })

  // Hiding a property of a kind this console did not ship would be guessing at
  // somebody else's vocabulary, so every other kind opens with all of them.
  it("hides nothing on a kind it does not ship", () => {
    expect(defaultHiddenColumns(kind("ada.example.com/tasks/task"))).toEqual([])
  })
})

describe("the temporal columns", () => {
  it("opens a column for the hot column core's qualified trait binds", () => {
    const task: KindInfo = {
      ...kind("ada.example.com/tasks/task"),
      source: "installed",
      definition: {
        traits: ["substrate.reamde.dev/core/temporal(point: dueAt)"],
        properties: {},
      },
    }
    expect(buildColumns(task, [task]).map((c) => c.id)).toContain("dueAt")
  })
})

// ── reference columns read as names ────────────────────────────────────────

/** A reference stores the referent's PATH and nothing else, so a cell built
 * from the value alone prints a record id where a name belongs (owner report,
 * 2026-09-18: a task's `assignee` read as an id). The page expands its
 * references on the list read and hands the titles down; what follows is what
 * the cell does with them, and what it does without them. */

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: ReactNode
  }) =>
    createElement(
      "a",
      {
        href: Object.entries(params ?? {}).reduce(
          (path, [key, value]) => path.replace(`$${key}`, value),
          to
        ),
        ...rest,
      },
      children
    ),
}))

const TASK: KindInfo = {
  ...kind("ada.example.com/tasks/task"),
  source: "installed",
  definition: {
    properties: {
      assignee: { type: "reference", kind: "ada.example.com/people/person" },
      watchers: {
        type: "reference",
        kind: "ada.example.com/people/person",
        repeated: true,
      },
    },
  },
}

const REGISTRY = [TASK, kind("ada.example.com/people/person")]

/** Render one column's cell over one stored value. */
function renderCell(
  columnId: string,
  value: unknown,
  titles?: ReferenceTitles
) {
  const column = buildColumns(TASK, REGISTRY, titles).find(
    (c) => c.id === columnId
  )
  const cell = column?.cell as (ctx: { getValue: () => unknown }) => ReactNode
  return render(cell({ getValue: () => value }))
}

afterEach(cleanup)

describe("a reference column", () => {
  it("reads as the referent's title when the page expanded it", () => {
    const { container } = renderCell(
      propertyColumnId("assignee"),
      { ref: "ada.example.com/people/person/p1" },
      new Map([["ada.example.com/people/person/p1", "Ada Lovelace"]])
    )
    expect(container.textContent).toBe("Ada Lovelace")
    expect(container.textContent).not.toContain("p1")
    expect(container.querySelector("a")?.getAttribute("href")).toBe(
      "/data/ada.example.com/people/person/p1"
    )
  })

  // The expansion is a sidecar: a page that could not carry it (the server
  // refused the expand and the read degraded) still has rows, and the pill
  // names the referent by its kind, never by its bare id.
  it("names the referent by its kind when no title came back", () => {
    const { container } = renderCell(propertyColumnId("assignee"), {
      ref: "ada.example.com/people/person/p1",
    })
    expect(container.textContent).toBe("Untitled person")
    expect(container.querySelector("a")).not.toBeNull()
  })

  it("shows the first referent of a repeated reference and how many more", () => {
    const { container } = renderCell(
      propertyColumnId("watchers"),
      [
        { ref: "ada.example.com/people/person/p1" },
        { ref: "ada.example.com/people/person/p2" },
      ],
      new Map([
        ["ada.example.com/people/person/p1", "Ada Lovelace"],
        ["ada.example.com/people/person/p2", "Grace Hopper"],
      ])
    )
    expect(
      [...container.querySelectorAll("a")].map((a) => a.textContent)
    ).toEqual(["Ada Lovelace"])
    expect(container.textContent).toContain("+1")
  })

  // A reference may name a kind nobody installed. There is no page to link
  // to, the batch never asks about it, and it reads by its kind, never its id.
  it("stays unlinked for a kind the registry does not have", () => {
    const { container } = renderCell(propertyColumnId("assignee"), {
      ref: "ada.example.com/crm/lead/7",
    })
    expect(container.querySelector("a")).toBeNull()
    expect(container.textContent).toBe("Untitled lead")
  })
})

describe("what the page asks the list to expand", () => {
  it("names every reference the kind declares, and nothing else", () => {
    expect(expandableReferences(TASK)).toEqual(["assignee", "watchers"])
  })
})

describe("the opening columns", () => {
  const RICH: KindInfo = {
    ...kind("ada.example.com/tasks/task"),
    source: "installed",
    definition: {
      displayTemplate: "{name|title}",
      traits: ["substrate.reamde.dev/core/temporal(point: dueAt)"],
      properties: {
        name: { type: "string" },
        notes: { type: "markdown" },
        priority: { type: "enum", values: ["low", "high"] },
        owner: { type: "reference", kind: "ada.example.com/people/person" },
        status: { type: "state", states: ["open", "done"], initial: "open" },
        dueAt: { type: "datetime" },
      },
    },
  }

  it("leads with the title, then state, time, references and enums", () => {
    expect(buildColumns(RICH, [RICH]).map((c) => c.id)).toEqual([
      "title",
      "prop:status",
      "dueAt",
      "prop:owner",
      "prop:priority",
      "prop:name",
      "updatedAt",
    ])
  })

  it("adds the record id in technical mode", () => {
    expect(
      buildColumns(RICH, [RICH], undefined, { technical: true }).map(
        (c) => c.id
      )
    ).toContain("id")
  })

  it("hides the properties the title is made of", () => {
    expect(defaultHiddenColumns(RICH)).toEqual([propertyColumnId("name")])
  })
})
