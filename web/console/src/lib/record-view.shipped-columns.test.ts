/** The columns of the tables the shipped record views list. A
 * property the listed kind declares as an object draws as a nested sheet, a
 * label and a value per field, which makes every row of the table as tall as
 * the object has fields. A shipped table names such a property only where
 * the console reads it by the core `result` trait's meaning and draws it on
 * one line (the flags and the reference ranges). */

import { describe, expect, it } from "vitest"
import { parseAllDocuments } from "yaml"

import type { SubstrateRecord } from "@/lib/api/types"
import { RECORD_VIEW_KIND, recordViewOf } from "@/lib/record-view"

const viewFiles = import.meta.glob<string>(
  ["../../../../samples/*/views.yaml", "../../../../kinds/*/*/views.yaml"],
  { query: "?raw", import: "default", eager: true }
)

const shippedFiles = import.meta.glob<string>(
  ["../../../../samples/*/*.yaml", "../../../../kinds/*/*/*.yaml"],
  { query: "?raw", import: "default", eager: true }
)

interface Doc {
  kind?: string
  metadata?: { id?: string }
  data?: { properties?: Record<string, unknown> }
}

function docs(raw: string): Doc[] {
  return parseAllDocuments(raw).map((d) => d.toJS() as Doc)
}

const KIND_DECLARATION = "substrate.reamde.dev/core/kind"
const KIND_PREFIX = `${KIND_DECLARATION}/`

// The object columns the table block draws on one line by the core result
// trait's meaning (components/record-view/table-block.tsx).
const ONE_LINE_OBJECTS = new Set(["interpretation", "referenceRanges"])

// Every property a shipped kind declares as an object, by the kind's
// reference.
const OBJECT_PROPERTIES = new Map<string, Set<string>>(
  Object.values(shippedFiles)
    .flatMap(docs)
    .filter((d) => d?.kind === KIND_DECLARATION && d.metadata?.id)
    .map((d) => {
      const props = (d.data?.properties ?? {}) as Record<
        string,
        { type?: string } | undefined
      >
      const objects = Object.entries(props)
        .filter(([, spec]) => spec?.type === "object")
        .map(([name]) => name)
      return [d.metadata!.id!, new Set(objects)] as const
    })
)

const VIEWS: SubstrateRecord[] = Object.values(viewFiles)
  .flatMap(docs)
  .filter((d) => d?.kind === RECORD_VIEW_KIND)
  .map((d) => ({
    id: d.metadata?.id ?? "",
    kind: RECORD_VIEW_KIND,
    properties: d.data?.properties ?? {},
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  }))

// Each shipped table with a shipped kind, as the view id, the kind and the
// columns it names.
const TABLES = VIEWS.flatMap((record) =>
  recordViewOf(record).sections.flatMap((s) => {
    if (!("block" in s) || s.block.type !== "table") return []
    const ref = s.block.kind.startsWith(KIND_PREFIX)
      ? s.block.kind.slice(KIND_PREFIX.length)
      : s.block.kind
    if (!OBJECT_PROPERTIES.has(ref)) return []
    return [[record.id, ref, s.block.columns] as const]
  })
)

describe("the shipped tables' columns", () => {
  it("are read", () => {
    expect(TABLES.length).toBeGreaterThan(0)
    // The lab result kind declares specimen as an object, so the rule below
    // has something to hold.
    expect(
      OBJECT_PROPERTIES.get("substrate.reamde.dev/labs/labresult")?.has(
        "specimen"
      )
    ).toBe(true)
  })

  it.each(TABLES)(
    "%s lists %s with no multi-line object column",
    (_, kind, columns) => {
      const objects = OBJECT_PROPERTIES.get(kind) ?? new Set<string>()
      expect(
        columns.filter((c) => objects.has(c) && !ONE_LINE_OBJECTS.has(c))
      ).toEqual([])
    }
  )
})
