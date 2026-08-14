/** Registration against the REAL discovery hook: the reader (or a password
 * manager) can complete and submit the first step before
 * `GET /.well-known/substrate/server.json` answers,
 * and the answer decides whether there is a second step at all. */

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

import { RegisterPage } from "./register"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status })
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

function fillFirstStep() {
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
}

describe("RegisterPage and the door's own answer", () => {
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

  it("drops an enrollment bought before discovery said there is no second factor", async () => {
    let releaseDiscovery: (() => void) | undefined
    const discovered = new Promise<void>((resolve) => {
      releaseDiscovery = resolve
    })
    fetchMock.mockImplementation(async (input) => {
      const url = String(input)
      if (url === "/.well-known/substrate/server.json") {
        await discovered
        return jsonResponse(200, { registration: { totpRequired: false } })
      }
      if (url === "/register/enroll") return jsonResponse(200, ENROLLMENT)
      return jsonResponse(201, MINT)
    })

    render(<RegisterPage />)
    fillFirstStep()
    // Submitted under the strict default, because nothing has answered yet.
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    await screen.findByText("SEED")

    releaseDiscovery!()
    // The answer retires the whole second step: no seed on screen, no code to
    // prove, and the button is the commit.
    await waitFor(() => expect(screen.queryByText("SEED")).toBeNull())
    expect(screen.queryByLabelText("One-time code")).toBeNull()

    fireEvent.click(
      screen.getByRole("button", { name: "Create my repository" })
    )
    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    const call = fetchMock.mock.calls.find(
      ([url]) => String(url) === "/register"
    )!
    // An empty seed asks the substrate to mint the one it seals: the seed the
    // abandoned enrollment handed out is not smuggled into the commit.
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({
      inviteCode: "INV-1",
      username: "geoah",
      password: PASSWORD,
      totpSecret: "",
      totpCode: "",
      label: "console",
    })
  })

  it("keeps the enrollment step where discovery never answers", async () => {
    fetchMock.mockImplementation(async (input) => {
      const url = String(input)
      if (url === "/.well-known/substrate/server.json")
        throw new TypeError("offline")
      if (url === "/register/enroll") return jsonResponse(200, ENROLLMENT)
      return jsonResponse(201, MINT)
    })
    render(<RegisterPage />)
    fillFirstStep()
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    await screen.findByText("SEED")
    expect(screen.getByLabelText("One-time code")).toBeTruthy()
  })
})
