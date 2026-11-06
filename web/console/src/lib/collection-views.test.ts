/** A collection's shipped saved views (decision record 0229): a view's
 * `where` converts to the filter bar's tokens and back without changing what
 * it asks the server, a condition the bar cannot show leaves the view listed
 * with the reason, and every view the tree ships converts. */

import { describe, expect, it } from "vitest"
import { parseAllDocuments } from "yaml"

import type { SubstrateRecord } from "@/lib/api/types"
import {
  COLLECTION_VIEW_KIND,
  collectionViewOf,
  collectionViewWrite,
  collectionViewsOf,
  defaultView,
  tokensWhere,
  whereTokens,
} from "@/lib/collection-views"
import type { DeclaredProperty } from "@/lib/definition"
import { matchingView, type ViewState } from "@/lib/saved-views"
import { SHIPPED_UNITS } from "@/lib/unit-graph.test-fixtures"

const prop = (
  name: string,
  kind: string,
  repeated = false
): DeclaredProperty => ({ name, kind, repeated })

// The contact kind the contacts plan proposes, as far as its views read it.
const CONTACT = [
  prop("contactType", "enum"),
  prop("prominence", "enum"),
  prop("name", "string"),
  prop("weight", "quantity"),
  prop("emails", "email", true),
]

const columns = {
  idOf: (p: string) => (p === "updatedAt" ? p : `prop:${p}`),
  all: [
    "title",
    "id",
    "prop:name",
    "prop:contactType",
    "prop:prominence",
    "updatedAt",
  ],
}

const record = (
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord => ({
  id,
  kind: COLLECTION_VIEW_KIND,
  labels: {},
  version: 1,
  createdAt: "2026-11-06T00:00:00Z",
  updatedAt: "2026-11-06T00:00:00Z",
  properties,
})

describe("a view's where and the bar's tokens", () => {
  // The six views of the contacts plan, and the forms the bar has a control
  // for beside them.
  const wheres: Record<string, unknown>[] = [
    { contactType: { eq: "person" }, prominence: { eq: "known" } },
    { contactType: { eq: "organization" } },
    { contactType: { eq: "group" } },
    { contactType: { eq: "bot" } },
    { contactType: { exists: false } },
    { prominence: { eq: "hidden" } },
    { contactType: { in: ["person", "bot"] } },
    { emails: { contains: "a@example.com" } },
    { name: { match: "ada lov*" } },
    { name: { prefix: "Ada" } },
    { weight: { lt: { value: "155", unit: "lb" } } },
  ]

  it.each(wheres.map((w) => [JSON.stringify(w), w] as const))(
    "%s reads back as the same where",
    (_, where) => {
      const tokens = whereTokens(where, CONTACT)
      expect(Array.isArray(tokens)).toBe(true)
      expect(tokensWhere(SHIPPED_UNITS, tokens as string[], CONTACT)).toEqual(
        where
      )
    }
  )

  it("is the bar's own spelling, one condition per row", () => {
    expect(
      whereTokens(
        { prominence: { eq: "known" }, contactType: { exists: false } },
        CONTACT
      )
    ).toEqual(["contactType~exists~false", "prominence~eq~known"])
    expect(whereTokens(undefined, CONTACT)).toEqual([])
  })

  it("says why the bar cannot show a condition", () => {
    const problem = (where: unknown) =>
      (whereTokens(where, CONTACT) as { problem?: string }).problem
    expect(problem({ name: { gt: "M" } })).toContain("“gt” condition on name")
    expect(problem({ contactType: { in: [null, "bot"] } })).toContain("“in”")
    expect(problem({ name: { eq: "Lovelace, Ada" } })).toContain("“eq”")
    expect(problem({ name: { near: "x" } })).toContain("“near”")
    expect(problem({ name: "Ada" })).toContain("not an object")
    expect(problem(["contactType"])).toContain("one condition per property")
  })
})

describe("a shipped view record", () => {
  it("reads into the strip's view, the columns it names shown and the rest hidden", () => {
    const view = collectionViewOf(
      record("contact-people", {
        name: "People",
        where: { contactType: { eq: "person" } },
        sort: "name:asc",
        columns: ["name", "updatedAt", "gone"],
        group: "prominence",
        default: true,
      }),
      "substrate.reamde.dev/core/contact",
      CONTACT,
      columns
    )
    expect(view).toEqual({
      id: `${COLLECTION_VIEW_KIND}/contact-people`,
      collection: "substrate.reamde.dev/core/contact",
      name: "People",
      record: "contact-people",
      isDefault: true,
      filter: ["contactType~eq~person"],
      sort: "name:asc",
      columns: ["prop:name", "updatedAt"],
      hidden: ["prop:contactType", "prop:prominence"],
      group: "prominence",
    })
  })

  it("keeps a view it cannot show, with the reason, and never opens on it", () => {
    const views = collectionViewsOf(
      [
        record("b", { name: "Later", where: { prominence: { eq: "known" } } }),
        record("a", {
          name: "Ranged",
          where: { name: { gt: "M" } },
          default: true,
        }),
        record("c", { name: "Sorting", default: true }),
      ],
      "x.example.com/p/k",
      CONTACT,
      columns
    )
    expect(views.map((v) => v.name)).toEqual(["Ranged", "Later", "Sorting"])
    expect(views[0].problem).toContain("“gt”")
    expect(defaultView(views)?.name).toBe("Sorting")
  })

  it("lights its tab where the page shows the rows with no type", () => {
    const view = collectionViewOf(
      record("to-sort", {
        name: "To sort",
        where: { contactType: { exists: false } },
      }),
      "k",
      CONTACT,
      columns
    )
    const state: ViewState = {
      filter: ["contactType~exists~false"],
      sort: "updatedAt:desc",
      nest: true,
      columns: columns.all,
      hidden: [],
    }
    expect(matchingView([view], state, "updatedAt:desc")?.id).toBe(view.id)
    expect(
      matchingView([view], { ...state, filter: [] }, "updatedAt:desc")
    ).toBeUndefined()
  })
})

describe("saving the page into a shipped view", () => {
  it("writes each dimension, and null for one left at the default", () => {
    const state: ViewState = {
      filter: ["contactType~exists~false"],
      sort: "updatedAt:desc",
      nest: true,
      columns: ["title", "prop:name", "prop:contactType", "updatedAt"],
      hidden: ["prop:contactType"],
    }
    const propertyOf = (id: string) =>
      id === "title" ? undefined : id.replace(/^prop:/, "")
    expect(
      collectionViewWrite(
        state,
        "updatedAt:desc",
        tokensWhere(SHIPPED_UNITS, state.filter, CONTACT),
        propertyOf
      )
    ).toEqual({
      where: { contactType: { exists: false } },
      sort: null,
      columns: ["name", "updatedAt"],
      group: null,
    })
    expect(
      collectionViewWrite(
        { ...state, filter: [], sort: "name:asc", group: "prominence" },
        "updatedAt:desc",
        undefined,
        propertyOf
      )
    ).toMatchObject({ where: null, sort: "name:asc", group: "prominence" })
  })
})

// ── every view the tree ships ───────────────────────────────────────────────

const shippedFiles = import.meta.glob<string>(
  ["../../../../samples/*/*.yaml", "../../../../kinds/*/*/*.yaml"],
  { query: "?raw", import: "default", eager: true }
)

interface Doc {
  kind?: string
  metadata?: { id?: string }
  data?: {
    properties?: Record<string, unknown>
  }
}

const DOCS: Doc[] = Object.values(shippedFiles).flatMap((raw) =>
  parseAllDocuments(raw).map((d) => d.toJS() as Doc)
)

const KIND_PREFIX = "substrate.reamde.dev/core/kind/"

// Every kind the tree declares, by reference, with its properties as the
// filter bar reads them.
const SHIPPED_KINDS = new Map(
  DOCS.filter(
    (d) => d?.kind === "substrate.reamde.dev/core/kind" && d.metadata?.id
  ).map((d) => {
    const declared = (d.data?.properties ?? {}) as Record<
      string,
      { type?: string; repeated?: boolean }
    >
    return [
      d.metadata!.id!,
      Object.entries(declared).map(([name, p]) =>
        prop(name, p.type ?? "string", Boolean(p.repeated))
      ),
    ] as const
  })
)

const SHIPPED_VIEWS = DOCS.filter((d) => d?.kind === COLLECTION_VIEW_KIND)

describe("the shipped collection views", () => {
  it("are read", () => {
    expect(SHIPPED_VIEWS.length).toBeGreaterThan(0)
  })

  it.each(SHIPPED_VIEWS.map((d) => [d.metadata?.id ?? "", d] as const))(
    "%s names a shipped kind and shows on the strip as it asks the server",
    (id, doc) => {
      const props = doc.data?.properties ?? {}
      const applies = (props.appliesTo as { ref?: string } | undefined)?.ref
      expect(applies?.startsWith(KIND_PREFIX)).toBe(true)
      const declared = SHIPPED_KINDS.get(applies!.slice(KIND_PREFIX.length))
      expect(declared).toBeDefined()
      const view = collectionViewOf(
        record(id, props),
        applies!,
        declared!,
        columns
      )
      expect(view.problem).toBeUndefined()
      expect(tokensWhere(SHIPPED_UNITS, view.filter ?? [], declared!)).toEqual(
        props.where
      )
    }
  )
})
