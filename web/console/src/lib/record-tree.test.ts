/** The tree a self-referencing kind folds into: which property nests it, how
 * a level's read is asked for, and how `resolveTree` turns the cache into rows
 * and the next reads to make. */

import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

import {
  childrenFilter,
  hasToggles,
  matchedRoots,
  matchingParentsFilter,
  nestingProperty,
  parentIdsOf,
  parentIdOf,
  resolveTree,
  rootsFilter,
  selfReferences,
  type ChildrenPage,
} from "./record-tree"

const TEAM = "acme.example.com/people/team"
const PERSON = "acme.example.com/people/person"

function kind(identity: string, properties: Record<string, unknown>): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: { authority, package: pkg, properties },
  }
}

const team = kind(TEAM, {
  name: { type: "string" },
  parent: { type: "reference", kind: TEAM },
  members: { type: "reference", kind: PERSON, repeated: true },
})

function record(id: string, parent?: string): SubstrateRecord {
  return {
    id,
    kind: TEAM,
    properties: parent
      ? { name: id, parent: { ref: `${TEAM}/${parent}` } }
      : { name: id },
    labels: {},
    version: 1,
    createdAt: "2026-09-22T00:00:00Z",
    updatedAt: "2026-09-22T00:00:00Z",
  }
}

describe("nestingProperty", () => {
  it("finds the pin at the kind itself", () => {
    expect(nestingProperty(team)?.name).toBe("parent")
  })

  it("nests by nothing for a bare pin: a pin is a full identity", () => {
    const bare = kind(TEAM, { parent: { type: "reference", kind: "team" } })
    expect(selfReferences(bare)).toEqual([])
    expect(nestingProperty(bare)).toBeUndefined()
  })

  it("ignores repeated and keyed pointers, and pointers at other kinds", () => {
    const k = kind(TEAM, {
      members: { type: "reference", kind: TEAM, repeated: true },
      byRole: { type: "reference", kind: TEAM, keyed: true },
      owner: { type: "reference", kind: PERSON },
    })
    expect(selfReferences(k)).toEqual([])
    expect(nestingProperty(k)).toBeUndefined()
  })

  it("prefers `parent`, else the first self-reference by name", () => {
    const withParent = kind(TEAM, {
      above: { type: "reference", kind: TEAM },
      parent: { type: "reference", kind: TEAM },
    })
    expect(nestingProperty(withParent)?.name).toBe("parent")
    const without = kind(TEAM, {
      under: { type: "reference", kind: TEAM },
      above: { type: "reference", kind: TEAM },
    })
    expect(nestingProperty(without)?.name).toBe("above")
  })
})

describe("the level filters", () => {
  it("add the parent predicate beside the view's own filter", () => {
    expect(
      rootsFilter(
        { properties: { status: { eq: "active" } }, search: "x" },
        "parent"
      )
    ).toEqual({
      search: "x",
      properties: { status: { eq: "active" }, parent: { exists: false } },
    })
    expect(childrenFilter(undefined, TEAM, "parent", ["a", "b"])).toEqual({
      properties: { parent: { in: [`${TEAM}/a`, `${TEAM}/b`] } },
    })
  })
})

describe("parentIdOf", () => {
  it("reads the served object and the authored string", () => {
    expect(parentIdOf(record("infra", "platform"), "parent")).toBe("platform")
    const authored = { ...record("x"), properties: { parent: `${TEAM}/y` } }
    expect(parentIdOf(authored, "parent")).toBe("y")
  })

  it("follows neither a pointer at another kind nor an absent one", () => {
    const other = {
      ...record("x"),
      properties: { parent: { ref: `${PERSON}/ada` } },
    }
    expect(parentIdOf(other, "parent")).toBeUndefined()
    expect(parentIdOf(record("x"), "parent")).toBeUndefined()
  })
})

describe("resolveTree", () => {
  const roots = [record("engineering"), record("design")]
  const platform = record("platform", "engineering")
  const product = record("product", "engineering")
  const infra = record("infra", "platform")
  const ids = (rows: SubstrateRecord[]) => rows.map((r) => r.id)
  /** A cache: level read (its parent ids, joined) → what it answered. */
  const cache =
    (answers: Record<string, ChildrenPage>) => (parents: readonly string[]) =>
      answers[parents.join(",")]

  it("names the roots' level read and holds every row pending until it answers", () => {
    const out = resolveTree({
      roots,
      property: "parent",
      expanded: new Set(),
      lookup: () => undefined,
    })
    expect(ids(out.rows)).toEqual(["engineering", "design"])
    expect(out.wanted).toEqual([["engineering", "design"]])
    expect(out.nodes.get("engineering")).toMatchObject({
      depth: 0,
      children: "pending",
      open: false,
    })
  })

  it("tells leaves from parents once the level answers whole", () => {
    const out = resolveTree({
      roots,
      property: "parent",
      expanded: new Set(),
      lookup: cache({
        "engineering,design": { records: [platform, product], complete: true },
      }),
    })
    expect(out.nodes.get("engineering")?.children).toBe("some")
    expect(out.nodes.get("design")?.children).toBe("none")
    // A closed parent still knows its children, which its badge counts.
    expect(
      out.nodes.get("engineering")?.childRecords?.map((r) => r.id)
    ).toEqual(["platform", "product"])
    expect(out.nodes.get("design")?.childRecords).toBeUndefined()
    expect(ids(out.rows)).toEqual(["engineering", "design"])
  })

  it("opens a row onto its children and names the next level's read", () => {
    const out = resolveTree({
      roots,
      property: "parent",
      expanded: new Set(["engineering", "design"]),
      lookup: cache({
        "engineering,design": { records: [platform, product], complete: true },
        "platform,product": { records: [infra], complete: true },
      }),
    })
    expect(ids(out.rows)).toEqual([
      "engineering",
      "platform",
      "product",
      "design",
    ])
    expect(out.wanted).toEqual([
      ["engineering", "design"],
      ["platform", "product"],
    ])
    expect(out.nodes.get("engineering")).toMatchObject({
      open: true,
      children: "some",
    })
    expect(out.nodes.get("platform")).toMatchObject({
      depth: 1,
      children: "some",
      open: false,
    })
    expect(out.nodes.get("product")).toMatchObject({
      depth: 1,
      children: "none",
    })
    // opening a leaf opens nothing
    expect(out.nodes.get("design")).toMatchObject({
      open: false,
      children: "none",
    })
  })

  it("groups a child under the parent it names by a former id", () => {
    const merged = { ...record("engineering"), formerIds: ["eng"] }
    const child = record("platform", "eng")
    const out = resolveTree({
      roots: [merged],
      property: "parent",
      expanded: new Set(["engineering"]),
      lookup: cache({
        engineering: { records: [child], complete: true },
        platform: { records: [], complete: true },
      }),
    })
    expect(ids(out.rows)).toEqual(["engineering", "platform"])
  })

  it("falls back to a read per row when the level's read was cut short", () => {
    const cut: ChildrenPage = { records: [platform], complete: false }
    const closed = resolveTree({
      roots,
      property: "parent",
      expanded: new Set(),
      lookup: cache({ "engineering,design": cut }),
    })
    expect(closed.nodes.get("design")?.children).toBe("unknown")
    expect(closed.wanted).toEqual([["engineering", "design"]])

    const loading = resolveTree({
      roots,
      property: "parent",
      expanded: new Set(["engineering"]),
      lookup: cache({ "engineering,design": cut }),
    })
    expect(loading.wanted).toEqual([["engineering", "design"], ["engineering"]])
    expect(loading.nodes.get("engineering")).toMatchObject({
      open: true,
      loading: true,
      children: "unknown",
    })

    const answered = resolveTree({
      roots,
      property: "parent",
      expanded: new Set(["engineering"]),
      lookup: cache({
        "engineering,design": cut,
        engineering: { records: [platform, product], complete: false },
        "platform,product": { records: [], complete: true },
      }),
    })
    expect(ids(answered.rows)).toEqual([
      "engineering",
      "platform",
      "product",
      "design",
    ])
    expect(answered.nodes.get("engineering")).toMatchObject({
      open: true,
      loading: false,
      truncated: true,
      children: "some",
    })
  })

  it("carries a refused read onto the row that asked", () => {
    const out = resolveTree({
      roots,
      property: "parent",
      expanded: new Set(["engineering"]),
      lookup: cache({
        "engineering,design": {
          records: [],
          complete: false,
          error: "level refused",
        },
        engineering: { records: [], complete: false, error: "no" },
      }),
    })
    expect(out.nodes.get("engineering")).toMatchObject({
      open: true,
      error: "no",
      loading: false,
    })
    expect(out.nodes.get("design")?.children).toBe("unknown")
  })

  it("places a record once, however the pointers loop", () => {
    const out = resolveTree({
      roots: [record("root")],
      property: "parent",
      expanded: new Set(["root", "a", "b"]),
      lookup: (parents) => {
        const key = parents.join(",")
        if (key === "root")
          return { records: [record("a", "root")], complete: true }
        if (key === "a") return { records: [record("b", "a")], complete: true }
        if (key === "b") return { records: [record("a", "b")], complete: true }
        return { records: [], complete: true }
      },
    })
    expect(ids(out.rows)).toEqual(["root", "a", "b"])
    expect(out.wanted).toEqual([["root"], ["a"], ["b"]])
  })
})

/** A filtered tree nests the MATCHES: the page is every match, a match whose
 * parent matches too sits under it (and nowhere else), and one whose parent
 * does not match stands at the top level carrying that parent as context. */
describe("the filtered tree", () => {
  const ids = (rows: SubstrateRecord[]) => rows.map((r) => r.id)
  // engineering > platform > infra, and design; say the filter matched
  // engineering, platform, infra and a design child, but not design.
  const engineering = record("engineering")
  const platform = record("platform", "engineering")
  const infra = record("infra", "platform")
  const research = record("research", "design")
  const page = [infra, research, engineering, platform]

  it("asks about each parent the page names, once, and never a self-pointer", () => {
    expect(
      parentIdsOf([...page, record("loop", "loop"), infra], "parent")
    ).toEqual(["platform", "design", "engineering"])
    expect(
      matchingParentsFilter({ search: "ops" }, ["platform", "design"])
    ).toEqual({ search: "ops", ids: ["platform", "design"] })
  })

  it("keeps a match whose parent matches off the top level, and gives the rest their context", () => {
    const out = matchedRoots(page, "parent", [platform, engineering])
    // infra sits under platform, platform under engineering: only the
    // match with no parent and the one whose parent missed stay on top.
    expect(ids(out.roots)).toEqual(["research", "engineering"])
    expect([...out.context]).toEqual([
      ["research", "acme.example.com/people/team/design"],
    ])
  })

  it("counts a parent matched under a former id", () => {
    const merged = { ...record("eng2"), formerIds: ["engineering"] }
    const out = matchedRoots([platform], "parent", [merged])
    expect(out.roots).toEqual([])
  })

  it("stands every match on top when no parent matched", () => {
    const out = matchedRoots(page, "parent", [])
    expect(ids(out.roots)).toEqual(ids(page))
    expect(out.context.size).toBe(3)
  })

  it("opens rows by themselves, and a toggle closes one", () => {
    const lookup = (parents: readonly string[]) => {
      const key = parents.join(",")
      if (key === "research,engineering")
        return { records: [platform], complete: true }
      if (key === "platform") return { records: [infra], complete: true }
      return { records: [], complete: true }
    }
    const open = resolveTree({
      roots: [research, engineering],
      property: "parent",
      expanded: new Set(),
      lookup,
      openByDefault: true,
    })
    expect(ids(open.rows)).toEqual([
      "research",
      "engineering",
      "platform",
      "infra",
    ])
    expect(open.nodes.get("infra")?.depth).toBe(2)
    // A leaf is never open, whatever the default.
    expect(open.nodes.get("research")?.open).toBe(false)

    const closed = resolveTree({
      roots: [research, engineering],
      property: "parent",
      expanded: new Set(["platform"]),
      lookup,
      openByDefault: true,
    })
    expect(ids(closed.rows)).toEqual(["research", "engineering", "platform"])
  })

  it("leaves a row whose level was cut short closed until it is asked", () => {
    const out = resolveTree({
      roots: [engineering],
      property: "parent",
      expanded: new Set(),
      lookup: () => ({ records: [platform], complete: false }),
      openByDefault: true,
    })
    expect(out.nodes.get("engineering")).toMatchObject({
      children: "unknown",
      open: false,
    })
    expect(out.wanted).toEqual([["engineering"]])
  })
})

/** The chevron's column is reserved on every row when any row can open, and
 * on none when nothing can: a tree with nothing to open is a flat table. */
describe("hasToggles", () => {
  const node = (children: "pending" | "unknown" | "none" | "some") => ({
    id: children,
    depth: 0,
    children,
    open: false,
    loading: false,
    truncated: false,
  })
  it("is off while nothing is known and when every row is a leaf", () => {
    expect(hasToggles(new Map())).toBe(false)
    expect(hasToggles(new Map([["a", node("pending")]]))).toBe(false)
    expect(
      hasToggles(
        new Map([
          ["a", node("none")],
          ["b", node("none")],
        ])
      )
    ).toBe(false)
  })
  it("is on as soon as one row has children, or may have", () => {
    expect(
      hasToggles(
        new Map([
          ["a", node("none")],
          ["b", node("some")],
        ])
      )
    ).toBe(true)
    expect(hasToggles(new Map([["a", node("unknown")]]))).toBe(true)
  })
})
