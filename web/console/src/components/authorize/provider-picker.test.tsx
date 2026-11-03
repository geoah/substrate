// @vitest-environment jsdom
import { render, screen, fireEvent } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { ProviderPicker } from "./provider-picker"

it("shows the model and daily cap and sends the owner's choice", () => {
  const onChange = vi.fn()
  render(
    <ProviderPicker
      input={{
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
      }}
      value="one"
      disabled={false}
      onChange={onChange}
    />
  )
  expect(screen.getByText(/daily cap 100 US cents/).textContent).toContain(
    "gateway/a"
  )
  expect(screen.getByRole<HTMLSelectElement>("combobox").value).toBe("one")
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "" } })
  expect(onChange).toHaveBeenCalledWith("")
})
