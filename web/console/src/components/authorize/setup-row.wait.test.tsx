// @vitest-environment jsdom
/** An optional add-on whose provider is not here yet (decision 0216) waits:
 * its row names the provider and wears its own icon, not the check of a
 * row in place. */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"

import { SetupRow } from "./setup-row"

afterEach(cleanup)

it("says what a waiting add-on waits for, under an hourglass", () => {
  render(
    <ul>
      <SetupRow
        row={{
          type: "package",
          action: "wait",
          package: {
            id: "example.com/addon",
            required: 1,
            addonFor: "example.com/whoop",
          },
        }}
        webhooks={[]}
        failure={undefined}
        technical={false}
      />
    </ul>
  )
  const row = screen.getByRole("listitem", { name: "Addon waits for WHOOP" })
  expect(row.querySelector(".lucide-hourglass")).toBeTruthy()
  expect(row.querySelector(".lucide-check")).toBeNull()
})
