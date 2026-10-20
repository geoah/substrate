// @vitest-environment jsdom
/** A record view's breakdown block: bars from the record's own properties,
 * a keyed map's keys in the view's order or listed properties, each with
 * its share of a total, a value that is no amount left out and named, and
 * a line saying none is set where nothing remains. It reads nothing. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { AUTHORITY, KINDS } from "@/lib/observations.test-fixtures"
import { RECORD_VIEW_KIND, viewsOf } from "@/lib/record-view"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

import { RecordViews } from "./record-views"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const NIGHT: KindInfo = {
  identity: `${AUTHORITY}/fitness/night`,
  name: "night",
  authority: AUTHORITY,
  package: "fitness",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    authority: AUTHORITY,
    package: "fitness",
    names: { singular: "night" },
    traits: [],
    properties: {},
  },
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

function mount(sections: unknown[], properties: Record<string, unknown>) {
  const fetch = vi.fn(
    async () =>
      new Response(JSON.stringify({ records: [], head: 1, generation: "g" }))
  )
  vi.stubGlobal("fetch", fetch)
  const view = record(RECORD_VIEW_KIND, "fitness/night", {
    appliesTo: { ref: `substrate.reamde.dev/core/kind/${NIGHT.identity}` },
    name: "This night",
    sections,
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RecordViews
        views={viewsOf([view])}
        record={record(NIGHT.identity, "n-1", properties)}
        kind={NIGHT}
        kinds={[...KINDS, NIGHT]}
      />
    </QueryClientProvider>
  )
  return { fetch }
}

const bars = (label: string) => {
  const dl = screen.getByRole("group", { name: label }).querySelector("dl")!
  return [...dl.querySelectorAll("dt")].map(
    (dt) =>
      `${dt.textContent}: ${dt.nextElementSibling!.nextElementSibling!.textContent}`
  )
}

it("draws listed properties as bars with their share of a total, and names one it leaves out", () => {
  const { fetch } = mount(
    [
      {
        breakdown: {
          label: "Stages",
          items: [
            { property: "awake", label: "Awake" },
            { property: "light", label: "Light" },
            { property: "deep", label: "Deep" },
            { property: "rem", label: "REM" },
            { property: "note" },
          ],
          total: "timeInBed",
        },
      },
    ],
    {
      awake: "PT30M",
      light: "PT4H",
      deep: "PT1H30M",
      rem: "PT2H",
      timeInBed: "PT8H",
      note: "restless",
    }
  )
  expect(bars("Stages")).toEqual([
    "Awake: 30m, 6%",
    "Light: 4h, 50%",
    "Deep: 1h 30m, 19%",
    "REM: 2h, 25%",
  ])
  const group = screen.getByRole("group", { name: "Stages" })
  const colours = [
    ...group.querySelectorAll<HTMLElement>("dl div div div"),
  ].map((d) => d.style.background)
  expect(colours).toEqual([
    "var(--series-1)",
    "var(--series-2)",
    "var(--series-3)",
    "var(--series-4)",
  ])
  expect(group.textContent).toContain(
    "Left out, as no number, duration or quantity: note."
  )
  // Nothing past the record is read.
  expect(fetch).not.toHaveBeenCalled()
})

it("draws a keyed map in the view's order of keys, and says when none is set", () => {
  mount(
    [
      {
        breakdown: {
          label: "Zones",
          property: "zones",
          keys: [
            { key: "z2", label: "Zone 2" },
            { key: "z1", label: "Zone 1" },
          ],
        },
      },
      { breakdown: { label: "Empty", items: [{ property: "absent" }] } },
    ],
    { zones: { z1: "PT10M", z2: "PT30M" } }
  )
  expect(bars("Zones")).toEqual(["Zone 2: 30m, 75%", "Zone 1: 10m, 25%"])
  expect(screen.getByRole("group", { name: "Empty" }).textContent).toContain(
    "None of these is set on this night."
  )
})
