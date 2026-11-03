// @vitest-environment jsdom
/** A package row's choices on the approval page: a setting by its label
 * with its id beside it, a secret kept blank and never filled by the
 * browser, a required field that says so and names itself once left empty,
 * and an optional add-on by its name, waiting for its provider. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import { PackageChoices } from "./package-choices"

afterEach(cleanup)

it("labels a setting by its words, keeps a secret blank, and distinguishes settings of the same id", () => {
  const onValues = vi.fn()
  render(
    <PackageChoices
      pkg={{
        id: "example.com/addon",
        required: 1,
        settings: [
          {
            id: "example.com/addon/apiKey",
            secret: true,
            required: true,
            description: "From the add-on's own site.",
          },
        ],
      }}
      waiting={false}
      selected
      disabled={false}
      onSelect={vi.fn()}
      values={[
        { id: "example.com/addon/apiKey", secret: false, value: "plain" },
      ]}
      onValues={onValues}
    />
  )
  const field = screen.getByLabelText<HTMLInputElement>("Api key")
  expect(field.type).toBe("password")
  expect(field.autocomplete).toBe("new-password")
  expect(field.value).toBe("")
  expect(field.getAttribute("aria-required")).toBe("true")
  expect(field.getAttribute("aria-invalid")).toBeNull()
  expect(screen.getByText("example.com/addon/apiKey")).toBeTruthy()
  expect(screen.getByText(/Required, kept sealed/)).toBeTruthy()
  expect(field.getAttribute("aria-describedby")).toBeTruthy()
  expect(screen.getByText("From the add-on's own site.").id).toBe(
    field.getAttribute("aria-describedby")
  )
  fireEvent.change(field, { target: { value: "typed" } })
  expect(onValues).toHaveBeenCalledWith([
    { id: "example.com/addon/apiKey", secret: false, value: "plain" },
    { id: "example.com/addon/apiKey", secret: true, value: "typed" },
  ])
})

it("names a required field left empty, and ties the message to it", () => {
  render(
    <PackageChoices
      pkg={{
        id: "example.com/addon",
        required: 1,
        settings: [
          { id: "example.com/addon/region", secret: false, required: true },
        ],
      }}
      waiting={false}
      selected
      disabled={false}
      onSelect={vi.fn()}
      values={[]}
      onValues={vi.fn()}
    />
  )
  const field = screen.getByLabelText<HTMLInputElement>("Region")
  expect(field.autocomplete).toBe("off")
  expect(screen.queryByText("Fill in region.")).toBeNull()
  fireEvent.blur(field)
  const error = screen.getByText("Fill in region.")
  expect(field.getAttribute("aria-invalid")).toBe("true")
  expect(field.getAttribute("aria-describedby")).toBe(error.id)
})

it("offers a waiting add-on as an optional choice, by name, with its id beside it", () => {
  const onSelect = vi.fn()
  render(
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
  )
  expect(screen.getByRole("checkbox", { name: /Include Addon/ })).toBeTruthy()
  expect(screen.getByText("example.com/addon")).toBeTruthy()
  expect(screen.getByText("Installs when you connect Provider.")).toBeTruthy()
  fireEvent.click(screen.getByRole("checkbox"))
  expect(onSelect).toHaveBeenCalledWith(false)
})
