import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { clearSession, getToken, getTokenId, getUsername } from "@/lib/api/session"

const navigate = vi.fn().mockResolvedValue(undefined)

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}))

vi.mock("@/router", () => ({
  loginRoute: { useSearch: () => ({ redirect: undefined }) },
}))

import { LoginPage } from "./login"

function jsonResponse(
  status: number,
  body: unknown,
  headers: Record<string, string> = {}
): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers,
  })
}

const MINT = {
  token: { id: "tok-1", label: "console", createdAt: "2026-08-12T00:00:00Z" },
  secret: "substrate_tok_minted",
}

function signIn() {
  fireEvent.change(screen.getByLabelText("Username"), {
    target: { value: "geoah" },
  })
  fireEvent.change(screen.getByLabelText("Password"), {
    target: { value: "correct horse battery" },
  })
  fireEvent.change(screen.getByLabelText("One-time code"), {
    target: { value: "123 456" },
  })
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }))
}

describe("LoginPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    navigate.mockClear()
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("asks for username, password and the one-time code", () => {
    render(<LoginPage />)
    expect(screen.getByLabelText("Username")).toBeTruthy()
    expect(screen.getByLabelText("Password")).toBeTruthy()
    expect(screen.getByLabelText("One-time code")).toBeTruthy()
  })

  it("logs in and stores the minted token, username and token id", async () => {
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<LoginPage />)
    signIn()
    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    expect(getUsername()).toBe("geoah")
    expect(getTokenId()).toBe("tok-1")
    expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/login")
    const body = JSON.parse((init as RequestInit).body as string)
    expect(body).toEqual({
      username: "geoah",
      password: "correct horse battery",
      totpCode: "123456",
      label: "console",
    })
    // The door is anonymous: no bearer rides a login.
    expect(
      ((init as RequestInit).headers as Record<string, string>).Authorization
    ).toBeUndefined()
  })

  it("shows the factor/lockout message on a refused sign-in", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(401, {
        error: { code: "auth", message: "invalid credentials" },
      })
    )
    render(<LoginPage />)
    signIn()
    await screen.findByText(/username, password or code is wrong/i)
    expect(getToken()).toBeNull()
    expect(navigate).not.toHaveBeenCalled()
  })

  it("shows the retry hint on a rate limit", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(
        429,
        { error: { code: "rate_limited", message: "too many attempts" } },
        { "Retry-After": "5" }
      )
    )
    render(<LoginPage />)
    signIn()
    await screen.findByText(/try again in 5s/i)
    expect(getToken()).toBeNull()
  })

  it("rejects a non-six-digit code before any request", async () => {
    render(<LoginPage />)
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "geoah" },
    })
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "correct horse battery" },
    })
    fireEvent.change(screen.getByLabelText("One-time code"), {
      target: { value: "12345" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }))
    await screen.findByText("Enter the current 6-digit code.")
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
