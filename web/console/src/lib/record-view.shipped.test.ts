/** The record views the tree ships (the `views.yaml` of a sample, a
 * first-party catalogue or a provider), read the way the console reads every
 * view: none holds a section the console would draw as a notice, every
 * metric a block names is the full path of one the tree ships, every kind a
 * table or a lanes row lists is one the tree declares, and a view binds a
 * window wherever one of its blocks reads one. The engine holds the views to
 * their declared shape on write; the rules only the console checks are held
 * here. */

import { describe, expect, it } from "vitest"
import { parseAllDocuments } from "yaml"

import type { SubstrateRecord } from "@/lib/api/types"
import type { Block } from "@/lib/record-view"
import {
  RECORD_VIEW_KIND,
  readsWindow,
  recordViewOf,
  viewNeedsWindow,
} from "@/lib/record-view"

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

// Every metric record the tree ships, by its full record path.
const METRIC_PATHS = new Set(
  Object.values(shippedFiles)
    .flatMap(docs)
    .filter((d) => d?.kind?.endsWith("/metric") && d.metadata?.id)
    .map((d) => `${d.kind}/${d.metadata!.id!}`)
)

const KIND_DECLARATION = "substrate.reamde.dev/core/kind"
const KIND_PREFIX = `${KIND_DECLARATION}/`

// Every kind the tree declares, by its reference.
const SHIPPED_KINDS = new Set(
  Object.values(shippedFiles)
    .flatMap(docs)
    .filter((d) => d?.kind === KIND_DECLARATION && d.metadata?.id)
    .map((d) => d.metadata!.id!)
)

// Every metric a block reads, its bands included.
function metricsOf(block: Block): string[] {
  const banded =
    "bands" in block ? block.bands.flatMap((b) => b.metric ?? []) : []
  switch (block.type) {
    case "chart":
      return [...block.metrics, ...banded]
    case "spans":
      return [block.metric]
    case "stats":
    case "trend":
      return [...block.items.flatMap((i) => i.metric ?? []), ...banded]
    case "lanes":
      return block.rows.flatMap((r) => r.metric ?? [])
    default:
      return []
  }
}

// Every kind a block lists.
function kindsOf(block: Block): string[] {
  switch (block.type) {
    case "table":
      return [block.kind]
    case "lanes":
      return block.rows.flatMap((r) => r.kind ?? [])
    default:
      return []
  }
}

// The parsed view holds a listed kind's reference; the declaration's record
// path is accepted too.
function shippedKind(path: string): boolean {
  const ref = path.startsWith(KIND_PREFIX)
    ? path.slice(KIND_PREFIX.length)
    : path
  return SHIPPED_KINDS.has(ref)
}

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
    expect(SHIPPED_KINDS.size).toBeGreaterThan(0)
  })

  it.each(VIEWS.map((v) => [v.id, v] as const))(
    "%s draws every section and names shipped metrics and kinds",
    (_, record) => {
      const view = recordViewOf(record)
      expect(view.sections.length).toBeGreaterThan(0)
      const blocks = view.sections.flatMap((s) =>
        "block" in s ? [s.block] : []
      )
      // A window is required only where a block reads one.
      expect(!blocks.some((b) => readsWindow(b)) || viewNeedsWindow(view)).toBe(
        true
      )
      const problems = view.sections.flatMap((s) =>
        "problem" in s ? [s.problem] : []
      )
      expect(problems).toEqual([])
      const named = blocks.flatMap(metricsOf)
      expect(named.filter((m) => !METRIC_PATHS.has(m))).toEqual([])
      const listed = blocks.flatMap(kindsOf)
      expect(listed.filter((k) => !shippedKind(k))).toEqual([])
    }
  )
})
