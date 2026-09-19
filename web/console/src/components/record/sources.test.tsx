// @vitest-environment jsdom
/** The Sources section: one group per mapping, headed by the mapping as a
 * link with the kind it reads, its count and what it contributes; members are
 * RecordPills deduplicated by record, sorted by title, ten at a time; merged-
 * away records sit under Merged with the merge and its request linked. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

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

const CORE = "substrate.reamde.dev/core"
const PERSON = "ada.example.com/people/person"
const BEEPER = "providers.substrate.reamde.dev/beeper/user"
const GITHUB = "providers.substrate.reamde.dev/github/user"
const BEEPER_MAPPING = "ada.example.com/people/beeperuserperson"
const GITHUB_MAPPING = "ada.example.com/people/githubuserperson"

// The two reverse reads the Merged group makes: the recordmerge whose
// winner is the record, and the request that proposed it.
vi.mock("@/lib/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/http")>()
  return {
    ...actual,
    request: vi.fn((_method: string, path: string) => {
      const url = new URL(path, "http://x")
      const filter = JSON.parse(url.searchParams.get("filter") ?? "{}")
      const row = (kind: string, id: string, properties: object) => ({
        id,
        kind,
        properties,
        labels: {},
        version: 1,
        createdAt: "2026-09-01T00:00:00Z",
        updatedAt: "2026-09-01T00:00:00Z",
      })
      if (filter.kinds?.includes(`${CORE}/recordmerge`)) {
        return Promise.resolve({
          records: [
            row(`${CORE}/recordmerge`, "m1", {
              winner: { ref: `${PERSON}/p1` },
              loser: { ref: `${PERSON}/p0` },
            }),
          ],
          head: 1,
          generation: "g",
        })
      }
      if (filter.kinds?.includes(`${CORE}/recordmergerequest`)) {
        return Promise.resolve({
          records: [
            row(`${CORE}/recordmergerequest`, "dupe-p0-p1", {
              title: "same person",
              winner: { ref: `${PERSON}/p1` },
              loser: { ref: `${PERSON}/p0` },
              decision: "accepted",
            }),
          ],
          head: 1,
          generation: "g",
        })
      }
      throw new Error(`unexpected request: ${path}`)
    }),
  }
})

import { SourcesSection } from "./sources"
import type { KindInfo, LinkedRecord, SubstrateRecord } from "@/lib/api/types"

const kind = (identity: string): KindInfo => {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: {},
  }
}
const kinds = [
  kind(PERSON),
  kind(BEEPER),
  kind(`${CORE}/recordmapping`),
  kind(`${CORE}/recordmerge`),
  kind(`${CORE}/recordmergerequest`),
]

const link = (
  k: string,
  id: string,
  title: string,
  mapping: string
): LinkedRecord => ({
  ref: `${k}/${id}`,
  kind: k,
  title,
  property: "person",
  mapping,
})

const beeperLinks = Array.from({ length: 12 }, (_, i) =>
  link(BEEPER, `u${String(i).padStart(2, "0")}`, `Person ${i}`, BEEPER_MAPPING)
)

const record: SubstrateRecord = {
  id: "p1",
  kind: PERSON,
  properties: { name: "Ada", title: "Ada" },
  labels: {},
  version: 4,
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-01T00:00:00Z",
  linkedFrom: [
    ...beeperLinks,
    // The index kept two rows for one source: one member.
    beeperLinks[0],
    link(GITHUB, "gh1", "ada", GITHUB_MAPPING),
  ],
}

const mappings: SubstrateRecord[] = [
  {
    id: BEEPER_MAPPING,
    kind: `${CORE}/recordmapping`,
    properties: {
      title: "beeperuserperson",
      description: "Converges a Beeper user onto the person it belongs to.",
      from: { ref: `${CORE}/kind/${BEEPER}` },
      to: { ref: `${CORE}/kind/${PERSON}` },
      property: "person",
      map: {
        name: { path: "displayName" },
        emails: { path: "email", merge: "union" },
        phones: { path: "phone", merge: "union" },
      },
    },
    labels: {},
    version: 2,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  },
]

function renderSources(e: SubstrateRecord = record) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <SourcesSection record={e} kinds={kinds} mappings={mappings} />
    </QueryClientProvider>
  )
}

afterEach(cleanup)

describe("SourcesSection", () => {
  it("groups the links by mapping, headed by the mapping as a link with its count and what it contributes", () => {
    const { container } = renderSources()
    const text = container.textContent ?? ""
    expect(text).toContain("13 records map onto this person")
    expect(text).toContain("through 2 mappings")
    // The header pill links to the recordmapping record and reads as its title.
    const heading = [...container.querySelectorAll("a")].find(
      (a) => a.textContent === "beeperuserperson"
    )
    expect(heading?.getAttribute("href")).toBe(
      `/data/substrate.reamde.dev/core/recordmapping/${BEEPER_MAPPING}`
    )
    expect(text).toContain("12 records")
    expect(text).toContain("name, emails, phones")
    expect(text).toContain("Converges a Beeper user")
    // The kind it reads links to that collection, and the slot is named.
    const kindLink = [...container.querySelectorAll("a")].find(
      (a) => a.textContent === BEEPER
    )
    expect(kindLink?.getAttribute("href")).toBe(
      "/data/providers.substrate.reamde.dev/beeper/user"
    )
    expect(text).toContain("through its person slot")
  })

  it("deduplicates members by record, sorts them by title and folds past ten", () => {
    const { container, getByText } = renderSources()
    const pills = () =>
      [...container.querySelectorAll("a")]
        .map((a) => a.getAttribute("href") ?? "")
        .filter((h) => h.includes("/beeper/user/"))
    expect(pills()).toHaveLength(10)
    // Sorted by title: "Person 0", "Person 1", "Person 10", "Person 11", …
    expect(pills()[0]).toContain("/u00")
    expect(pills()[2]).toContain("/u10")
    fireEvent.click(getByText("Show all 12"))
    expect(pills()).toHaveLength(12)
    expect(new Set(pills()).size).toBe(12)
  })

  it("still lists a group whose mapping declaration did not load, and says so", () => {
    const { container } = renderSources()
    const text = container.textContent ?? ""
    expect(text).toContain("githubuserperson")
    expect(text).toContain("1 record")
    expect(text).toContain("mapping declaration not found")
    // An unknown source kind renders inert, never as a dead link.
    expect(
      [...container.querySelectorAll("a")].some((a) =>
        a.getAttribute("href")?.includes("gh1")
      )
    ).toBe(false)
  })

  it("lists merged-away records under Merged with the merge and its request", async () => {
    const { container } = renderSources({ ...record, formerIds: ["p0"] })
    await waitFor(() => {
      expect(container.textContent).toContain("merged in by")
    })
    const hrefs = [...container.querySelectorAll("a")].map((a) =>
      a.getAttribute("href")
    )
    expect(hrefs).toContain(`/data/ada.example.com/people/person/p0`)
    expect(hrefs).toContain(`/data/substrate.reamde.dev/core/recordmerge/m1`)
    expect(hrefs).toContain(
      `/data/substrate.reamde.dev/core/recordmergerequest/dupe-p0-p1`
    )
    expect(container.textContent).toContain("same person")
  })

  it("says when no mapping targets the kind at all", () => {
    const { container } = renderSources({ ...record, linkedFrom: undefined })
    expect(container.textContent).toContain("No mapping targets this kind")
  })
})
