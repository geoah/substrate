/** The record views the samples ship (each sample's `views.yaml`), read the way
 * the console reads every view: none holds a section the console would
 * draw as a notice, and every metric a chart or a spans block names is the
 * full path of one a sample ships. The engine holds the views to their declared shape on
 * write; the rules only the console checks are held here. */

import { describe, expect, it } from "vitest"
import { parseAllDocuments } from "yaml"

import type { SubstrateRecord } from "@/lib/api/types"
import { RECORD_VIEW_KIND, recordViewOf } from "@/lib/record-view"

const viewFiles = import.meta.glob<string>("../../../../samples/*/views.yaml", {
  query: "?raw",
  import: "default",
  eager: true,
})

const sampleFiles = import.meta.glob<string>("../../../../samples/*/*.yaml", {
  query: "?raw",
  import: "default",
  eager: true,
})

interface Doc {
  kind?: string
  metadata?: { id?: string }
  data?: { properties?: Record<string, unknown> }
}

function docs(raw: string): Doc[] {
  return parseAllDocuments(raw).map((d) => d.toJS() as Doc)
}

// Every metric record a sample ships, by its full record path.
const METRIC_PATHS = new Set(
  Object.values(sampleFiles)
    .flatMap(docs)
    .filter((d) => d?.kind?.endsWith("/metric") && d.metadata?.id)
    .map((d) => `${d.kind}/${d.metadata!.id!}`)
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

describe("the shipped record views", () => {
  it("are read", () => {
    expect(VIEWS.length).toBeGreaterThan(0)
    expect(METRIC_PATHS.size).toBeGreaterThan(0)
  })

  it.each(VIEWS.map((v) => [v.id, v] as const))(
    "%s draws every section and names shipped metrics",
    (_, record) => {
      const view = recordViewOf(record)
      expect(view.sections.length).toBeGreaterThan(0)
      expect(view.window).toBeDefined()
      const problems = view.sections.flatMap((s) =>
        "problem" in s ? [s.problem] : []
      )
      expect(problems).toEqual([])
      const named = view.sections.flatMap((s) =>
        "block" in s
          ? s.block.type === "chart"
            ? s.block.metrics
            : s.block.type === "spans"
              ? [s.block.metric]
              : []
          : []
      )
      expect(named.filter((m) => !METRIC_PATHS.has(m))).toEqual([])
    }
  )
})
