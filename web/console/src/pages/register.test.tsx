// @vitest-environment jsdom
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
  getRepository,
} from "@/lib/api/session"

const navigate = vi.fn().mockResolvedValue(undefined)

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}))

/** What GET /.well-known/substrate/server.json said about the door;
 * discovery.test.ts covers the fetching. `policy.live` hands the hook back to
 * the real module for the last describe, which is about the answer arriving
 * after the reader has already submitted the first step. */
const policy = vi.hoisted(() => ({ totpRequired: true, live: false }))
vi.mock("@/lib/api/discovery", async (importOriginal) => {
  const real = await importOriginal<typeof import("@/lib/api/discovery")>()
  return {
    ...real,
    useAuthPolicy: () => (policy.live ? real.useAuthPolicy() : policy),
  }
})

import { resetAuthPolicy } from "@/lib/api/discovery"

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
  repository: "geoah.localhost",
}

const PASSWORD = "correct horse battery"

/** Step one now collects the password too, BEFORE the QR is revealed — a
 * password manager saves the new login first, then attaches the OTP. */
async function enroll() {
  fireEvent.change(screen.getByLabelText("Invite code"), {
    target: { value: "INV-1" },
  })
  fireEvent.change(screen.getByLabelText("Repository"), {
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
    policy.live = false
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("collects the credentials first, and shows no one-time code until enrolled", () => {
    render(<RegisterPage />)
    expect(screen.getByLabelText("Invite code")).toBeTruthy()
    expect(screen.getByLabelText("Repository")).toBeTruthy()
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
    fireEvent.change(screen.getByLabelText("Repository"), {
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
      repository: "geoah.localhost",
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
    expect(getRepository()).toBe("geoah.localhost")
    expect(getTokenId()).toBe("tok-1")
    expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    const [url, init] = fetchMock.mock.calls[1]
    expect(url).toBe("/register")
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      inviteCode: "INV-1",
      repository: "geoah.localhost",
      password: PASSWORD,
      totpSecret: "SEED",
      totpCode: "123456",
      label: "console",
    })
  })

  it("holds the reader on the recovery key", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, ENROLLMENT))
    render(<RegisterPage />)
    await enroll()

    fetchMock.mockResolvedValueOnce(
      jsonResponse(201, {
        ...MINT,
        recoveryKey: "AGE-SECRET-KEY-1TEST",
        recoveryPublicKey: "age1test",
      })
    )
    fireEvent.change(screen.getByLabelText("One-time code"), {
      target: { value: "123456" },
    })
    fireEvent.click(
      screen.getByRole("button", { name: "Create my repository" })
    )

    // Logged in, but NOT navigated: the recovery key arrives only on this
    // response, and the page holds the reader until they carry it off.
    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    const recovery = (await screen.findByLabelText(
      "Recovery key"
    )) as HTMLInputElement
    expect(recovery.value).toBe("AGE-SECRET-KEY-1TEST")
    expect(navigate).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole("button", { name: /I saved it/ }))
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    )
  })

  it("registers in ONE step where no second factor is verified", async () => {
    policy.totpRequired = false
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<RegisterPage />)
    // No enrollment step at all: no QR, no seed, no code field, ever.
    fireEvent.change(screen.getByLabelText("Invite code"), {
      target: { value: "INV-1" },
    })
    fireEvent.change(screen.getByLabelText("Repository"), {
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
      repository: "geoah.localhost",
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
    fireEvent.change(screen.getByLabelText("Repository"), {
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

/** The same page against the REAL discovery hook: the reader (or a password
 * manager) can complete and submit the first step before
 * `GET /.well-known/substrate/server.json` answers, and the answer decides
 * whether there is a second step at all. */
describe("RegisterPage and the door's own answer", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    navigate.mockClear()
    policy.live = true
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
    fireEvent.change(screen.getByLabelText("Invite code"), {
      target: { value: "INV-1" },
    })
    fireEvent.change(screen.getByLabelText("Repository"), {
      target: { value: "geoah" },
    })
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: PASSWORD },
    })
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: PASSWORD },
    })
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
    // The bare label the reader typed, completed under this console's host
    // (jsdom serves from `localhost`), because the reader left it derived.
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({
      inviteCode: "INV-1",
      repository: "geoah.localhost",
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
    fireEvent.change(screen.getByLabelText("Invite code"), {
      target: { value: "INV-1" },
    })
    fireEvent.change(screen.getByLabelText("Repository"), {
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
    expect(screen.getByLabelText("One-time code")).toBeTruthy()
  })
})
