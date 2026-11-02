// @vitest-environment jsdom
/** The approval page's agent row (decision 0207): what the agent reads and
 * changes by name, that it asks questions, its actual daily cap, where what
 * it reads goes, and, in the technical view, every identifier. */

import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"

import type { AuthorizationSetup } from "@/lib/api/types"

import { SetupRow } from "./setup-row"

afterEach(cleanup)

const row: AuthorizationSetup = {
  type: "agent",
  action: "none",
  agent: {
    agent: "ada.example.com/trainingcoach/workoutCoach",
    reads: [
      {
        identity: "ada.example.com/training/routine",
        label: { singular: "Workout routine", plural: "Workout routines" },
        held: true,
      },
      { identity: "substrate.reamde.dev/clinical/condition" },
    ],
    writes: [
      {
        identity: "ada.example.com/training/routine",
        label: { singular: "Workout routine", plural: "Workout routines" },
        held: true,
      },
    ],
    spendCentsPerDay: 100,
    talkedToBy: ["Substrate Chat"],
  },
}

it("names the agent, its reach, its cap and where its reads go", () => {
  render(
    <ul>
      <SetupRow row={row} webhooks={[]} failure={undefined} technical={false} />
    </ul>
  )
  expect(screen.getByText("Add an AI agent: Workout coach")).toBeTruthy()
  const reads = screen.getByRole("list", { name: "What the agent reads" })
  expect(within(reads).getByText("Workout routines")).toBeTruthy()
  expect(
    within(reads).getByText(
      "substrate.reamde.dev/clinical/condition (not on this server yet)"
    )
  ).toBeTruthy()
  expect(screen.getByText(/once it has spent about \$1\.00/)).toBeTruthy()
  expect(screen.getByText("It can ask you questions in the chat.")).toBeTruthy()
  expect(
    screen.getByText(
      /only if they can read everything it reads\. Already: Substrate Chat\./
    )
  ).toBeTruthy()
  expect(screen.getByText(/sent to the AI provider it runs on/)).toBeTruthy()
  expect(screen.queryByText("In place")).toBeNull()
})

it("lists every identifier in the technical view", () => {
  render(
    <ul>
      <SetupRow row={row} webhooks={[]} failure={undefined} technical />
    </ul>
  )
  expect(
    screen.getByText("ada.example.com/trainingcoach/workoutCoach")
  ).toBeTruthy()
  expect(screen.getByText("100 cents")).toBeTruthy()
  expect(screen.getByText(/llm\/interaction \(its questions\)/)).toBeTruthy()
})

it("says approving leaves the agent out when another hand keeps its package", () => {
  render(
    <ul>
      <SetupRow
        row={{
          ...row,
          agent: {
            ...row.agent!,
            withheld: "ada.example.com/trainingcoach is your package",
          },
        }}
        webhooks={[]}
        failure={undefined}
        technical={false}
      />
    </ul>
  )
  expect(screen.getByText("Left out")).toBeTruthy()
  expect(screen.getByText(/Approving leaves this agent out/)).toBeTruthy()
  expect(screen.queryByText(/once it has spent about/)).toBeNull()
})
