import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  clearSession,
  getToken,
  getTokenId,
  getUsername,
} from "@/lib/api/session"

const navigate = vi.fn().mockResolvedValue(undefined)

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}))

/** What GET /.well-known/substrate/server.json said about the door; discovery.test.ts covers the fetching. */
const policy = vi.hoisted(() => ({ totpRequired: true }))
vi.mock("@/lib/api/discovery", () => ({ useAuthPolicy: () => policy }))

import { RegisterPage } from "./register"

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

const ENROLLMENT = {
  totpSecret: "SEED",
  otpauthUri: "otpauth://totp/geoah?secret=SEED",
}
const MINT = {
  token: { id: "tok-1", label: "console", createdAt: "2026-08-12T00:00:00Z" },
  secret: "substrate_tok_minted",
}

const PASSWORD = "correct horse battery"

/** Step one now collects the password too, BEFORE the QR is revealed — a
 * password manager saves the new login first, then attaches the OTP. */
async function enroll() {
  fireEvent.change(screen.getByLabelText("Invite code"), {
    target: { value: "INV-1" },
  })
  fireEvent.change(screen.getByLabelText("Username"), {
    target: { value: "geoah" },
  })
  fireEvent.change(screen.getByLabelText("Password"), {
    target: { value: PASSWORD },
  })
  fireEvent.change(screen.getByLabelText("Confirm password"), {
    target: { value: PASSWORD },
  })
  fireEvent.click(screen.getByRole("button", { name: "Continue" }))
  await screen.findByText("SEED")
}

describe("RegisterPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    navigate.mockClear()
    policy.totpRequired = true
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("collects the credentials first, and shows no one-time code until enrolled", () => {
    render(<RegisterPage />)
    expect(screen.getByLabelText("Invite code")).toBeTruthy()
    expect(screen.getByLabelText("Username")).toBeTruthy()
    // Password is on the FIRST step now, so a manager saves the login before
    // any QR appears.
    expect(screen.getByLabelText("Password")).toBeTruthy()
    expect(screen.getByLabelText("Confirm password")).toBeTruthy()
    // The authenticator secret and the code prove-it field come only after enroll.
    expect(screen.queryByLabelText("One-time code")).toBeNull()
    expect(screen.queryByText("SEED")).toBeNull()
  })

  it("enrolls only after the password is set, then reveals the secret and the code step", async () => {
    render(<RegisterPage />)
    // With no password, Continue cannot enroll.
    fireEvent.change(screen.getByLabelText("Invite code"), {
      target: { value: "INV-1" },
    })
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "geoah" },
    })
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled")
    ).toBe(true)

    fetchMock.mockResolvedValue(jsonResponse(200, ENROLLMENT))
    await enroll()

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/register/enroll")
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      inviteCode: "INV-1",
      username: "geoah",
    })
    expect(screen.getByText("SEED")).toBeTruthy()
    expect(screen.getByLabelText("One-time code")).toBeTruthy()
  })

  it("commits the second step and lands logged in", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, ENROLLMENT))
    render(<RegisterPage />)
    await enroll()

    fetchMock.mockResolvedValueOnce(jsonResponse(201, MINT))
    fireEvent.change(screen.getByLabelText("One-time code"), {
      target: { value: "123 456" },
    })
    fireEvent.click(
      screen.getByRole("button", { name: "Create my repository" })
    )

    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    expect(getUsername()).toBe("geoah")
    expect(getTokenId()).toBe("tok-1")
    expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    const [url, init] = fetchMock.mock.calls[1]
    expect(url).toBe("/register")
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      inviteCode: "INV-1",
      username: "geoah",
      password: PASSWORD,
      totpSecret: "SEED",
      totpCode: "123456",
      label: "console",
    })
  })

  it("registers in ONE step where no second factor is verified", async () => {
    policy.totpRequired = false
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<RegisterPage />)
    // No enrollment step at all: no QR, no seed, no code field, ever.
    fireEvent.change(screen.getByLabelText("Invite code"), {
      target: { value: "INV-1" },
    })
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "geoah" },
    })
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: PASSWORD },
    })
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: PASSWORD },
    })
    expect(screen.queryByLabelText("One-time code")).toBeNull()
    fireEvent.click(
      screen.getByRole("button", { name: "Create my repository" })
    )

    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    expect(fetchMock.mock.calls.length).toBe(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/register")
    // An empty seed asks the substrate to mint one; an empty code is what a
    // door that verifies none expects.
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      inviteCode: "INV-1",
      username: "geoah",
      password: PASSWORD,
      totpSecret: "",
      totpCode: "",
      label: "console",
    })
  })

  it("explains a closed door when no invite code is configured", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(501, {
        error: { code: "unsupported", message: "registration disabled" },
      })
    )
    render(<RegisterPage />)
    fireEvent.change(screen.getByLabelText("Invite code"), {
      target: { value: "INV-1" },
    })
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "geoah" },
    })
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: PASSWORD },
    })
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: PASSWORD },
    })
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    await screen.findByText(/Registration is closed/i)
    expect(getToken()).toBeNull()
  })
})
