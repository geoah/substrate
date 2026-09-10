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

vi.mock("@/router", () => ({
  loginRoute: { useSearch: () => ({ redirect: undefined }) },
}))

/** What GET /.well-known/substrate/server.json said about the door. A settled
 * answer is what most cases want, so the fetch assertions below stay about the
 * login call itself; discovery.test.ts covers the fetching.
 *
 * `policy.live` hands the hook back to the real module for the last describe,
 * which is about the one render the stub cannot show: the policy landing after
 * the first paint, with whatever the reader already typed in the fields. */
const policy = vi.hoisted(() => ({ totpRequired: true, live: false }))
vi.mock("@/lib/api/discovery", async (importOriginal) => {
  const real = await importOriginal<typeof import("@/lib/api/discovery")>()
  return {
    ...real,
    useAuthPolicy: () => (policy.live ? real.useAuthPolicy() : policy),
  }
})

import { resetAuthPolicy } from "@/lib/api/discovery"

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
  fireEvent.change(screen.getByLabelText("Repository"), {
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
    policy.totpRequired = true
    policy.live = false
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("asks for the repository, password and the one-time code", () => {
    render(<LoginPage />)
    expect(screen.getByLabelText("Repository")).toBeTruthy()
    expect(screen.getByLabelText("Password")).toBeTruthy()
    expect(screen.getByLabelText("One-time code")).toBeTruthy()
  })

  it("logs in and stores the minted token, repository and token id", async () => {
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<LoginPage />)
    signIn()
    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    expect(getRepository()).toBe("geoah")
    expect(getTokenId()).toBe("tok-1")
    expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/login")
    const body = JSON.parse((init as RequestInit).body as string)
    expect(body).toEqual({
      repository: "geoah",
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
    await screen.findByText(/repository, password or code is wrong/i)
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

  it("asks for no code where the substrate verifies none", async () => {
    policy.totpRequired = false
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<LoginPage />)
    expect(screen.queryByLabelText("One-time code")).toBeNull()
    fireEvent.change(screen.getByLabelText("Repository"), {
      target: { value: "geoah" },
    })
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "correct horse battery" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }))
    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/login")
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      repository: "geoah",
      password: "correct horse battery",
      totpCode: "",
      label: "console",
    })
  })

  it("rejects a non-six-digit code before any request", async () => {
    render(<LoginPage />)
    fireEvent.change(screen.getByLabelText("Repository"), {
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

/** The same page against the REAL discovery hook: the policy lands one render
 * after the first paint, and whatever the reader (or their password manager)
 * put in the fields before then has to survive it. */
describe("LoginPage and the door's own answer", () => {
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
    fireEvent.change(screen.getByLabelText("Repository"), {
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
    expect(
      (screen.getByLabelText("Repository") as HTMLInputElement).value
    ).toBe("geoah")
    expect((screen.getByLabelText("Password") as HTMLInputElement).value).toBe(
      "correct horse battery"
    )

    fireEvent.click(screen.getByRole("button", { name: "Sign in" }))
    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    const call = fetchMock.mock.calls.find(([url]) => String(url) === "/login")!
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({
      repository: "geoah",
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
    fireEvent.change(screen.getByLabelText("Repository"), {
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
