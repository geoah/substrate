// @vitest-environment jsdom
/** The approval page's AI provider choice: the package and its agents by
 * name, each agent's cap in dollars and its model on a line of its own, the
 * ids behind the technical view, and the owner's choice sent as made. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import type { AuthorizationProviderInput } from "@/lib/api/types"

import { ProviderPicker } from "./provider-picker"

afterEach(cleanup)

const input: AuthorizationProviderInput = {
  package: "coach.example.com/coach",
  input: "model",
  agents: [
    {
      agent: "coach.example.com/coach/run",
      models: ["a"],
      spendCentsPerDay: 100,
    },
  ],
  options: [
    {
      id: "one",
      label: "My AI",
      models: { "coach.example.com/coach/run": "gateway/a" },
    },
  ],
}

function mount(technical = false, value = "one") {
  const onChange = vi.fn()
  render(
    <ProviderPicker
      input={input}
      value={value}
      disabled={false}
      technical={technical}
      onChange={onChange}
    />
  )
  return onChange
}

it("names the package and the agent, says the cap in dollars and the model on its own line, and sends the choice", () => {
  const onChange = mount()
  expect(
    screen.getByLabelText<HTMLSelectElement>("AI provider for Coach").value
  ).toBe("one")
  expect(screen.getByRole("option", { name: "My AI" })).toBeTruthy()
  const agent = screen.getByRole("listitem")
  expect(agent.textContent).toContain("Run spends up to $1.00 a day")
  expect(agent.textContent).toContain("Model gateway/a")
  expect(screen.queryByText(/US cents/)).toBeNull()
  // Identifiers wait behind the switch.
  expect(screen.queryByText("coach.example.com/coach/run")).toBeNull()
  expect(screen.queryByText("coach.example.com/coach/model")).toBeNull()
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "" } })
  expect(onChange).toHaveBeenCalledWith("")
})

it("shows no model until a provider is chosen", () => {
  mount(false, "")
  expect(screen.queryByText(/Model/)).toBeNull()
})

it("adds the input's and the agent's id in the technical view", () => {
  mount(true)
  expect(screen.getByText("coach.example.com/coach/model")).toBeTruthy()
  expect(screen.getByText("coach.example.com/coach/run")).toBeTruthy()
})

it("names the input where a package has more than one", () => {
  render(
    <ProviderPicker
      input={input}
      inputs={[input, { ...input, input: "judge" }]}
      value=""
      disabled={false}
      onChange={vi.fn()}
    />
  )
  expect(screen.getByLabelText("AI provider for Coach (model)")).toBeTruthy()
})
