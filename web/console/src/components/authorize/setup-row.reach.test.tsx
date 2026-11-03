// @vitest-environment jsdom
/** The approval page's package row says what the package's code reaches
 * and whether it uses the internet (decision 0215), and the technical view
 * lists each function's reads, writes and hosts and each reach with what
 * reaches it and the scope that covers it. */

import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react"
import { afterEach, expect, it } from "vitest"

import type { AuthorizationSetup } from "@/lib/api/types"

import { SetupRow } from "./setup-row"

afterEach(cleanup)

const APPLEHEALTH = "providers.substrate.reamde.dev/applehealth"

const row: AuthorizationSetup = {
  type: "package",
  action: "install",
  package: {
    id: APPLEHEALTH,
    required: 5,
    shipped: 5,
    functions: [
      {
        id: `${APPLEHEALTH}/adaptapplehealth`,
        reads: [`${APPLEHEALTH}/export`],
        writes: ["substrate.reamde.dev/health/sleep"],
        network: ["api.example.com"],
      },
      { id: `${APPLEHEALTH}/applehealthingest` },
    ],
    reach: [
      {
        pattern: `${APPLEHEALTH}/export`,
        by: [{ type: "function", id: `${APPLEHEALTH}/adaptapplehealth` }],
        covered: true,
        scope: `read:${APPLEHEALTH}/export`,
      },
      {
        pattern: "substrate.reamde.dev/health/sleep",
        write: true,
        by: [
          { type: "function", id: `${APPLEHEALTH}/adaptapplehealth` },
          { type: "trigger", id: "applehealth-to-health" },
        ],
        covered: false,
        scope: "write:substrate.reamde.dev/health/sleep",
      },
    ],
  },
}

it("says in plain words what its code reaches and that it uses the internet", () => {
  render(
    <ul>
      <SetupRow row={row} webhooks={[]} failure={undefined} technical={false} />
    </ul>
  )
  expect(screen.getByText("Runs code on your server.")).toBeTruthy()
  expect(
    screen.getByText(
      "Its code can send data to the internet; it names api.example.com."
    )
  ).toBeTruthy()
  expect(
    screen.getByText("Its code changes sleeps (not allowed) and reads exports.")
  ).toBeTruthy()
  expect(screen.queryByText(`${APPLEHEALTH}/adaptapplehealth`)).toBeNull()
})

it("lists each function's reads, writes and hosts and each reach with its scope, technically", async () => {
  render(
    <ul>
      <SetupRow row={row} webhooks={[]} failure={undefined} technical />
    </ul>
  )
  fireEvent.click(
    screen.getByRole("button", { name: "Apple Health functions (2)" })
  )
  const adapter = (
    await screen.findByText(`${APPLEHEALTH}/adaptapplehealth`, {
      selector: "span",
    })
  ).closest("li") as HTMLElement
  expect(within(adapter).getByText("May read")).toBeTruthy()
  expect(within(adapter).getByText(`${APPLEHEALTH}/export`)).toBeTruthy()
  expect(within(adapter).getByText("api.example.com")).toBeTruthy()
  const ingest = screen
    .getByText(`${APPLEHEALTH}/applehealthingest`, { selector: "span" })
    .closest("li") as HTMLElement
  expect(within(ingest).getByText("Reads nothing")).toBeTruthy()
  expect(within(ingest).getByText("Writes nothing")).toBeTruthy()
  expect(within(ingest).getByText("none")).toBeTruthy()
  fireEvent.click(
    screen.getByRole("button", { name: "Apple Health reach (2)" })
  )
  expect(await screen.findByText("Covered by")).toBeTruthy()
  expect(screen.getByText("Not covered: needs")).toBeTruthy()
  expect(
    screen.getByText("write:substrate.reamde.dev/health/sleep")
  ).toBeTruthy()
  expect(screen.getByText("applehealth-to-health")).toBeTruthy()
})
