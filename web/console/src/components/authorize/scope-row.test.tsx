// @vitest-environment jsdom
/** A scope row says what the scope lets the app do, and the technical view
 * of an agents scope says the app reaches an agent only when the rest of its
 * access covers what that agent reads (decision 0214). */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import type { AuthorizationScope } from "@/lib/api/types"

import { ScopeRow } from "./scope-row"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}))

afterEach(cleanup)

function scope(verb: AuthorizationScope["verb"], pattern: string) {
  return {
    scope: `${verb}:${pattern}`,
    requested: `${verb}:${pattern}`,
    verb,
    write: verb === "write",
    pattern,
    glob: pattern.endsWith("*"),
    kinds: [],
    agents: [],
  } satisfies AuthorizationScope
}

function mount(s: AuthorizationScope, technical = false) {
  render(
    <ScopeRow
      scope={s}
      repository="ada.example.com"
      checked
      disabled={false}
      technical={technical}
      onChange={() => {}}
    />
  )
}

it("starts every scope checked and says nothing of ownership", () => {
  mount(scope("write", "ada.example.com/training/*"))
  const box = screen.getByRole<HTMLInputElement>("checkbox")
  expect(box.checked).toBe(true)
  expect(box.disabled).toBe(false)
  expect(screen.queryByText(/will own these kinds/)).toBeNull()
  expect(screen.queryByText(/Can’t be approved/)).toBeNull()
})

it("says in the technical view that an agents scope reaches only agents the rest of its access covers", () => {
  mount(scope("agents", "*"), true)
  expect(
    screen.getByText(
      /reaches an agent only when the rest of its access covers everything that agent reads/
    )
  ).toBeTruthy()
  expect(screen.getByText("Covers today")).toBeTruthy()
})
