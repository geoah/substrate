// @vitest-environment jsdom
/** A declare scope says on the approval page that the app will own the
 * package's kinds (decision 0205), and no other scope says it. */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import type { AuthorizationScope } from "@/lib/api/types"
import { DECLARE_OWNERSHIP_NOTE } from "@/lib/app-scopes"

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

function mount(s: AuthorizationScope) {
  render(
    <ScopeRow
      scope={s}
      repository="ada.example.com"
      checked
      disabled={false}
      technical={false}
      onChange={() => {}}
    />
  )
}

it("says the app will own the kinds a declare scope installs", () => {
  mount(scope("declare", "ada.example.com/training"))
  expect(screen.getByText(DECLARE_OWNERSHIP_NOTE)).toBeTruthy()
})

it("says nothing of ownership for a write scope", () => {
  mount(scope("write", "ada.example.com/training/*"))
  expect(screen.queryByText(DECLARE_OWNERSHIP_NOTE)).toBeNull()
})
