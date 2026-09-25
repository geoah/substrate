// @vitest-environment jsdom
/** "Connected to": the fan-in grouped by the kind and property it points
 * through, headed in everyday words ("Tasks with this as their Assignee"),
 * carrying a done count where the kind has a done state, folded after ten
 * rows, and without the mapping slots a provider's copies point through. */

import type { ReactNode } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, ...rest }: { children?: ReactNode }) => (
    <a href="#" {...(rest as object)}>
      {children}
    </a>
  ),
}))

const PERSON = "ada.example.com/people/person"
const TASK = "ada.example.com/tasks/task"
const CONTACT = "providers.substrate.reamde.dev/google/contact"

const tasks = Array.from({ length: 12 }, (_, i) => ({
  id: `t${i}`,
  kind: TASK,
  properties: { title: `Task ${i}`, status: i < 3 ? "done" : "open" },
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
}))

vi.mock("@/lib/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/http")>()
  return {
    ...actual,
    request: vi.fn((_method: string, path: string) => {
      if (decodeURIComponent(path).includes("referencing")) {
        const contact = {
          id: "c1",
          kind: CONTACT,
          properties: { title: "Grace (Google)" },
          labels: {},
          version: 1,
          createdAt: "",
          updatedAt: "",
        }
        return Promise.resolve({
          records: [...tasks, contact],
          matches: {
            ...Object.fromEntries(
              tasks.map((t) => [`${TASK}/${t.id}`, [{ property: "assignee" }]])
            ),
            [`${CONTACT}/c1`]: [{ property: "person" }],
          },
          head: 1,
          generation: "g",
        })
      }
      return Promise.resolve({ records: [], head: 1, generation: "g" })
    }),
  }
})

import { ConnectedSection } from "./connected"
import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

const kind = (identity: string, properties: Record<string, unknown>) => {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: { properties },
  } satisfies KindInfo
}
const task = kind(TASK, {
  status: { type: "state", states: ["open", "done"], initial: "open" },
  assignee: { type: "reference", kind: PERSON, displayName: "Assignee" },
})
const person = kind(PERSON, {})

const grace: SubstrateRecord = {
  id: "grace",
  kind: PERSON,
  properties: { title: "Grace" },
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
  linkedFrom: [
    {
      ref: `${CONTACT}/c1`,
      kind: CONTACT,
      property: "person",
      mapping: "ada.example.com/people/googlecontactperson",
    },
  ],
}

function preferences(
  technicalDetails: boolean
): ConsolePreferencesContextValue {
  return {
    preferences: {
      collapsed: [],
      favorites: [],
      sidebarOpen: true,
      ...DEFAULT_SETTINGS,
      technicalDetails,
    },
    busy: false,
    change: () => {},
    set: () => {},
  }
}

function renderSection(technical = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(["registry", "kinds"], [task, person])
  return render(
    <ConsolePreferencesContext.Provider value={preferences(technical)}>
      <QueryClientProvider client={client}>
        <ConnectedSection record={grace} kind={person} kinds={[task, person]} />
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

afterEach(cleanup)

describe("ConnectedSection", () => {
  it("groups by kind and property, in everyday words, without the mapping slot", async () => {
    renderSection()
    await screen.findByText("Tasks")
    const groups = document.querySelectorAll("[data-slot=connected-group]")
    expect(groups).toHaveLength(1)
    expect(groups[0].textContent).toContain("with this as their Assignee")
    expect(groups[0].textContent).toContain("3 of 12 done")
    expect(document.body.textContent).not.toContain("Grace (Google)")
  })

  it("shows ten rows, then all of them on asking", async () => {
    renderSection()
    await screen.findByText("Tasks")
    expect(screen.getAllByRole("link", { name: /Task \d+/ })).toHaveLength(10)
    fireEvent.click(screen.getByRole("button", { name: "Show all 12" }))
    expect(screen.getAllByRole("link", { name: /Task \d+/ })).toHaveLength(12)
  })

  it("names the kind reference and the property key in technical mode", async () => {
    renderSection(true)
    const group = await screen.findByText(/records point here through their/)
    expect(group.closest("[data-slot=connected-group]")?.textContent).toContain(
      "assignee"
    )
  })
})
