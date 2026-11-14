// @vitest-environment jsdom
/** The package page's head, and the one fact on it read off the package's own
 * row: the stability the package states for its kinds. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import { bundleStatusesQueryOptions } from "@/lib/api/bundles"
import { catalogQueryOptions } from "@/lib/api/catalog"
import { discoveryQueryOptions } from "@/lib/api/discovery"
import { functionsQueryOptions } from "@/lib/api/functions"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { releasesQueryOptions } from "@/lib/api/publish"
import { repositoryQueryOptions } from "@/lib/api/repository"
import type { SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { packageRows, packagesQueryOptions } from "@/lib/packages"

let params = { authority: "substrate.reamde.dev", pkg: "core" }

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}))

vi.mock("@/router", () => ({
  authorityRoute: { useParams: () => params },
  packageRoute: { useParams: () => params },
}))

import { PackagePage } from "./authority"

function packageRecord(
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    kind: "substrate.reamde.dev/core/package",
    id,
    properties,
  } as unknown as SubstrateRecord
}

const PACKAGES = [
  packageRecord("substrate.reamde.dev/core", { stability: "stable" }),
  packageRecord("ada.example.com/tasks", {}),
]

/** The page over a repository whose package rows are `records`, or still
 * loading where none are given. Every other read is answered empty: the head
 * is what these cases read. */
function renderPage(
  technical: boolean,
  records?: SubstrateRecord[],
  functions: SubstrateRecord[] = [],
  seed?: (client: QueryClient) => void
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  seed?.(client)
  client.setQueryData(kindsQueryOptions.queryKey, [])
  client.setQueryData(functionsQueryOptions.queryKey, functions)
  client.setQueryData(catalogQueryOptions.queryKey, [])
  client.setQueryData(bundleStatusesQueryOptions.queryKey, [])
  client.setQueryData(repositoryQueryOptions.queryKey, {
    authority: "ada.example.com",
  } as never)
  if (records)
    client.setQueryData(packagesQueryOptions.queryKey, packageRows({ records }))
  return render(
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          views: [],
          ...DEFAULT_SETTINGS,
          technicalDetails: technical,
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>
        <PackagePage />
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

/** The words of the head's meta line that carry the label, or undefined
 * where the line has none. */
function stabilityFact(): string | undefined {
  const meta = document.querySelector('[data-slot="page-meta"]')
  return [...(meta?.children ?? [])]
    .map((el) => el.textContent ?? "")
    .find((text) => text.startsWith("Stability"))
}

describe("PackagePage and the package's stability", () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    params = { authority: "substrate.reamde.dev", pkg: "core" }
  })

  it("shows the label the package states, under its own word", () => {
    renderPage(true, PACKAGES)
    expect(screen.getByRole("heading", { name: "core" })).toBeTruthy()
    expect(stabilityFact()).toBe("Stability stable")
  })

  // A package that states none promises the least.
  it("shows experimental for a package that states no label", () => {
    params = { authority: "ada.example.com", pkg: "tasks" }
    renderPage(true, PACKAGES)
    expect(stabilityFact()).toBe("Stability experimental")
  })

  // The label is for somebody building on the package's kinds: it sits with
  // the package's id, which everyday mode leaves out too.
  it("leaves the label out of everyday mode", () => {
    renderPage(false, PACKAGES)
    expect(stabilityFact()).toBeUndefined()
  })

  // The fallback is for a row that states nothing, never for a row not read
  // yet: until the rows load there is no label to show.
  it("shows no label until the package rows have loaded", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>(() => {}))
    )
    renderPage(true)
    expect(stabilityFact()).toBeUndefined()
  })

  it("shows no label for a package the repository holds no row of", () => {
    params = { authority: "ada.example.com", pkg: "nothing" }
    renderPage(true, PACKAGES)
    expect(stabilityFact()).toBeUndefined()
  })
})

/** The words of the head's meta line that say the code's own label, or
 * undefined where the line has none. */
function codeFact(): string | undefined {
  const meta = document.querySelector('[data-slot="page-meta"]')
  return [...(meta?.children ?? [])]
    .map((el) => el.textContent ?? "")
    .find((text) => text.startsWith("Code"))
}

describe("PackagePage and code from outside publishers", () => {
  const sync = {
    kind: "substrate.reamde.dev/core/function",
    id: "publisher.example/notes/sync",
    properties: { name: "sync" },
  } as unknown as SubstrateRecord
  const published = packageRecord("publisher.example/notes", {
    stability: "stable",
    publishedFrom:
      "https://publisher.example/.well-known/substrate/authority.json",
  })

  afterEach(() => {
    cleanup()
    params = { authority: "substrate.reamde.dev", pkg: "core" }
  })

  // Where a person decides, beside the publisher's own label and apart from
  // it: the code a publisher's document installed is experimental at 1.0.
  it("says the code of a package a publisher's document installed is experimental", () => {
    params = { authority: "publisher.example", pkg: "notes" }
    renderPage(false, [published], [sync])
    expect(codeFact()).toBe("Code experimental")
  })

  it("keeps the publisher's own label as it states it", () => {
    params = { authority: "publisher.example", pkg: "notes" }
    renderPage(true, [published], [sync])
    expect(stabilityFact()).toBe("Stability stable")
    expect(codeFact()).toBe("Code experimental")
  })

  it("says nothing of a package that ships no code, or of this server's own", () => {
    params = { authority: "publisher.example", pkg: "notes" }
    renderPage(false, [published])
    expect(codeFact()).toBeUndefined()
    cleanup()
    params = { authority: "ada.example.com", pkg: "tasks" }
    renderPage(
      false,
      [packageRecord("ada.example.com/tasks", {})],
      [
        {
          ...sync,
          id: "ada.example.com/tasks/sweep",
        },
      ]
    )
    expect(codeFact()).toBeUndefined()
  })
})

/** The head's publishing action: Publish… on a package of the repository's
 * own, New release… on one it publishes, nothing elsewhere. */
describe("PackagePage and publishing", () => {
  afterEach(() => {
    cleanup()
    params = { authority: "substrate.reamde.dev", pkg: "core" }
  })

  function publishing(client: QueryClient) {
    client.setQueryData(discoveryQueryOptions.queryKey, {
      features: [
        { name: "publishing", stability: "experimental", revision: 1 },
      ],
    })
    client.setQueryData(releasesQueryOptions.queryKey, [
      {
        package: "ada.example.com/recipes",
        version: 1,
        sha256: "a".repeat(64),
        releasedAt: "2026-11-14T12:00:00Z",
      },
    ])
  }

  it("offers Publish… on a package in ~", () => {
    params = { authority: "~", pkg: "recipes" }
    renderPage(false, PACKAGES, [], publishing)
    expect(screen.getByRole("button", { name: "Publish…" })).toBeTruthy()
    expect(screen.queryByRole("button", { name: "New release…" })).toBeNull()
  })

  it("offers New release… on a package the repository publishes", () => {
    params = { authority: "ada.example.com", pkg: "recipes" }
    renderPage(false, PACKAGES, [], publishing)
    expect(screen.getByRole("button", { name: "New release…" })).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Publish…" })).toBeNull()
  })

  it("offers neither on a package another publisher keeps", () => {
    renderPage(false, PACKAGES, [], publishing)
    expect(
      screen.queryByRole("button", { name: /Publish…|New release…/ })
    ).toBeNull()
  })
})
