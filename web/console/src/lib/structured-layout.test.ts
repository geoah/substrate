/** The structured view's inferences: which field names an item, the order
 * fields read in, which two fields read as a range, and when a list is a
 * table. Each is decided from the declaration and the values alone, so the
 * same record reads the same way on every surface. */

import { describe, expect, it } from "vitest"

import { propSpecs, type PropSpec } from "@/lib/record-schema"
import {
  cellName,
  fieldsToAdd,
  held,
  identityField,
  itemSummary,
  listLayout,
  looseKeys,
  objectCells,
  orderedFields,
  rangePairs,
  rangeText,
  TABLE_COLUMNS,
} from "@/lib/structured-layout"
import type { KindInfo } from "@/lib/api/types"

/** A kind's properties as the console reads them. */
function specs(properties: Record<string, unknown>): Record<string, PropSpec> {
  const kind = {
    identity: "ada.example.com/gym/routine",
    name: "routine",
    authority: "ada.example.com",
    package: "gym",
    version: 1,
    source: "installed",
    description: "",
    definition: { properties },
  } as KindInfo
  return Object.fromEntries(propSpecs(kind).map((s) => [s.name, s]))
}

const EXERCISE = "ada.example.com/gym/exercise"

const routine = specs({
  groups: {
    type: "object",
    repeated: true,
    fields: {
      key: { type: "string", required: true },
      groupKind: { type: "enum", required: true, values: ["superset"] },
      pairing: { type: "enum", values: ["antagonist", "unrelated"] },
      roundRest: { type: "duration" },
      rounds: { type: "int", min: 1 },
    },
  },
  slots: {
    type: "object",
    repeated: true,
    fields: {
      key: { type: "string", required: true },
      exercise: { type: "reference", kind: EXERCISE, required: true },
      group: { type: "string" },
      notes: { type: "text" },
      transition: { type: "duration" },
      sets: {
        type: "object",
        repeated: true,
        fields: {
          key: { type: "string", required: true },
          role: { type: "enum", values: ["warmup", "working"] },
          structure: { type: "enum", values: ["straight", "drop"] },
          repsMin: { type: "int" },
          repsMax: { type: "int" },
          rirMin: { type: "decimal" },
          rirMax: { type: "decimal" },
          rest: { type: "duration" },
          tempo: { type: "string" },
          load: { type: "decimal", writer: ["function", "agent"] },
          stamp: { type: "datetime", managed: true },
        },
      },
    },
  },
})

const sets = routine.slots.fields!.find((f) => f.name === "sets")!
const working = [
  {
    key: "s1",
    role: "working",
    structure: "straight",
    repsMin: 12,
    repsMax: 15,
    rirMin: "4",
    rirMax: "5",
  },
  { key: "s2", role: "working", repsMin: 12, repsMax: 15 },
]

const names = (fields: readonly PropSpec[]) => fields.map((f) => f.name)

describe("held", () => {
  it("counts zero and false as said, and blanks and empty containers as not", () => {
    expect([0, false, "x", [1], { a: 1 }].map(held)).toEqual([
      true,
      true,
      true,
      true,
      true,
    ])
    expect([undefined, null, "", [], {}].map(held)).toEqual([
      false,
      false,
      false,
      false,
      false,
    ])
  })
})

describe("identityField", () => {
  it("names an item by a name some item holds", () => {
    const named = specs({
      codes: {
        type: "object",
        repeated: true,
        fields: {
          code: { type: "string", required: true },
          display: { type: "string" },
        },
      },
    }).codes
    expect(identityField(named, [{ code: "a", display: "Alpha" }])?.name).toBe(
      "display"
    )
    // A name nobody wrote names nothing: the handle does.
    expect(identityField(named, [{ code: "a" }])?.name).toBe("code")
  })

  it("names an item by the one record it is about before its handle", () => {
    expect(identityField(routine.slots, [])?.name).toBe("exercise")
  })

  it("names an item by its handle, else its sole required short field", () => {
    expect(identityField(routine.groups, [])?.name).toBe("key")
    const muscles = specs({
      muscles: {
        type: "object",
        repeated: true,
        fields: {
          muscle: { type: "enum", required: true, values: ["quads"] },
          share: { type: "decimal" },
        },
      },
    }).muscles
    expect(identityField(muscles, [])?.name).toBe("muscle")
    const pairs = specs({
      pairs: {
        type: "object",
        repeated: true,
        fields: {
          a: { type: "string", required: true },
          b: { type: "string", required: true },
        },
      },
    }).pairs
    expect(identityField(pairs, [])).toBeUndefined()
  })
})

describe("orderedFields", () => {
  it("reads the name first, then what an item must hold, then by band, a range low end first", () => {
    const item = { ...sets, repeated: false }
    expect(names(orderedFields(item, identityField(sets, working)))).toEqual([
      "key",
      "role",
      "structure",
      "stamp",
      "load",
      "repsMin",
      "repsMax",
      "rest",
      "rirMin",
      "rirMax",
      "tempo",
    ])
  })

  it("puts prose after the plain values and structures last", () => {
    const item = { ...routine.slots, repeated: false }
    expect(names(orderedFields(item, identityField(routine.slots)))).toEqual([
      "exercise",
      "key",
      "group",
      "transition",
      "notes",
      "sets",
    ])
  })
})

describe("rangePairs", () => {
  it("pairs a field ending Min with one ending Max of the same datatype", () => {
    const pairs = rangePairs(sets.fields!)
    expect(pairs.map((p) => [p.stem, p.label, p.min.name, p.max.name])).toEqual(
      [
        ["reps", "Reps", "repsMin", "repsMax"],
        ["rir", "Rir", "rirMin", "rirMax"],
      ]
    )
  })

  it("leaves two ends of different datatypes apart", () => {
    const mixed = specs({
      o: {
        type: "object",
        fields: { sizeMin: { type: "int" }, sizeMax: { type: "string" } },
      },
    }).o
    expect(rangePairs(mixed.fields!)).toEqual([])
  })

  it("labels a range by its stem where the authored labels share no words", () => {
    const labelled = specs({
      o: {
        type: "object",
        fields: {
          heartMin: { type: "int", displayName: "Lowest pulse" },
          heartMax: { type: "int", displayName: "Highest pulse" },
        },
      },
    }).o
    expect(rangePairs(labelled.fields!)[0].label).toBe("Heart")
  })
})

describe("listLayout", () => {
  it("reads flat items as a table of the fields they hold, a range as one column", () => {
    const layout = listLayout(sets, working)
    expect(layout.mode).toBe("table")
    expect(layout.cells.map(cellName)).toEqual([
      "key",
      "role",
      "structure",
      "reps~range",
      "rir~range",
    ])
    expect(layout.cells.map((c) => c.label)).toEqual([
      "Key",
      "Role",
      "Structure",
      "Reps",
      "Rir",
    ])
  })

  it("stacks items that hold prose or a list of their own", () => {
    const slots = [
      {
        key: "glute-bridge",
        exercise: { ref: `${EXERCISE}/glute-bridge` },
        sets: working,
      },
    ]
    expect(listLayout(routine.slots, slots).mode).toBe("stack")
  })

  it("stacks items whose fields would make too many columns", () => {
    const wide = specs({
      rows: {
        type: "object",
        repeated: true,
        fields: Object.fromEntries(
          Array.from({ length: TABLE_COLUMNS + 1 }, (_, i) => [
            `f${i}`,
            { type: "int" },
          ])
        ),
      },
    }).rows
    const item = Object.fromEntries(
      Array.from({ length: TABLE_COLUMNS + 1 }, (_, i) => [`f${i}`, i])
    )
    expect(listLayout(wide, [item]).mode).toBe("stack")
    const fewer = Object.fromEntries(Object.entries(item).slice(1))
    expect(listLayout(wide, [fewer]).mode).toBe("table")
  })

  it("shows a field being set as a column without changing the layout", () => {
    const layout = listLayout(sets, working, ["tempo"])
    expect(layout.mode).toBe("table")
    expect(layout.cells.map(cellName)).toContain("tempo")
  })
})

describe("objectCells", () => {
  it("shows what an object must hold and what it holds", () => {
    const item = { ...sets, repeated: false }
    expect(objectCells(item, { key: "w1", repsMin: 6 }).map(cellName)).toEqual([
      "key",
      "repsMin",
    ])
    expect(
      objectCells(item, { key: "w1", repsMin: 6, repsMax: 6 }).map(cellName)
    ).toEqual(["key", "reps~range"])
  })
})

describe("fieldsToAdd", () => {
  it("offers what an item does not hold and the owner may write, never a structure", () => {
    const item = orderedFields({ ...sets, repeated: false })
    expect(names(fieldsToAdd(item, working[0]))).toEqual(["rest", "tempo"])
    const slot = orderedFields({ ...routine.slots, repeated: false })
    expect(names(fieldsToAdd(slot, { key: "a" }))).not.toContain("sets")
  })
})

describe("itemSummary", () => {
  it("says two of a folded item's values and how much each list holds", () => {
    const slots = [
      {
        key: "glute-bridge",
        exercise: { ref: `${EXERCISE}/glute-bridge` },
        group: "a",
        transition: "PT25S",
        sets: working,
      },
    ]
    const layout = listLayout(routine.slots, slots)
    expect(
      itemSummary(layout, slots[0]).map((p) => [p.label, p.count])
    ).toEqual([
      ["Key", undefined],
      ["Group", undefined],
      ["Sets", 2],
    ])
  })
})

describe("rangeText", () => {
  const text = (v: unknown) => String(v)
  it("reads one value where the ends agree and both where they differ", () => {
    expect(rangeText(6, 6, text)).toBe("6")
    expect(rangeText(12, 15, text)).toBe("12–15")
  })

  it("reads the end that is set where only one is, and nothing for neither", () => {
    expect(rangeText(12, undefined, text)).toBe("≥ 12")
    expect(rangeText(undefined, 15, text)).toBe("≤ 15")
    expect(rangeText(undefined, null, text)).toBe("")
  })

  it("keeps a zero end", () => {
    expect(rangeText(0, 3, text)).toBe("0–3")
  })
})

describe("undeclared keys", () => {
  it("names the keys items hold that no field declares, once each", () => {
    expect(
      looseKeys(sets.fields!, [
        { key: "s1", odd: 1, extra: "x" },
        { key: "s2", odd: 2, blank: "" },
      ])
    ).toEqual(["extra", "odd"])
  })

  it("shows them after the declared fields, read only, a structure among them stacking the list", () => {
    const flat = listLayout(sets, [{ key: "s1", odd: "kept" }])
    expect(flat.mode).toBe("table")
    expect(flat.cells.map(cellName)).toEqual(["key", "odd~loose"])
    const deep = listLayout(sets, [{ key: "s1", odd: { a: 1 } }])
    expect(deep.mode).toBe("stack")
    const one = objectCells({ ...sets, repeated: false }, { key: "w1", z: 0 })
    expect(one.map(cellName)).toEqual(["key", "z~loose"])
  })
})
