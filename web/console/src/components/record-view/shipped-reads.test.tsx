// @vitest-environment jsdom
/** The aggregate reads every shipped record view issues on one record page
 * (decision record 0175). The engine serves two aggregate reads at once per
 * process, so a view edit or a batching change that multiplies a page's
 * reads fails here, not on a repository: each view is mounted on a record
 * with a window, every read answered empty, and the distinct `aggregate=`
 * requests counted. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { parseAllDocuments } from "yaml"

import type { SubstrateRecord } from "@/lib/api/types"
import { KINDS } from "@/lib/observations.test-fixtures"
import {
  RECORD_VIEW_KIND,
  recordViewOf,
  type RecordView,
} from "@/lib/record-view"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

import { RecordViews } from "./record-views"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const viewFiles = import.meta.glob<string>(
  "../../../../../samples/*/views.yaml",
  {
    query: "?raw",
    import: "default",
    eager: true,
  }
)

const sampleFiles = import.meta.glob<string>(
  "../../../../../samples/*/*.yaml",
  {
    query: "?raw",
    import: "default",
    eager: true,
  }
)

interface Doc {
  kind?: string
  metadata?: { id?: string }
  data?: { properties?: Record<string, unknown> }
}

function docs(raw: string): Doc[] {
  return parseAllDocuments(raw).map((d) => d.toJS() as Doc)
}

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-10-11T07:00:00Z",
    updatedAt: "2026-10-11T07:00:00Z",
  }
}

// Every metric record a sample ships, answered by its id.
const METRICS = Object.values(sampleFiles)
  .flatMap(docs)
  .filter((d) => d?.kind?.endsWith("/metric") && d.metadata?.id)
  .map((d) => record(d.kind!, d.metadata!.id!, d.data?.properties ?? {}))

const KIND_PREFIX = "substrate.reamde.dev/core/kind/"

// Every shipped view, with the kind it applies to.
const VIEWS: { view: RecordView; kind: string }[] = Object.values(viewFiles)
  .flatMap(docs)
  .filter((d) => d?.kind === RECORD_VIEW_KIND && d.metadata?.id)
  .map((d) => ({
    view: recordViewOf(
      record(RECORD_VIEW_KIND, d.metadata!.id!, d.data?.properties ?? {})
    ),
    kind: String(d.data?.properties?.appliesTo ?? "").replace(KIND_PREFIX, ""),
  }))

/** A record of the kind `view` applies to, holding a closed eight-hour
 * window, a subject and a source wherever the view binds them. */
function recordFor(view: RecordView, kind: string): SubstrateRecord {
  const w = view.window
  const p: Record<string, unknown> = {}
  if (w?.start) p[w.start] = "2026-10-10T22:00:00Z"
  if (w?.end) p[w.end] = "2026-10-11T06:00:00Z"
  if (w?.about)
    p[w.about] = { ref: "samples.substrate.reamde.dev/people/person/me" }
  if (w?.source)
    p[w.source] = { ref: "providers.substrate.reamde.dev/whoop/source/strap" }
  return record(kind, "r1", p)
}

/** Mounts `view` on its record and answers every read empty; the distinct
 * aggregate reads it issued, once no read is left in flight. */
async function aggregateReads(view: RecordView, kind: string): Promise<number> {
  const aggregates = new Set<string>()
  let inFlight = 0
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      inFlight++
      try {
        const url = new URL(String(input), "http://test")
        const ok = (body: unknown) =>
          new Response(JSON.stringify(body), { status: 200 })
        if (url.pathname !== "/api/v1/records") {
          const metric = METRICS.find((m) =>
            url.pathname.endsWith(`/${encodeURIComponent(m.id)}`)
          )
          return metric
            ? ok(metric)
            : new Response(
                JSON.stringify({ error: { code: "not_found", message: "no" } }),
                { status: 404 }
              )
        }
        const aggregate = url.searchParams.get("aggregate")
        if (aggregate) {
          aggregates.add(aggregate)
          return ok({
            unit: "",
            from: "raw",
            exact: false,
            rows: [],
            head: { changelog: 1, generation: "g" },
          })
        }
        return ok({ records: [], head: 1, generation: "g" })
      } finally {
        inFlight--
      }
    })
  )
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RecordViews
        views={[view]}
        record={recordFor(view, kind)}
        kinds={KINDS}
      />
    </QueryClientProvider>
  )
  await waitFor(() => {
    expect(client.isFetching()).toBe(0)
    expect(inFlight).toBe(0)
  })
  // One more turn: a read that waited on the metric records issues now.
  await new Promise((r) => setTimeout(r, 0))
  await waitFor(() => expect(client.isFetching()).toBe(0))
  return aggregates.size
}

/** The aggregate reads each shipped view issues, by view id. A change to
 * one of these numbers is a change to what a page costs the engine: raise
 * it only with the view's reads batched as far as `statsReads` and
 * `chartReads` can. */
const READS: Record<string, number> = {
  // The chart's three metrics in one read, and the stage totals: the stats
  // and the breakdown read the sleep's own properties.
  "samples.substrate.reamde.dev/health/sleep": 2,
  // Three charts, one read each.
  "samples.substrate.reamde.dev/health/workout": 3,
  // stats: recovery, strain, sleep performance, calories and average heart
  // rate in one read, max heart rate in a second (a read names a metric
  // once), each repeated over the baseline: 4. The vitals: 1, and 1 over
  // the baseline. Stage totals: 1. The trend: 1.
  "samples.substrate.reamde.dev/healthwhoop/cycle": 8,
  // stats: 1, and 1 over the baseline. The trend: 1.
  "samples.substrate.reamde.dev/healthwhoop/sleep": 3,
  "samples.substrate.reamde.dev/health/labreport": 0,
  "samples.substrate.reamde.dev/health/labresult": 0,
  "samples.substrate.reamde.dev/health/metric": 0,
}

describe("the shipped record views", () => {
  it("are all counted here", () => {
    expect(VIEWS.length).toBeGreaterThan(0)
    expect(VIEWS.map(({ view }) => view.id).sort()).toEqual(
      Object.keys(READS).sort()
    )
  })

  for (const { view, kind } of VIEWS) {
    it(`${view.id} issues its counted aggregate reads`, async () => {
      const got = await aggregateReads(view, kind)
      expect(got).toBe(READS[view.id])
      // No shipped page may need more than four rounds of the engine's two
      // aggregate slots.
      expect(got).toBeLessThanOrEqual(8)
    })
  }
})
