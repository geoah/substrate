// @vitest-environment jsdom
/** A standard package's update, pause and removal live on its All data page
 * (decision record 0238): Providers lists no standard package, so the
 * update its publisher ships is offered here, or why it is blocked. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import type { BundleUpgrade } from "@/lib/api/types"
import type { BundleRow } from "@/lib/bundles"

import { StandardPackageSection } from "./standard-package"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
  useNavigate: () => vi.fn(),
}))

afterEach(cleanup)

const ID = "substrate.reamde.dev/clinical"

function row(upgrade?: BundleUpgrade): BundleRow {
  return {
    key: ID,
    id: ID,
    name: "clinical",
    authority: "substrate.reamde.dev",
    package: "clinical",
    installed: true,
    tier: "published",
    requires: [],
    upgrade,
    catalog: {
      id: ID,
      name: "clinical",
      authority: "substrate.reamde.dev",
      package: "clinical",
      description: "Your medical record.",
      version: 3,
      tier: "published",
      brings: ["collections"],
      installed: true,
      upgrade,
      closure: {
        kinds: null,
        traits: null,
        functions: null,
        agents: null,
        mappings: null,
        triggers: null,
        records: null,
      },
    },
    status: {
      id: ID,
      name: "clinical",
      authority: "substrate.reamde.dev",
      package: "clinical",
      installed: true,
      enabled: true,
      version: 2,
      accounts: 0,
      functions: 0,
      kinds: 12,
      liveRecords: 40,
    },
  }
}

function mount(r: BundleRow) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <StandardPackageSection row={r} name="Clinical package" />
    </QueryClientProvider>
  )
}

it("says who keeps it up to date, and offers the update it ships", () => {
  mount(row({ available: true, from: 2, to: 3, work: 0, lossy: false }))
  expect(
    screen.getByText(/substrate\.reamde\.dev keeps this package up to date/)
  ).toBeTruthy()
  expect(screen.getByRole("button", { name: "Update" })).toBeTruthy()
  expect(screen.getByRole("button", { name: "Pause" })).toBeTruthy()
  expect(screen.getByRole("button", { name: /Remove/ })).toBeTruthy()
})

it("offers no update where there is none", () => {
  mount(row())
  expect(screen.queryByRole("button", { name: "Update" })).toBeNull()
  expect(screen.getByRole("button", { name: "Pause" })).toBeTruthy()
})
