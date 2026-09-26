// @vitest-environment jsdom
/** The new-record sheet, held to every kind this repository ships: each one
 * opens, and every row it folds away opens too, without a throw. A row the
 * console cannot draw fails alone, in words, and the rest of the sheet stays
 * usable. */

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

// One property name no shipped kind declares stands in for a row the console
// cannot draw.
const BROKEN = "brokenrow"
vi.mock("@/components/record/property-field", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/components/record/property-field")>()
  return {
    ...actual,
    PropertyField: (props: Parameters<typeof actual.PropertyField>[0]) => {
      if (props.field.name === BROKEN) throw new Error("a row that cannot draw")
      return <actual.PropertyField {...props} />
    },
  }
})

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

function renderSheet(kind: KindInfo, kinds: KindInfo[] = KINDS) {
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
          technicalDetails: true,
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
    "%s draws every row, the folded ones too",
    (_, kind) => {
      const failed = vi.spyOn(console, "error")
      renderSheet(kind)
      openFold()
      expect(screen.queryByRole("button", { name: /\d+ more:/ })).toBeNull()
      // A caught row says so in words; none may.
      expect(screen.queryByText(/couldn’t be shown/)).toBeNull()
      expect(
        failed.mock.calls.filter((c) => String(c[0]).endsWith(" failed"))
      ).toEqual([])
      failed.mockRestore()
    }
  )

  // The task's `source` points at any kind: until one is chosen its picker
  // names no kind, and it must not ask for the plural of nothing.
  it("opens a task's Assignee, Project and any-kind Source", () => {
    const task = KINDS.find((k) => k.identity === TASK)
    if (!task) throw new Error("the tasks sample ships no task kind")
    renderSheet(task)
    openFold()
    expect(screen.getByLabelText(/^Assignee/)).toBeTruthy()
    expect(screen.getByLabelText(/^Project/)).toBeTruthy()
    expect(screen.getByLabelText(/^Due at/)).toBeTruthy()
    // The template's blank lines are "not set", and the write leaves them
    // out: an untouched row names no problem.
    expect(screen.queryAllByRole("alert").map((a) => a.textContent)).toEqual([])
    const source = document.getElementById("new-source")
    if (!source) throw new Error("the task's Source row is not drawn")
    fireEvent.click(source)
    expect(
      screen.getByPlaceholderText("Search records, or type an id")
    ).toBeTruthy()
    expect(screen.getByText(/its records are listed here/)).toBeTruthy()
  })

  it("fails one row alone and keeps the rest of the sheet", () => {
    const quiet = vi.spyOn(console, "error").mockImplementation(() => {})
    const kind: KindInfo = {
      identity: "example.com/things/thing",
      name: "thing",
      authority: "example.com",
      package: "things",
      version: 1,
      source: "installed",
      description: "",
      definition: {
        properties: {
          name: { type: "string" },
          [BROKEN]: { type: "string", required: true },
          note: { type: "string", required: true },
        },
      },
    }
    renderSheet(kind, [kind])
    quiet.mockRestore()
    expect(screen.getByRole("alert").textContent).toContain(
      "couldn’t be shown: a row that cannot draw"
    )
    expect(screen.getByLabelText(/^Note/)).toBeTruthy()
    openFold()
    expect(screen.getByLabelText(/^Name/)).toBeTruthy()
  })
})
