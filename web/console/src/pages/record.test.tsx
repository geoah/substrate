// @vitest-environment jsdom
/** The record page with and without record views: a view draws above the
 * property sheet, the properties it folds leave the sheet under one line
 * naming the view (or "the view above" where the view has no name), and a
 * kind with no view gets the generic sheet, nothing folded. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, expect, it, vi } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { KindInfo, RecordFilter, SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { RECORD_VIEW_KIND } from "@/lib/record-view"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: ReactNode }) => <a>{children}</a>,
  useNavigate: () => () => undefined,
}))
vi.mock("@/router", () => ({ recordRoute: { useParams: () => ({}) } }))
vi.mock("@/hooks/use-live-invalidation", () => ({
  useLiveInvalidation: () => undefined,
}))

import { RecordDocument } from "./record"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const AUTHORITY = "ada.example.com"

const SESSION: KindInfo = {
  identity: `${AUTHORITY}/fitness/session`,
  name: "session",
  authority: AUTHORITY,
  package: "fitness",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    authority: AUTHORITY,
    package: "fitness",
    names: { singular: "session" },
    properties: {
      sport: { type: "string", description: "the sport" },
      coach: { type: "string", description: "who coached it" },
    },
  },
}

const RUN: SubstrateRecord = {
  id: "run-1",
  kind: SESSION.identity,
  properties: { sport: "running", coach: "Mia" },
  labels: {},
  version: 1,
  createdAt: "2026-10-11T07:00:00Z",
  updatedAt: "2026-10-11T07:00:00Z",
}

function view(properties: Record<string, unknown>): SubstrateRecord {
  return {
    ...RUN,
    id: "fitness/session",
    kind: RECORD_VIEW_KIND,
    properties: {
      appliesTo: { ref: `substrate.reamde.dev/core/kind/${SESSION.identity}` },
      fold: ["sport"],
      sections: [
        {
          properties: {
            label: "At a glance",
            list: [{ property: "sport", label: "Sport" }],
          },
        },
      ],
      ...properties,
    },
  }
}

function mount(
  views: SubstrateRecord[],
  record: SubstrateRecord = RUN,
  technical = false
) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://test")
      const filter = JSON.parse(
        url.searchParams.get("filter") ?? "{}"
      ) as RecordFilter
      const records = filter.kinds?.includes(RECORD_VIEW_KIND) ? views : []
      return new Response(
        JSON.stringify({ records, changes: [], head: 1, generation: "g" }),
        { status: 200 }
      )
    })
  )
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
          technicalDetails: technical,
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>
        <RecordDocument record={record} kind={SESSION} kinds={[SESSION]} />
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

/** The sheet's rows, by property name. */
function sheetRows(): string[] {
  return [
    ...document.querySelectorAll("[data-slot=property-sheet] [data-property]"),
  ].map((r) => r.getAttribute("data-property")!)
}

it("folds what a view shows out of the sheet, under the view's name", async () => {
  mount([view({ name: "This session" })])
  const section = await screen.findByRole("region", { name: "This session" })
  // The view draws above the sheet.
  const sheet = document.querySelector("[data-slot=property-sheet]")!
  expect(
    section.compareDocumentPosition(sheet) & Node.DOCUMENT_POSITION_FOLLOWING
  ).toBeTruthy()
  await waitFor(() => expect(sheetRows()).toEqual(["coach"]))
  expect(sheet.textContent).toContain("1 shown under This session")
})

it("names an unnamed view's fold as the view above", async () => {
  mount([view({})])
  await screen.findByRole("region", { name: "Session view" })
  await waitFor(() => expect(sheetRows()).toEqual(["coach"]))
  expect(
    document.querySelector("[data-slot=property-sheet]")!.textContent
  ).toContain("1 shown under the view above")
})

it("draws the generic sheet, nothing folded, for a kind with no view", async () => {
  mount([])
  await waitFor(() => expect(sheetRows().sort()).toEqual(["coach", "sport"]))
  expect(screen.queryByRole("region")).toBe(null)
  expect(
    document.querySelector("[data-slot=property-sheet]")!.textContent
  ).not.toContain("shown under")
})

it("shows who wrote an adapter's output and offers no edit, only how to correct it", async () => {
  const sync = "function:ada.example.com:fitnessimport:importer"
  const imported: SubstrateRecord = {
    ...RUN,
    properties: {
      ...RUN.properties,
      origin: { ref: "vendor.example.com/mirror/session/abc" },
    },
    propertyMeta: {
      sport: { manager: sync, tier: "machine" },
      coach: { manager: sync, tier: "machine" },
    },
  }
  mount([], imported)
  const notice = await waitFor(() => {
    const n = document.querySelector("[data-slot=adapter-output]")
    expect(n).not.toBe(null)
    return n!
  })
  expect(notice.textContent).toContain("Written by")
  expect(notice.textContent).toContain("abc")
  expect(notice.textContent).toContain("To correct it, write your own session")
  expect(notice.textContent).toContain("New session")
  // The sheet offers no edit.
  expect(
    document.querySelectorAll("[data-slot=property-sheet] [id$=-edit]")
  ).toHaveLength(0)

  // The same record written by its owner, with no origin, is editable.
  cleanup()
  mount([])
  await waitFor(() =>
    expect(
      document.querySelectorAll("[data-slot=property-sheet] [id$=-edit]").length
    ).toBeGreaterThan(0)
  )
  expect(document.querySelector("[data-slot=adapter-output]")).toBe(null)
})

const IMPORTER = "function:ada.example.com:fitnessimport:importer"

/** RUN as an adapter wrote it, naming its provider record the way the kind
 * does: `origin`, or a packed period's `origins`. */
function adapterRun(provenance: Record<string, unknown>): SubstrateRecord {
  return {
    ...RUN,
    properties: { ...RUN.properties, ...provenance },
    propertyMeta: {
      sport: { manager: IMPORTER, tier: "machine" },
      coach: { manager: IMPORTER, tier: "machine" },
    },
  }
}

// A packed period names its provider records in `origins`; only `origin`
// was read, so a packed adapter output was offered every edit.
it("reads a packed period's origins as an adapter's output", async () => {
  mount(
    [],
    adapterRun({ origins: [{ ref: "vendor.example.com/mirror/day/d1" }] })
  )
  const notice = await waitFor(() => {
    const n = document.querySelector("[data-slot=adapter-output]")
    expect(n).not.toBe(null)
    return n!
  })
  expect(notice.textContent).toContain("d1")
  expect(
    document.querySelectorAll("[data-slot=property-sheet] [id$=-edit]")
  ).toHaveLength(0)
})

// Technical mode's YAML source offered Edit YAML on an adapter's output,
// and the editor behind it saved an ordinary put.
it("offers no YAML edit of an adapter's output in technical mode", async () => {
  mount(
    [],
    adapterRun({ origin: { ref: "vendor.example.com/mirror/session/abc" } }),
    true
  )
  fireEvent.click(await screen.findByRole("button", { name: "View source" }))
  await waitFor(() =>
    expect(document.querySelector("[data-slot=record-source]")).not.toBe(null)
  )
  expect(screen.queryByText("Edit YAML")).toBe(null)
  // The owner's own record keeps it.
  cleanup()
  mount([], RUN, true)
  fireEvent.click(await screen.findByRole("button", { name: "View source" }))
  expect(await screen.findByText("Edit YAML")).toBeTruthy()
})
