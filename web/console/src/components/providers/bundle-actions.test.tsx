// @vitest-environment jsdom
/** A blocked update's note says which of three things is in the way, each
 * under its own heading: connected app builds that still ask for the
 * version held (decision 0214), listed by their facts; a publisher's
 * document that could not be checked; or live records the server's guard
 * lines name. */

import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"

import type { BundleUpgrade } from "@/lib/api/types"
import type { BundleRow } from "@/lib/bundles"
import { shortDateTime } from "@/lib/format"

import { UpgradeBlockedNote } from "./bundle-actions"

afterEach(cleanup)

function row(upgrade: BundleUpgrade): BundleRow {
  return {
    key: "substrate.reamde.dev/training",
    id: "substrate.reamde.dev/training",
    name: "Training",
    authority: "substrate.reamde.dev",
    package: "training",
    installed: true,
    requires: [],
    upgrade,
  }
}

const HOLDER_LINE =
  'Workouts (https://workouts.example.com/client.json), build "2.1", asks for version 3; last checked 2026-11-03T10:12:00Z'

it("lists the builds holding an update back, labelled, and the other blockers apart", () => {
  render(
    <UpgradeBlockedNote
      row={row({
        available: true,
        work: 0,
        lossy: false,
        from: 3,
        to: 4,
        holders: [
          {
            clientName: "Workouts",
            clientId: "https://workouts.example.com/client.json",
            softwareVersion: "2.1",
            checkedAt: "2026-11-03T10:12:00Z",
            floor: 3,
          },
          {
            clientName: "",
            clientId: "http://127.0.0.1:8765/client",
            floor: 2,
          },
        ],
        blockers: [
          HOLDER_LINE,
          "training/session: 3 records still hold nickname",
        ],
      })}
    />
  )
  const note = screen.getByRole("note")
  expect(
    within(note).getByText(
      "An update is waiting. Connected app builds still ask for the version you hold."
    )
  ).toBeTruthy()
  expect(within(note).queryByText(/hold something it would drop/)).toBeNull()
  const list = within(note).getByRole("list", {
    name: "Connected app builds holding the update back",
  })
  const [workouts, local] = within(list).getAllByRole("listitem")
  expect(within(workouts).getByText("Workouts")).toBeTruthy()
  expect(
    within(workouts).getByText("https://workouts.example.com/client.json")
  ).toBeTruthy()
  expect(within(workouts).getByText("2.1")).toBeTruthy()
  expect(within(workouts).getByText("version 3")).toBeTruthy()
  expect(
    within(workouts).getByText(shortDateTime("2026-11-03T10:12:00Z"))
  ).toBeTruthy()
  expect(within(local).getByText("Unnamed app")).toBeTruthy()
  expect(within(local).getByText("Not reported")).toBeTruthy()
  expect(within(local).getByText("No check-in recorded")).toBeTruthy()
  // The server's line restating the holder is not repeated; the guard line
  // on records is kept, apart.
  expect(within(note).queryByText(HOLDER_LINE)).toBeNull()
  expect(within(note).getByText("Also in the way")).toBeTruthy()
  expect(
    within(note).getByText("training/session: 3 records still hold nickname")
  ).toBeTruthy()
})

it("says a publisher's document could not be checked against what is held", () => {
  render(
    <UpgradeBlockedNote
      row={row({
        available: true,
        work: 0,
        lossy: false,
        reviewRequired: true,
        blockers: ["the publisher no longer lists held version 3"],
      })}
    />
  )
  const note = screen.getByRole("note")
  expect(
    within(note).getByText(
      "An update is waiting, but what you hold could not be checked against the publisher’s document."
    )
  ).toBeTruthy()
  expect(
    within(note).getByText("the publisher no longer lists held version 3")
  ).toBeTruthy()
  expect(within(note).queryByText("Also in the way")).toBeNull()
})

it("still says records hold the update back when the guard lines are the blockers", () => {
  render(
    <UpgradeBlockedNote
      row={row({
        available: true,
        work: 0,
        lossy: false,
        blockers: ["training/session: 3 records still hold nickname"],
      })}
    />
  )
  expect(
    screen.getByText(
      "An update is waiting. Some of your records still hold something it would drop."
    )
  ).toBeTruthy()
})
