// @vitest-environment jsdom
/** An app that names another repository than the one signed in is told
 * apart before anything is approved, and the way out signs out. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import { HintMismatch } from "./hint-mismatch"

const navigate = vi.fn()
const logout = vi.fn(async () => undefined)

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }))
vi.mock("@/lib/api/auth", () => ({ logout: () => logout() }))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

it("names both repositories and signs out toward the one the app asked for", async () => {
  render(<HintMismatch hint="ada.example.com" repository="bob.example.com" />)
  expect(screen.getByText(/asked to connect to/).textContent).toContain(
    "ada.example.com"
  )
  expect(
    screen.getByText(/Approving here connects it to bob\.example\.com/)
  ).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Sign out" }))
  await vi.waitFor(() => expect(navigate).toHaveBeenCalled())
  expect(logout).toHaveBeenCalledOnce()
  expect(navigate).toHaveBeenCalledWith({
    to: "/login",
    search: { login_hint: "ada.example.com" },
    replace: true,
  })
})
