/** The login page against the REAL discovery hook, which is what makes this
 * worth its own file: the policy lands one render after the first paint, and
 * whatever the reader (or their password manager) put in the fields before
 * then has to survive it. */

import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { resetAuthPolicy } from "@/lib/api/discovery"
import { clearSession, getToken } from "@/lib/api/session"

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

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status })
}

const MINT = {
  token: { id: "tok-1", label: "console", createdAt: "2026-08-12T00:00:00Z" },
  secret: "substrate_tok_minted",
}

describe("LoginPage and the door's own answer", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    navigate.mockClear()
    resetAuthPolicy()
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("keeps what was typed before the policy landed, and asks for no code after", async () => {
    let releaseDiscovery: (() => void) | undefined
    const discovered = new Promise<void>((resolve) => {
      releaseDiscovery = resolve
    })
    fetchMock.mockImplementation(async (input) => {
      if (String(input) === "/.well-known/substrate/server.json") {
        await discovered
        return jsonResponse(200, { registration: { totpRequired: false } })
      }
      return jsonResponse(201, MINT)
    })

    render(<LoginPage />)
    // Discovery has not answered yet: the strict door is what renders, so a
    // deployment that DOES want a code never hides the field.
    expect(screen.getByLabelText("One-time code")).toBeTruthy()
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "geoah" },
    })
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "correct horse battery" },
    })

    releaseDiscovery!()
    await waitFor(() =>
      expect(screen.queryByLabelText("One-time code")).toBeNull()
    )
    // The two fields survived the answer — a password manager fills them the
    // moment the page paints, and nothing here may throw that away.
    expect((screen.getByLabelText("Username") as HTMLInputElement).value).toBe(
      "geoah"
    )
    expect((screen.getByLabelText("Password") as HTMLInputElement).value).toBe(
      "correct horse battery"
    )

    fireEvent.click(screen.getByRole("button", { name: "Sign in" }))
    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    const call = fetchMock.mock.calls.find(([url]) => String(url) === "/login")!
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({
      username: "geoah",
      password: "correct horse battery",
      totpCode: "",
      label: "console",
    })
  })

  it("still refuses to sign in without a code where discovery never answers", async () => {
    fetchMock.mockImplementation(async (input) => {
      if (String(input) === "/.well-known/substrate/server.json")
        throw new TypeError("offline")
      return jsonResponse(201, MINT)
    })
    render(<LoginPage />)
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "geoah" },
    })
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "correct horse battery" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }))
    await screen.findByText("Enter the current 6-digit code.")
    expect(fetchMock.mock.calls.some(([url]) => String(url) === "/login")).toBe(
      false
    )
  })
})
