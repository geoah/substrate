// @vitest-environment jsdom
/** The property ledger: a function manager reads as the sync of the kind its
 * value came from and links its declaration, the source record is a pill of
 * its own, the tier is a chip that says what it means, every alternative is a
 * row with "Use this" — and the two writes are the PATCH docs/projection.md
 * describes: an alternative's value, or null to release. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: React.ReactNode
  }) => (
    <a
      href={Object.entries(params ?? {}).reduce(
        (path, [key, value]) => path.replace(`$${key}`, value),
        to
      )}
      {...rest}
    >
      {children}
    </a>
  ),
}))

const wire = vi.hoisted(() => ({
  writes: [] as { method: string; path: string; body: unknown }[],
}))

vi.mock("@/lib/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/http")>()
  return {
    ...actual,
    request: vi.fn((method: string, path: string, body?: unknown) => {
      if (method === "PATCH") {
        wire.writes.push({ method, path, body })
        return Promise.resolve({ ...record, version: record.version + 1 })
      }
      throw new Error(`unexpected request: ${method} ${path}`)
    }),
  }
})

import { ProvenanceRail } from "./provenance"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

const PERSON = "ada.example.com/people/person"
const BEEPER = "providers.substrate.reamde.dev/beeper/user"
const GITHUB = "providers.substrate.reamde.dev/github/user"
const BEEPER_SYNC = "function:providers.substrate.reamde.dev:beeper:beepersync"
const GITHUB_SYNC = "function:providers.substrate.reamde.dev:github:githubsync"

const kind = (identity: string, properties = {}): KindInfo => {
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
  }
}
const person = kind(PERSON, {
  name: { type: "string" },
  displayName: { type: "string" },
  emails: { type: "email", repeated: true },
})
const kinds = [
  person,
  kind(BEEPER),
  kind(GITHUB),
  kind("substrate.reamde.dev/core/function"),
]

const record: SubstrateRecord = {
  id: "p1",
  kind: PERSON,
  properties: {
    name: "Ada Lovelace",
    displayName: "ada",
    emails: ["ada@example.com"],
    title: "Ada Lovelace",
  },
  labels: {},
  version: 7,
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-10T00:00:00Z",
  propertyMeta: {
    name: {
      manager: "console",
      tier: "owner",
      updatedAt: "2026-09-10T00:00:00Z",
      alternatives: [
        {
          actor: BEEPER_SYNC,
          value: "Ada L.",
          updatedAt: "2026-09-09T00:00:00Z",
          source: `${BEEPER}/u1`,
        },
        {
          actor: GITHUB_SYNC,
          value: "adalovelace",
          updatedAt: "2026-09-08T00:00:00Z",
          source: `${GITHUB}/gh1`,
        },
      ],
    },
    displayName: {
      manager: GITHUB_SYNC,
      tier: "machine",
      updatedAt: "2026-09-08T00:00:00Z",
      source: `${GITHUB}/gh1`,
    },
    emails: {
      manager: BEEPER_SYNC,
      tier: "machine",
      updatedAt: "2026-09-09T00:00:00Z",
      source: `${BEEPER}/u1`,
    },
  },
  linkedFrom: [
    {
      ref: `${BEEPER}/u1`,
      kind: BEEPER,
      title: "Ada (Beeper)",
      property: "person",
      mapping: "ada.example.com/people/beeperuserperson",
    },
    {
      ref: `${GITHUB}/gh1`,
      kind: GITHUB,
      title: "adalovelace",
      property: "person",
      mapping: "ada.example.com/people/githubuserperson",
    },
  ],
}

function renderRail(e: SubstrateRecord = record) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ProvenanceRail record={e} kind={person} kinds={kinds} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  wire.writes = []
})
afterEach(cleanup)

const rowOf = (container: HTMLElement, property: string) =>
  container.querySelector<HTMLElement>(`[data-property="${property}"]`)!

describe("ProvenanceRail", () => {
  it("lists one row per property in the declaration's order, with the stored value", () => {
    const { container } = renderRail()
    const names = [...container.querySelectorAll("[data-property]")].map((li) =>
      li.getAttribute("data-property")
    )
    // The derived `title` built-in is not a row: nothing manages it.
    expect(names).toEqual(["name", "displayName", "emails"])
    expect(rowOf(container, "name").textContent).toContain("Ada Lovelace")
  })

  it("never shows a bare actor string: a function manager is its provider's sync, linked", () => {
    const { container } = renderRail()
    const row = rowOf(container, "displayName")
    expect(row.textContent).toContain("GitHub sync")
    expect(row.textContent).not.toContain(GITHUB_SYNC)
    const pill = [...row.querySelectorAll("a")].find((a) =>
      a.textContent?.includes("GitHub sync")
    )
    // The full actor rides the hover card; the click reaches its declaration.
    expect(pill?.getAttribute("href")).toBe(
      "/data/substrate.reamde.dev/core/function/providers.substrate.reamde.dev/github/githubsync"
    )
  })

  it("shows the source record behind a machine-held value as a pill titled off the links", () => {
    const { container } = renderRail()
    const row = rowOf(container, "displayName")
    const source = [...row.querySelectorAll("a")].find(
      (a) => a.textContent === "adalovelace"
    )
    expect(source?.getAttribute("href")).toBe(
      "/data/providers.substrate.reamde.dev/github/user/gh1"
    )
    expect(row.querySelector("[data-tier]")?.textContent).toBe(
      "follows sources"
    )
  })

  it("says a hand edit is held by you, offers Release, and lists every alternative as a row", () => {
    const { container } = renderRail()
    const row = rowOf(container, "name")
    expect(row.querySelector("[data-tier]")?.textContent).toBe("held by you")
    expect(row.textContent).toContain("Release")
    const alts = row.querySelectorAll("[data-alternative]")
    expect(alts).toHaveLength(2)
    expect(alts[0].textContent).toContain("Ada L.")
    expect(alts[0].textContent).toContain("Beeper sync")
    expect(alts[0].textContent).toContain("Ada (Beeper)")
    expect(alts[0].textContent).toContain("via beeperuserperson")
    expect(alts[1].textContent).toContain("adalovelace")
    // A machine-held row with no alternatives offers no Release.
    expect(rowOf(container, "displayName").textContent).not.toContain("Release")
  })

  it("Use this asks first, then PATCHes the alternative's value at the read version", async () => {
    const { container, getByText, getByRole } = renderRail()
    const row = rowOf(container, "name")
    fireEvent.click(row.querySelectorAll("[data-alternative] button")[0])
    // The consequence is named before the write.
    expect(getByRole("dialog").textContent).toContain(
      "ignores fresher source values until you release it"
    )
    expect(wire.writes).toHaveLength(0)
    fireEvent.click(getByText("Use this", { selector: "[role=dialog] button" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].path).toBe("/api/v1/ada.example.com/people/person/p1")
    expect(wire.writes[0].body).toEqual({
      properties: { name: "Ada L." },
      ifVersion: 7,
    })
  })

  it("Release asks first, then PATCHes the property to null so projection refills it", async () => {
    const { container, getByText, getByRole } = renderRail()
    fireEvent.click(
      getByText("Release", { selector: "[data-property] button" })
    )
    expect(getByRole("dialog").textContent).toContain("refills")
    fireEvent.click(getByText("Release", { selector: "[role=dialog] button" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0].body).toEqual({
      properties: { name: null },
      ifVersion: 7,
    })
    expect(container.querySelector("[role=dialog]")).toBeNull()
  })

  // A reference value is served as `{ref: "<kind>/<id>"}`; the ledger read it
  // through `cellValue`, which printed the object's keys — the literal `{ref}`.
  it("renders a reference value, its alternatives and the confirm dialog as record pills", () => {
    const client = new QueryClient()
    const e: SubstrateRecord = {
      ...record,
      properties: { ...record.properties, name: { ref: `${GITHUB}/gh1` } },
      propertyMeta: {
        name: {
          manager: "console",
          tier: "owner",
          alternatives: [
            {
              actor: BEEPER_SYNC,
              value: { ref: `${BEEPER}/u1` },
              updatedAt: "2026-09-09T00:00:00Z",
            },
          ],
        },
      },
      linkedFrom: [],
    }
    const { container, getByRole } = render(
      <QueryClientProvider client={client}>
        <ProvenanceRail
          record={e}
          kind={person}
          kinds={kinds}
          referenceTitles={new Map([[`${GITHUB}/gh1`, "adalovelace"]])}
        />
      </QueryClientProvider>
    )
    const row = rowOf(container, "name")
    expect(row.textContent).not.toContain("{ref}")
    const held = [...row.querySelectorAll("a")].find(
      (a) => a.getAttribute("href") === `/data/${GITHUB}/gh1`
    )
    expect(held?.textContent).toContain("adalovelace")
    const alt = row.querySelector<HTMLElement>("[data-alternative]")!
    // An untitled referent reads by its kind, never by a bare id.
    const altLink = alt.querySelector(`a[href="/data/${BEEPER}/u1"]`)
    expect(altLink?.textContent).not.toContain("{ref}")
    expect(altLink?.textContent).not.toMatch(/^u1$/)
    fireEvent.click(alt.querySelector("button")!)
    const dialog = getByRole("dialog")
    expect(dialog.textContent).not.toContain("{ref}")
    expect(dialog.querySelector(`a[href="/data/${BEEPER}/u1"]`)).not.toBeNull()
  })

  it("cancelling writes nothing", () => {
    const { container, getByText } = renderRail()
    const row = rowOf(container, "name")
    fireEvent.click(row.querySelectorAll("[data-alternative] button")[0])
    fireEvent.click(getByText("Cancel"))
    expect(wire.writes).toHaveLength(0)
  })
})
