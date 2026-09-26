// @vitest-environment jsdom
/** The new-record sheet, held to every kind this repository ships: each one
 * opens, every row it folds away opens too, and every row's editor opens,
 * without a throw. A new record asks first for what it must have, then for
 * the records it points at and the times it is about, and folds the rest. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { parseAllDocuments } from "yaml"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { KindInfo } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { templateYAML } from "@/lib/record-yaml"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
  useNavigate: () => vi.fn(),
}))

import { CreateSheet } from "./create-sheet"

const files = import.meta.glob<string>(
  ["../../../../../kinds/**/*.yaml", "../../../../../samples/**/*.yaml"],
  { query: "?raw", import: "default", eager: true }
)

/** Every shipped kind as the registry lists it: the declaration's `data` is
 * the definition. */
function shipped(): KindInfo[] {
  const out: KindInfo[] = []
  for (const source of Object.values(files)) {
    for (const doc of parseAllDocuments(source)) {
      const d = doc.toJS() as {
        kind?: string
        metadata?: { id?: string }
        data?: Record<string, unknown>
      } | null
      if (d?.kind !== "substrate.reamde.dev/core/kind" || !d.data) continue
      const identity = d.metadata?.id ?? ""
      const [authority = "", pkg = "", name = ""] = identity.split("/")
      out.push({
        identity,
        name,
        authority,
        package: pkg,
        version: 1,
        source: "installed",
        description: String(d.data.description ?? ""),
        definition: d.data,
      })
    }
  }
  return out.sort((a, b) => a.identity.localeCompare(b.identity))
}

const KINDS = shipped()
const TASK = "samples.substrate.reamde.dev/tasks/task"
const PERSON = "samples.substrate.reamde.dev/people/person"

function kindOf(identity: string): KindInfo {
  const kind = KINDS.find((k) => k.identity === identity)
  if (!kind) throw new Error(`no shipped kind ${identity}`)
  return kind
}

/** The properties the sheet shows, in order. */
function shownRows(): string[] {
  return [
    ...document.querySelectorAll("[data-slot=property-sheet] [data-property]"),
  ].map((el) => el.getAttribute("data-property") ?? "")
}

function renderSheet(
  kind: KindInfo,
  kinds: KindInfo[] = KINDS,
  technical = true
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, enabled: false } },
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
        <CreateSheet
          text={templateYAML(kind)}
          kind={kind}
          kinds={kinds}
          onChange={() => {}}
        />
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

/** Opens the fold, where there is one. */
function openFold() {
  const more = screen.queryByRole("button", { name: /\d+ more:/ })
  if (more) fireEvent.click(more)
}

afterEach(cleanup)

describe("the create sheet", () => {
  it("reads every shipped kind", () => {
    expect(KINDS.length).toBeGreaterThan(100)
    expect(KINDS.map((k) => k.identity)).toContain(TASK)
  })

  it.each(KINDS.map((k) => [k.identity, k] as const))(
    "%s draws every row, the folded ones too, and opens every editor",
    (_, kind) => {
      renderSheet(kind)
      openFold()
      expect(screen.queryByRole("button", { name: /\d+ more:/ })).toBeNull()
      // Untouched, nothing is wrong yet.
      expect(screen.queryAllByRole("alert")).toEqual([])
      const rows = [...document.querySelectorAll("[data-property]")].map(
        (el) => el.getAttribute("data-property") ?? ""
      )
      for (const name of rows) {
        const edit = document.querySelector(
          `[data-property="${name}"] [role=button]`
        )
        if (edit) fireEvent.click(edit)
      }
    }
  )

  it("asks a task for its Assignee, Project and Due at, and folds the rest", () => {
    renderSheet(kindOf(TASK), KINDS, false)
    expect(shownRows()).toEqual(["assignee", "project", "dueAt"])
    // The series' machinery and the time a move stamps fold last.
    expect(
      screen.getByRole("button", { name: /\d+ more:/ }).textContent
    ).toMatch(/^11 more: Status, Priority, URL, Recurrence of, Source,/)
    expect(screen.queryAllByRole("alert")).toEqual([])
  })

  it("starts a task where its machine starts it, and says a move comes later", () => {
    renderSheet(kindOf(TASK), KINDS, false)
    openFold()
    const status = document.querySelector("[data-property=status]")
    expect(status?.textContent).toContain("Open")
    expect(status?.textContent).toContain(
      "Set by moving it after the task exists"
    )
    expect(screen.queryByRole("button", { name: /^Status .*edit$/ })).toBeNull()
  })

  // The task's `source` points at any kind: until one is chosen its picker
  // names no kind, and it must not ask for the plural of nothing.
  it("opens a task's any-kind Source on a collection to pick first", () => {
    renderSheet(kindOf(TASK))
    openFold()
    fireEvent.click(screen.getByRole("button", { name: /^Source .*edit$/ }))
    expect(screen.getByText("Pick a collection")).toBeTruthy()
    const source = document.getElementById("sheet-source")
    if (!source) throw new Error("the task's Source picker is not drawn")
    expect(source.textContent).toContain("Pick a collection first")
    fireEvent.click(source)
    expect(
      screen.getByPlaceholderText("Search records, or type an id")
    ).toBeTruthy()
    expect(
      screen.getByText(/Pick a collection first, and its records/)
    ).toBeTruthy()
  })

  it("names nothing wrong on a new person before anything is typed", () => {
    renderSheet(kindOf(PERSON), KINDS, false)
    openFold()
    expect(shownRows()).toContain("relationship")
    expect(screen.queryAllByRole("alert")).toEqual([])
    // An enum reads in its display words, never its stored values.
    fireEvent.click(
      screen.getByRole("button", { name: /^Relationship .*edit$/ })
    )
    expect(screen.getByText("Public figure")).toBeTruthy()
  })
})
