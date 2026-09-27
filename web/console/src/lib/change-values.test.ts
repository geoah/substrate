import { describe, expect, it } from "vitest"

import type { ChangeRow, PropertyChange } from "@/lib/api/types"
import type { PropSpec } from "@/lib/record-schema"
import {
  hostWritten,
  netMoves,
  readerMoves,
  rowValues,
  shortText,
  VALUE_CHARS,
  type ValueMove,
} from "./change-values"

const TASK = "samples.substrate.reamde.dev/tasks/task"

function row(
  seq: number,
  properties?: PropertyChange[],
  names = (properties ?? []).map((p) => p.name)
): ChangeRow {
  return {
    seq,
    ts: new Date(seq * 1000).toISOString(),
    actor: "console",
    op: "patch",
    recordId: "t1",
    kind: TASK,
    payload: names.length ? { properties: names } : undefined,
    affected: [{ kind: TASK, id: "t1", version: seq, properties }],
  }
}

describe("rowValues", () => {
  it("reads the addressed record's values, or nothing from an older server", () => {
    const pcs = [{ name: "priority", before: "high", after: "urgent" }]
    expect(rowValues(row(2, pcs))).toEqual(pcs)
    expect(rowValues(row(2, pcs), "other")).toBeUndefined()
    expect(rowValues(row(2, undefined, ["priority"]))).toBeUndefined()
  })
})

describe("netMoves", () => {
  it("says one row's before and after", () => {
    expect(
      netMoves([
        row(2, [{ name: "priority", before: "high", after: "urgent" }]),
      ])
    ).toEqual([
      {
        name: "priority",
        before: "high",
        after: "urgent",
        beforeUnknown: false,
      },
    ])
  })

  it("folds a run into its net move, newest first as the feed reads", () => {
    const moves = netMoves([
      row(4, [{ name: "priority", before: "medium", after: "urgent" }]),
      row(3, [{ name: "url", before: "https://a.example.com" }]),
      row(2, [
        { name: "priority", before: "high", after: "medium" },
        { name: "url", after: "https://a.example.com" },
      ]),
    ])
    // The url came and went: it moved, and it ended where it began.
    expect(moves).toEqual([
      {
        name: "priority",
        before: "high",
        after: "urgent",
        beforeUnknown: false,
      },
      { name: "url", beforeUnknown: false, changedBack: true },
    ])
  })

  it("says a secret the change replaced, though both sides are sealed", () => {
    const sealed = { before: "<redacted>", after: "<redacted>" }
    expect(netMoves([row(2, [{ name: "apiKey", ...sealed }])])).toEqual([
      { name: "apiKey", ...sealed, beforeUnknown: false, replaced: true },
    ])
    // Rotated twice in a run: still replaced, never "changed back".
    expect(
      netMoves([
        row(3, [{ name: "apiKey", ...sealed }]),
        row(2, [{ name: "apiKey", ...sealed }]),
      ])
    ).toEqual([
      { name: "apiKey", ...sealed, beforeUnknown: false, replaced: true },
    ])
  })

  it("says a rename that kept its value as the rename, never replaced", () => {
    expect(
      netMoves([
        row(2, [
          {
            name: "dimensions",
            renamedFrom: "size",
            before: "bigger",
            after: "bigger",
          },
        ]),
      ])
    ).toEqual([
      {
        name: "dimensions",
        renamedFrom: "size",
        before: "bigger",
        after: "bigger",
        beforeUnknown: false,
      },
    ])
    const sealed = { before: "<redacted>", after: "<redacted>" }
    expect(
      netMoves([row(2, [{ name: "apiKey", renamedFrom: "token", ...sealed }])])
    ).toEqual([
      {
        name: "apiKey",
        renamedFrom: "token",
        ...sealed,
        beforeUnknown: false,
      },
    ])
  })

  it("carries a run's moves under the old name across a rename", () => {
    expect(
      netMoves([
        row(3, [
          {
            name: "dimensions",
            renamedFrom: "size",
            before: "bigger",
            after: "bigger",
          },
        ]),
        row(2, [{ name: "size", before: "small", after: "bigger" }]),
      ])
    ).toEqual([
      {
        name: "dimensions",
        renamedFrom: "size",
        before: "small",
        after: "bigger",
        beforeUnknown: false,
      },
    ])
  })

  it("says a value moved and moved back across a run", () => {
    expect(
      netMoves([
        row(3, [{ name: "priority", before: "high", after: "low" }]),
        row(2, [{ name: "priority", before: "low", after: "high" }]),
      ])
    ).toEqual([
      {
        name: "priority",
        before: "low",
        after: "low",
        beforeUnknown: false,
        changedBack: true,
      },
    ])
  })

  it("falls back to names when rows that name properties tell no values", () => {
    expect(netMoves([row(2, [], ["labels"])])).toBeUndefined()
  })

  it("keeps an unknown before as unknown", () => {
    expect(
      netMoves([row(2, [{ name: "name", after: "B", beforeUnknown: true }])])
    ).toEqual([{ name: "name", after: "B", beforeUnknown: true }])
  })

  it("says a list's change as what it gained and lost, copies counted", () => {
    const [move] =
      netMoves([
        row(2, [
          {
            name: "emails",
            before: ["a@example.com", "b@example.com", "b@example.com"],
            after: ["b@example.com", "c@example.com"],
          },
        ]),
      ]) ?? []
    expect(move.added).toEqual(["c@example.com"])
    expect(move.removed).toEqual(["a@example.com", "b@example.com"])
  })

  it("says a list that only changed order as the list it became", () => {
    const [move] =
      netMoves([
        row(2, [{ name: "tags", before: ["a", "b"], after: ["b", "a"] }]),
      ]) ?? []
    expect(move.added).toBeUndefined()
    expect(move.after).toEqual(["b", "a"])
  })

  it("adds a first list whole", () => {
    const [move] =
      netMoves([row(2, [{ name: "emails", after: ["a@example.com"] }])]) ?? []
    expect(move.added).toEqual(["a@example.com"])
    expect(move.removed).toEqual([])
  })

  it("falls back to names when any row names properties without values", () => {
    expect(
      netMoves([
        row(3, [{ name: "priority", before: "high", after: "urgent" }]),
        row(2, undefined, ["name"]),
      ])
    ).toBeUndefined()
    // A row that names nothing (a delete) costs the run nothing.
    expect(netMoves([row(3, [{ name: "name", after: "A" }]), row(2)])).toEqual([
      { name: "name", after: "A", beforeUnknown: false },
    ])
  })
})

describe("shortText", () => {
  it("cuts long text to one line and keeps the whole for the hover", () => {
    const long = "word ".repeat(40)
    const { text, full } = shortText(long)
    expect(text.length).toBe(VALUE_CHARS)
    expect(text.endsWith("…")).toBe(true)
    expect(full).toBe(long)
    expect(shortText("two\nlines").text).toBe("two lines")
    expect(shortText({ a: 1 }).text).toBe('{"a":1}')
  })
})

describe("readerMoves", () => {
  const spec = (name: string, extra: Partial<PropSpec> = {}): PropSpec => ({
    name,
    label: name,
    kind: "string",
    required: false,
    repeated: false,
    keyed: false,
    managed: false,
    ...extra,
  })
  const specs = new Map<string, PropSpec>([
    ["originDigest", spec("originDigest", { managed: true })],
    ["gmailHistoryId", spec("gmailHistoryId", { writer: "connector" })],
    ["name", spec("name", { writer: "owner" })],
  ])
  const move = (m: Partial<ValueMove> & { name: string }): ValueMove => ({
    beforeUnknown: false,
    ...m,
  })

  it("names what the host writes", () => {
    expect(hostWritten(specs.get("originDigest"))).toBe(true)
    expect(hostWritten(specs.get("gmailHistoryId"))).toBe(true)
    expect(hostWritten(specs.get("name"))).toBe(false)
    expect(hostWritten(undefined)).toBe(false)
  })

  it("keeps a person's moves and drops the host's and the empty ones", () => {
    const out = readerMoves(
      [
        move({ name: "originDigest", before: "a", after: "b" }),
        move({ name: "gmailHistoryId", after: "12" }),
        move({ name: "name", before: "Ada", after: "Grace" }),
        move({ name: "collapsed", after: [] }),
        move({
          name: "tags",
          before: ["a"],
          after: ["a"],
          added: [],
          removed: [],
        }),
        move({ name: "notes", beforeUnknown: true }),
      ],
      specs
    )
    expect(out.map((m) => m.name)).toEqual(["name", "notes"])
  })
})
