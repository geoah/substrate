// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { PackageChoices } from "./package-choices"

afterEach(cleanup)

it("keeps secret inputs blank and distinguishes settings of the same id", () => {
  const onValues = vi.fn()
  render(
    <ul>
      <PackageChoices
        pkg={{
          id: "example.com/addon",
          required: 1,
          settings: [
            { id: "example.com/addon/key", secret: true, required: true },
          ],
        }}
        waiting={false}
        selected
        disabled={false}
        onSelect={vi.fn()}
        values={[
          { id: "example.com/addon/key", secret: false, value: "plain" },
        ]}
        onValues={onValues}
      />
    </ul>
  )
  const field = screen.getByLabelText<HTMLInputElement>(
    "example.com/addon/key (required)"
  )
  expect(field.type).toBe("password")
  expect(field.value).toBe("")
  fireEvent.change(field, { target: { value: "typed" } })
  expect(onValues).toHaveBeenCalledWith([
    { id: "example.com/addon/key", secret: false, value: "plain" },
    { id: "example.com/addon/key", secret: true, value: "typed" },
  ])
})
it("offers a waiting add-on as an optional choice", () => {
  const onSelect = vi.fn()
  render(
    <ul>
      <PackageChoices
        pkg={{
          id: "example.com/addon",
          required: 1,
          addonFor: "example.com/provider",
        }}
        waiting
        selected
        disabled={false}
        onSelect={onSelect}
        values={[]}
        onValues={vi.fn()}
      />
    </ul>
  )
  expect(
    screen.getByText("Installs when you connect example.com/provider.")
  ).toBeTruthy()
  fireEvent.click(screen.getByRole("checkbox"))
  expect(onSelect).toHaveBeenCalledWith(false)
})
