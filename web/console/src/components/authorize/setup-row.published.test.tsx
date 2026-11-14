// @vitest-environment jsdom
/** The approval page's row for a package an authority document publishes
 * (decision 0214): what approving installs and who publishes it, that it
 * stays after the app is revoked and where it is paused, the publisher's
 * verification in the technical view, and a package nothing publishes. */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"

import type { AuthorizationSetup } from "@/lib/api/types"

import { SetupRow } from "./setup-row"

afterEach(cleanup)

const row: AuthorizationSetup = {
  type: "package",
  action: "install",
  package: {
    id: "substrate.reamde.dev/training",
    description: "Strength sessions down to the set.",
    keptBy: "publisher",
    required: 2,
    shipped: 3,
    publisher: {
      authority: "substrate.reamde.dev",
      name: "Substrate",
      documentUrl:
        "https://substrate.reamde.dev/.well-known/substrate/authority.json",
    },
    requires: [
      {
        id: "substrate.reamde.dev/health",
        action: "install",
        shipped: 4,
        functions: [{ id: "substrate.reamde.dev/core/function/x" }],
      },
    ],
  },
}

function mount(r: AuthorizationSetup, technical = false) {
  render(
    <ul>
      <SetupRow
        row={r}
        webhooks={[]}
        failure={undefined}
        technical={technical}
      />
    </ul>
  )
}

it("says what it installs, who publishes it, and that it stays after a revoke", () => {
  mount(row)
  expect(screen.getByText("Install Training")).toBeTruthy()
  expect(
    screen.getByText("Version 3, published by Substrate (substrate.reamde.dev)")
  ).toBeTruthy()
  expect(
    screen.getByText(
      /Training stays on your server after you revoke the app\. Pause or remove it from its page in the console\./
    )
  ).toBeTruthy()
  const link = screen.getByRole<HTMLAnchorElement>("link", {
    name: /Open its page/,
  })
  expect(link.getAttribute("href")).toBe(
    "/providers/substrate.reamde.dev/training"
  )
  expect(
    screen.getByText("Also installs Health, which runs code on your server.")
  ).toBeTruthy()
  // The catalog row's own version line is not repeated under it.
  expect(screen.queryByText("Version 3")).toBeNull()
})

it("names an update's versions with the publisher", () => {
  mount({ ...row, action: "upgrade", package: { ...row.package!, held: 2 } })
  expect(
    screen.getByText(
      "From version 2 to 3, published by Substrate (substrate.reamde.dev)"
    )
  ).toBeTruthy()
})

it("shows the publisher's verification in the technical view", () => {
  mount(row, true)
  expect(screen.getByText("Its publisher lists")).toBeTruthy()
  expect(screen.getByText("Published by")).toBeTruthy()
  expect(screen.getByText(", Substrate")).toBeTruthy()
  expect(
    screen.getByText(
      "https://substrate.reamde.dev/.well-known/substrate/authority.json"
    )
  ).toBeTruthy()
  expect(screen.getByText("DNS and the document it names")).toBeTruthy()
})

it("says a file on this server vouched where the operator configured one", () => {
  mount(
    {
      ...row,
      package: {
        ...row.package!,
        publisher: { ...row.package!.publisher!, fromFile: true },
      },
    },
    true
  )
  expect(
    screen.getByText("A local file (SUBSTRATE_INSECURE_AUTHORITY_DOCS)")
  ).toBeTruthy()
})

it("says a package nothing publishes can't be done, in the server's words", () => {
  mount({
    type: "package",
    action: "install",
    blocked: {
      reason: "not-published",
      problem:
        "This package is not published where this server can install it: example.org publishes no document listing example.org/training.",
    },
    package: { id: "example.org/training", required: 1 },
  })
  expect(screen.getByText("Can’t be done")).toBeTruthy()
  expect(
    screen.getByText(/example\.org publishes no document listing/)
  ).toBeTruthy()
  expect(screen.queryByText(/stays on your server/)).toBeNull()
  expect(screen.queryByText(/Installs/)).toBeNull()
})

it("says its code is experimental where a publisher outside this server ships it", () => {
  mount({
    ...row,
    package: {
      ...row.package!,
      functions: [{ id: "substrate.reamde.dev/core/function/training/plan" }],
    },
  })
  expect(screen.getByText("Code: experimental.")).toBeTruthy()
  // The publisher's own label is not what says it.
  expect(screen.queryByText(/stable/i)).toBeNull()
})

it("says so where a package it brings first is one", () => {
  mount({
    ...row,
    package: {
      ...row.package!,
      requires: [
        {
          ...row.package!.requires![0],
          publisher: row.package!.publisher,
        },
      ],
    },
  })
  expect(screen.getByText("Code: experimental.")).toBeTruthy()
})

it("says nothing of code this server ships", () => {
  // Training ships no code, and Health is this server's own.
  mount(row)
  expect(screen.queryByText("Code: experimental.")).toBeNull()
})
