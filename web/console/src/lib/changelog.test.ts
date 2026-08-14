import { describe, expect, it } from "vitest"

import type { ChangeRow, KindInfo } from "@/lib/api/types"

import {
  changeEffects,
  changeSummary,
  changedProperties,
  EMPTY_LIVE_FEED,
  flushLive,
  isVocabularyChange,
  mergeFeed,
  NAMED_PAYLOAD_KEYS,
  parseTimeInput,
  pushLive,
  changelogFacetFields,
  toChangelogQuery,
  verbOf,
} from "./changelog"

const T0 = Date.parse("2026-08-05T12:00:00Z")

function row(overrides: Partial<ChangeRow> & { seq: number }): ChangeRow {
  return {
    ts: new Date(T0 + overrides.seq * 1000).toISOString(),
    actor: "github.bundles.substrate.reamde.dev",
    op: "put",
    recordId: `e-${overrides.seq}`,
    kind: "github.bundles.substrate.reamde.dev/issue",
    ...overrides,
  }
}

describe("isVocabularyChange", () => {
  it("knows the v1 registry vocabulary, all under the core authority", () => {
    for (const k of [
      "core.substrate.reamde.dev/kind",
      "core.substrate.reamde.dev/propertytype",
      "core.substrate.reamde.dev/trait",
      "core.substrate.reamde.dev/recordmapping",
      "core.substrate.reamde.dev/function",
      "core.substrate.reamde.dev/authority",
      "core.substrate.reamde.dev/actor",
    ]) {
      expect(isVocabularyChange(row({ seq: 1, kind: k }))).toBe(true)
    }
  })

  it("the old pre-rename kinds no longer classify", () => {
    for (const k of [
      "core.substrate.reamde.dev/entitytype", // v0 — became entitykind, then kind
      "core.substrate.reamde.dev/entitykind", // the entity→record rename made this `kind`
      "core.substrate.reamde.dev/entitymapping", // …and this `recordmapping`
      "core.substrate.reamde.dev/schemagroup", // now authority
      "core.substrate.reamde.dev/connector", // removed with the connectors plane
    ]) {
      expect(isVocabularyChange(row({ seq: 1, kind: k }))).toBe(false)
    }
  })

  it("data kinds are not schema, even under the core authority", () => {
    expect(
      isVocabularyChange(
        row({ seq: 1, kind: "core.substrate.reamde.dev/recordmergerequest" })
      )
    ).toBe(false)
    expect(
      isVocabularyChange(
        row({ seq: 1, kind: "people.substrate.reamde.dev/person" })
      )
    ).toBe(false)
  })

  it("registry names only count under the core authority", () => {
    expect(
      isVocabularyChange(
        row({ seq: 1, kind: "acme.substrate.reamde.dev/function" })
      )
    ).toBe(false)
  })
})

describe("the effects a write recorded", () => {
  /** A row whose payload carries the recorded effects. The wire key is an
   * INTERNAL name (engine/fold.go) — the test spells it because it is the wire,
   * and nothing the console renders may. */
  function withEffects(effects: unknown[]): ChangeRow {
    return row({ seq: 1, payload: { fold: effects } })
  }

  it("reads a record delta as what was set, cleared and moved", () => {
    expect(
      changeEffects(
        withEffects([
          {
            kind: "record",
            ref: "people.substrate.reamde.dev/person",
            id: "p1",
            delta: {
              set: { name: "Ada", email: "ada@example.com" },
              del: ["nickname"],
              title: "Ada",
              states: { status: "active" },
            },
          },
        ])
      )
    ).toEqual([
      {
        verb: "updated",
        target: "people.substrate.reamde.dev/person/p1",
        detail: "set name, email; cleared nickname; moved title, states",
      },
    ])
  })

  it("distinguishes a creation from a restoration from an update", () => {
    const verbs = changeEffects(
      withEffects([
        { kind: "record", ref: "k", id: "a", delta: { created: true } },
        { kind: "record", ref: "k", id: "b", delta: { restored: true } },
        { kind: "record", ref: "k", id: "c", delta: {} },
      ])
    ).map((e) => e.verb)
    expect(verbs).toEqual(["created", "restored", "updated"])
  })

  it("says every effect kind the engine can record", () => {
    expect(
      changeEffects(
        withEffects([
          { kind: "tombstone", ref: "k", id: "a", finalizer: "merge" },
          { kind: "purge", ref: "k", id: "b" },
          { kind: "bump", ref: "k", id: "c" },
          {
            kind: "edge",
            ref: "k",
            id: "d",
            rel: "member",
            dstType: "o",
            dst: "o1",
          },
          {
            kind: "unedge",
            ref: "k",
            id: "e",
            rel: "member",
            dstType: "o",
            dst: "o1",
          },
          {
            kind: "edge1",
            ref: "k",
            id: "f",
            rel: "owner",
            dstType: "p",
            dst: "p1",
          },
          {
            kind: "annotation",
            ref: "k",
            id: "g",
            key: "owner/note",
            value: "hi",
          },
          { kind: "annotation", ref: "k", id: "h", key: "owner/note" },
          {
            kind: "manager",
            ref: "k",
            id: "i",
            property: "name",
            actor: "sync",
            tier: "bundle",
          },
          { kind: "manager", ref: "k", id: "j", property: "name" },
          { kind: "former", ref: "k", formerId: "old", id: "new" },
        ])
      )
    ).toEqual([
      { verb: "deleted", target: "k/a", detail: "held by merge" },
      {
        verb: "purged",
        target: "k/b",
        detail: "and everything hanging off it",
      },
      { verb: "touched", target: "k/c", detail: "version only" },
      { verb: "linked", target: "k/d", detail: "member → o/o1" },
      { verb: "unlinked", target: "k/e", detail: "member → o/o1" },
      {
        verb: "relinked",
        target: "k/f",
        detail: "owner now points only at p/p1",
      },
      { verb: "annotated", target: "k/g", detail: "owner/note" },
      { verb: "un-annotated", target: "k/h", detail: "owner/note" },
      { verb: "reassigned", target: "k/i", detail: "name → sync (bundle)" },
      { verb: "released", target: "k/j", detail: "name has no manager" },
      { verb: "aliased", target: "k/old", detail: "now resolves to new" },
    ])
  })

  it("counts what a resync restated, and over which records", () => {
    expect(
      changeEffects(
        withEffects([
          {
            kind: "resync",
            scope: [
              { kind: "people.substrate.reamde.dev/person", id: "winner" },
              { kind: "people.substrate.reamde.dev/person", id: "loser" },
            ],
            rows: {
              edges: [{}, {}],
              annotations: [{}],
              formerIds: [{}],
            },
          },
        ])
      )
    ).toEqual([
      {
        verb: "restated",
        target: "",
        detail:
          "2 edges, 1 annotation, 1 former id — on people.substrate.reamde.dev/person/winner, people.substrate.reamde.dev/person/loser",
      },
    ])
  })

  it("keeps falsy annotation values as values — only an absent one deletes", () => {
    const verbs = changeEffects(
      withEffects([
        { kind: "annotation", ref: "k", id: "a", key: "n", value: false },
        { kind: "annotation", ref: "k", id: "b", key: "n", value: 0 },
        { kind: "annotation", ref: "k", id: "c", key: "n", value: "" },
        { kind: "annotation", ref: "k", id: "d", key: "n", value: null },
        { kind: "annotation", ref: "k", id: "e", key: "n" },
      ])
    ).map((e) => e.verb)
    expect(verbs).toEqual([
      "annotated",
      "annotated",
      "annotated",
      "un-annotated",
      "un-annotated",
    ])
  })

  it("names an effect kind it does not know rather than dropping it", () => {
    expect(
      changeEffects(withEffects([{ kind: "teleport", ref: "k", id: "a" }]))
    ).toEqual([{ verb: "teleport (unrecognized)", target: "k/a", detail: "" }])
    expect(changeEffects(withEffects([{ ref: "k", id: "a" }]))).toEqual([
      { verb: "unrecognized", target: "k/a", detail: "" },
    ])
  })

  it("is empty for a row that recorded none, and never throws on junk", () => {
    expect(changeEffects(row({ seq: 1 }))).toEqual([])
    expect(changeEffects(row({ seq: 1, payload: {} }))).toEqual([])
    expect(
      changeEffects(row({ seq: 1, payload: { fold: "nonsense" } }))
    ).toEqual([])
    expect(changeEffects(withEffects([null, 7, "x"]))).toEqual([])
  })

  it("keeps the effects key out of the leftover JSON the detail dumps", () => {
    // The detail band renders every unnamed payload key as raw JSON. The
    // effects key is claimed here precisely so it never lands there.
    expect(NAMED_PAYLOAD_KEYS.has("fold")).toBe(true)
  })
})

describe("the summary voice", () => {
  it("verbs each op honestly", () => {
    expect(verbOf(row({ seq: 1, op: "put", payload: { created: true } }))).toBe(
      "created"
    )
    expect(verbOf(row({ seq: 1, op: "put" }))).toBe("updated")
    expect(verbOf(row({ seq: 1, op: "patch" }))).toBe("updated")
    expect(verbOf(row({ seq: 1, op: "link" }))).toBe("linked")
    expect(verbOf(row({ seq: 1, op: "merge" }))).toBe("merged")
    expect(verbOf(row({ seq: 1, op: "delete" }))).toBe("deleted")
    expect(verbOf(row({ seq: 1, op: "gc" }))).toBe("collected")
  })

  it("names the touched properties, count first", () => {
    expect(
      changeSummary(
        row({ seq: 1, payload: { properties: ["title", "state", "body"] } })
      )
    ).toBe("3 properties: title, state, body")
    expect(
      changeSummary(row({ seq: 1, payload: { properties: ["title"] } }))
    ).toBe("property: title")
  })

  it("speaks a trigger stance plainly — short name, plain verb", () => {
    expect(
      changeSummary(
        row({
          seq: 1,
          triggers: [
            {
              trigger: "on-githubwriteback.github.bundles.substrate.reamde.dev",
              callable: "githubwriteback.github.bundles.substrate.reamde.dev",
              state: "processed",
            },
          ],
        })
      )
    ).toBe("on-githubwriteback processed")
  })

  it("carries a parked trigger's error", () => {
    expect(
      changeSummary(
        row({
          seq: 1,
          triggers: [
            {
              trigger: "on-dedupe.core.substrate.reamde.dev",
              callable: "dedupe.core.substrate.reamde.dev",
              state: "parked",
              error: "409",
            },
          ],
        })
      )
    ).toBe("on-dedupe parked: 409")
  })

  it("joins properties, restored and triggers with commas", () => {
    expect(
      changeSummary(
        row({
          seq: 1,
          payload: { properties: ["a"], restored: true },
          triggers: [
            { trigger: "tr.g.dev", callable: "fn.g.dev", state: "pending" },
          ],
        })
      )
    ).toBe("property: a, restored, tr pending")
  })

  it("says nothing when the payload says nothing", () => {
    expect(changeSummary(row({ seq: 1 }))).toBe("")
  })

  it("reads changed property names off the payload, or none", () => {
    expect(
      changedProperties(row({ seq: 1, payload: { properties: ["x", "y"] } }))
    ).toEqual(["x", "y"])
    expect(changedProperties(row({ seq: 1 }))).toEqual([])
  })
})

describe("the live buffer", () => {
  it("prepends while at the top", () => {
    let feed = pushLive(EMPTY_LIVE_FEED, row({ seq: 1 }), false)
    feed = pushLive(feed, row({ seq: 2 }), false)
    expect(feed.rows.map((r) => r.seq)).toEqual([2, 1])
    expect(feed.pending).toHaveLength(0)
  })

  it("holds rows aside while scrolled away, then flushes in order", () => {
    let feed = pushLive(EMPTY_LIVE_FEED, row({ seq: 1 }), false)
    feed = pushLive(feed, row({ seq: 2 }), true)
    feed = pushLive(feed, row({ seq: 3 }), true)
    expect(feed.rows.map((r) => r.seq)).toEqual([1])
    expect(feed.pending.map((r) => r.seq)).toEqual([3, 2])
    feed = flushLive(feed)
    expect(feed.rows.map((r) => r.seq)).toEqual([3, 2, 1])
    expect(feed.pending).toHaveLength(0)
  })

  it("delivers each seq exactly once", () => {
    let feed = pushLive(EMPTY_LIVE_FEED, row({ seq: 1 }), false)
    feed = pushLive(feed, row({ seq: 1 }), false)
    feed = pushLive(feed, row({ seq: 1 }), true)
    expect(feed.rows).toHaveLength(1)
    expect(feed.pending).toHaveLength(0)
  })

  it("caps the tail's memory", () => {
    let feed = EMPTY_LIVE_FEED
    for (let i = 1; i <= 5; i++)
      feed = pushLive(feed, row({ seq: i }), false, 3)
    expect(feed.rows.map((r) => r.seq)).toEqual([5, 4, 3])
  })
})

describe("mergeFeed", () => {
  it("joins live over history newest-first, deduped by seq", () => {
    const merged = mergeFeed(
      [row({ seq: 5 }), row({ seq: 4 })],
      [row({ seq: 4 }), row({ seq: 3 }), row({ seq: 2 })]
    )
    expect(merged.map((r) => r.seq)).toEqual([5, 4, 3, 2])
  })
})

const KINDS: KindInfo[] = [
  {
    identity: "github.bundles.substrate.reamde.dev/issue",
    name: "issue",
    authority: "github.bundles.substrate.reamde.dev",
    version: "",
    plural: "issues",
    source: "installed",
  },
  {
    identity: "people.substrate.reamde.dev/person",
    name: "person",
    authority: "people.substrate.reamde.dev",
    version: "",
    plural: "people",
    source: "builtin",
  },
]

describe("toChangelogQuery", () => {
  it("passes kinds, actors and ops straight to the wire", () => {
    const q = toChangelogQuery(
      [
        {
          field: "kind",
          op: "eq",
          value: "people.substrate.reamde.dev/person",
        },
        { field: "actor", op: "eq", value: "owner,system" },
        { field: "op", op: "eq", value: "merge" },
      ],
      KINDS
    )
    expect(q.filter.kinds).toEqual(["people.substrate.reamde.dev/person"])
    expect(q.filter.actors).toEqual(["owner", "system"])
    expect(q.filter.ops).toEqual(["merge"])
  })

  it("expands an authority facet into its registry kinds (still server-side)", () => {
    const q = toChangelogQuery(
      [
        {
          field: "authority",
          op: "eq",
          value: "github.bundles.substrate.reamde.dev",
        },
      ],
      KINDS
    )
    expect(q.filter.kinds).toEqual([
      "github.bundles.substrate.reamde.dev/issue",
    ])
  })

  it("intersects explicit kinds with an authority — an empty AND matches nothing", () => {
    const q = toChangelogQuery(
      [
        {
          field: "kind",
          op: "eq",
          value: "people.substrate.reamde.dev/person",
        },
        {
          field: "authority",
          op: "eq",
          value: "github.bundles.substrate.reamde.dev",
        },
      ],
      KINDS
    )
    expect(q.filter.kinds).toEqual(["∅"])
  })

  it("reads since/until as instants and q as a substring", () => {
    const q = toChangelogQuery(
      [
        { field: "since", op: "eq", value: "2026-08-05T12:00:00Z" },
        { field: "until", op: "eq", value: "2026-08-05T13:00:00Z" },
        { field: "q", op: "eq", value: "pull-29" },
      ],
      KINDS
    )
    expect(q.sinceMs).toBe(Date.parse("2026-08-05T12:00:00Z"))
    expect(q.untilMs).toBe(Date.parse("2026-08-05T13:00:00Z"))
    expect(q.filter.q).toBe("pull-29")
  })

  it("tolerates an unparsable instant by dropping it", () => {
    const q = toChangelogQuery(
      [{ field: "until", op: "eq", value: "yesterdayish" }],
      KINDS
    )
    expect(q.untilMs).toBeUndefined()
  })
})

describe("parseTimeInput", () => {
  it("takes ISO and the space-separated spelling", () => {
    expect(parseTimeInput("2026-08-05T12:00:00Z")).toBe(T0)
    expect(parseTimeInput(" 2026-08-05 ")).toBeDefined()
  })
})

describe("changelogFacetFields", () => {
  it("offers kind/authority/actor/op/since/until/q — actor drops when fixed", () => {
    const fields = changelogFacetFields({ kinds: KINDS, actors: ["owner"] })
    expect(fields.map((f) => f.name)).toEqual([
      "kind",
      "authority",
      "actor",
      "op",
      "since",
      "until",
      "q",
    ])
    const fixed = changelogFacetFields({
      kinds: KINDS,
      actors: ["owner"],
      fixedActor: true,
    })
    expect(fixed.some((f) => f.name === "actor")).toBe(false)
  })
})
