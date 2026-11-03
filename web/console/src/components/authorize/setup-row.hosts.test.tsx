// @vitest-environment jsdom
/** The hosts a package's sign-in goes through appear on the approval page
 * in plain view (decision 0220), whole, and the technical view lists them
 * beside the package's other facts. */

import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"

import type { AuthorizationSetup } from "@/lib/api/types"

import { SetupRow } from "./setup-row"

afterEach(cleanup)

const row: AuthorizationSetup = {
  type: "package",
  action: "install",
  package: {
    id: "example.com/addon",
    required: 1,
    shipped: 1,
    oauthHosts: ["accounts.provider.example.com"],
    requires: [
      {
        id: "example.com/provider",
        action: "install",
        oauthHosts: ["login.provider.example.com"],
      },
    ],
  },
}

function mount(technical: boolean) {
  render(
    <ul>
      <SetupRow
        row={row}
        webhooks={[]}
        failure={undefined}
        technical={technical}
      />
    </ul>
  )
  return screen.getByRole("listitem", { name: "Install Addon" })
}

it("says where the sign-in goes without the technical view", () => {
  const li = mount(false)
  expect(
    within(li).getByText(
      "Signing in to it goes through accounts.provider.example.com and login.provider.example.com."
    )
  ).toBeTruthy()
  expect(within(li).queryByText("OAuth hosts")).toBeNull()
})

it("lists the hosts as a term of the package and of each requirement, technically", () => {
  const li = mount(true)
  expect(within(li).getAllByText("accounts.provider.example.com")).toHaveLength(
    1
  )
  expect(within(li).getAllByText("login.provider.example.com")).toHaveLength(1)
  expect(within(li).getAllByText(/OAuth hosts/)).toHaveLength(2)
})
