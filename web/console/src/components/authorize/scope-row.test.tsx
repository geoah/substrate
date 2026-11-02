// @vitest-environment jsdom
/** A declare scope's sentence says the app will own the package's kinds
 * (decision 0205), and a scope the server refuses says why on its row. */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import type { AuthorizationScope } from "@/lib/api/types"

import { ScopeRow } from "./scope-row"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}))

afterEach(cleanup)

function scope(
  verb: AuthorizationScope["verb"],
  pattern: string,
  refused?: string
) {
  return {
    scope: `${verb}:${pattern}`,
    requested: `${verb}:${pattern}`,
    verb,
    write: verb === "write",
    pattern,
    glob: pattern.endsWith("*"),
    kinds: [],
    agents: [],
    ...(refused ? { refused } : {}),
  } satisfies AuthorizationScope
}

function mount(s: AuthorizationScope) {
  render(
    <ScopeRow
      scope={s}
      repository="ada.example.com"
      checked={!s.refused}
      disabled={Boolean(s.refused)}
      technical={false}
      onChange={() => {}}
    />
  )
}

it("says in the declare sentence that the app will own the kinds", () => {
  mount(scope("declare", "ada.example.com/training"))
  expect(
    screen.getByText(/The app will own these kinds: you can’t edit them/)
  ).toBeTruthy()
  expect(screen.queryByText(/Can’t be approved/)).toBeNull()
})

it("says nothing of ownership for a write scope", () => {
  mount(scope("write", "ada.example.com/training/*"))
  expect(screen.queryByText(/will own these kinds/)).toBeNull()
})

it("says why a refused declare scope can't be approved, unchecked", () => {
  mount(
    scope(
      "declare",
      "ada.example.com/training",
      "ada.example.com/training is managed by another app, https://workouts.example.org/client.json"
    )
  )
  expect(
    screen.getByText(/Can’t be approved: .*another app, https:\/\/workouts/)
  ).toBeTruthy()
  const box = screen.getByRole<HTMLInputElement>("checkbox")
  expect(box.checked).toBe(false)
  expect(box.disabled).toBe(true)
})
