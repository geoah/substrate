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
  Link: ({
    to,
    search,
    children,
  }: {
    to: string
    search?: { redirect?: string }
    children: React.ReactNode
  }) => (
    <a
      href={
        search?.redirect
          ? `${to}?redirect=${encodeURIComponent(search.redirect)}`
          : to
      }
    >
      {children}
    </a>
  ),
}))

/** The page's `?redirect=` and `?login_hint=`, as the route validated them. */
const loginSearch = vi.hoisted(() => ({
  redirect: undefined as string | undefined,
  login_hint: undefined as string | undefined,
}))
vi.mock("@/router", () => ({
  loginRoute: { useSearch: () => loginSearch },
}))

/** What GET /.well-known/substrate/server.json said about the door. A settled
 * answer is what every case wants, so the fetch assertions below stay about
 * the login call itself; discovery.test.ts covers the fetching. Whether a code
 * is asked for is the login's own answer, not the policy's. */
const policy = vi.hoisted(() => ({
  inviteRequired: true,
  totpRequired: true,
}))
vi.mock("@/lib/api/discovery", async (importOriginal) => {
  const real = await importOriginal<typeof import("@/lib/api/discovery")>()
  return { ...real, useAuthPolicy: () => policy }
})

/** The deployment's site (lib/site.ts): none unless a case sets one. */
const door = vi.hoisted(() => ({
  site: null as null | { name: string; tagline?: string; logo?: string },
}))
vi.mock("@/lib/site", async (importOriginal) => {
  const real = await importOriginal<typeof import("@/lib/site")>()
  return { ...real, useSite: () => door.site }
})

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

/** The substrate's answer to a right password on an account that has a second
 * factor and sent no code. */
const CODE_ASKED = {
  error: { code: "totp_required", message: "a code is required" },
}

function signIn() {
  fireEvent.change(screen.getByLabelText("Your address"), {
    target: { value: "geoah" },
  })
  fireEvent.change(screen.getByLabelText("Password"), {
    target: { value: "correct horse battery" },
  })
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }))
}

/** The second step: the code the substrate asked for, then Sign in again. */
async function enterCode(code = "123 456") {
  fireEvent.change(await screen.findByLabelText("One-time code"), {
    target: { value: code },
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
    door.site = null
    loginSearch.redirect = undefined
    loginSearch.login_hint = undefined
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("asks for the repository and password, and no code until the substrate does", () => {
    render(<LoginPage />)
    expect(screen.getByLabelText("Your address")).toBeTruthy()
    expect(screen.getByLabelText("Password")).toBeTruthy()
    expect(screen.queryByLabelText("One-time code")).toBeNull()
  })

  it("shows the deployment's brand where it sets a site", () => {
    door.site = {
      name: "Crane",
      tagline: "A home for your people",
      logo: "/site/logo.svg",
    }
    render(<LoginPage />)
    expect(screen.getByText("Crane")).toBeTruthy()
    expect(screen.getByText("Welcome back to Crane.")).toBeTruthy()
    expect(document.querySelector('img[src="/site/logo.svg"]')).not.toBeNull()
    expect(document.querySelector('img[src="/substrate.svg"]')).toBeNull()
  })

  it("says a branded deployment is a Substrate provider, and only then", () => {
    door.site = { name: "Crane" }
    render(<LoginPage />)
    expect(screen.getByText("A Substrate provider")).toBeTruthy()
    expect(
      screen.getByRole("heading", { name: "Sign in to your Substrate" })
    ).toBeTruthy()
    cleanup()
    door.site = null
    render(<LoginPage />)
    expect(screen.queryByText(/provider/i)).toBeNull()
    expect(
      screen.getByRole("heading", { name: "Sign in to your Substrate" })
    ).toBeTruthy()
  })

  it("keeps the substrate's brand where no site is set", () => {
    render(<LoginPage />)
    expect(screen.getByText("substrate")).toBeTruthy()
    expect(screen.getByText("Welcome back.")).toBeTruthy()
    expect(document.querySelector('img[src="/substrate.svg"]')).not.toBeNull()
  })

  it("fills the repository an app named in its login_hint", () => {
    loginSearch.login_hint = "ada.example.org"
    render(<LoginPage />)
    expect(screen.getByLabelText<HTMLInputElement>("Your address").value).toBe(
      "ada.example.org"
    )
  })

  it("logs in with the password alone where the account has no second factor", async () => {
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<LoginPage />)
    signIn()
    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    expect(getRepository()).toBe("geoah")
    expect(getTokenId()).toBe("tok-1")
    expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/login")
    const body = JSON.parse((init as RequestInit).body as string)
    expect(body).toEqual({
      repository: "geoah",
      password: "correct horse battery",
      totpCode: "",
      label: "console",
    })
    // The door is anonymous: no bearer rides a login.
    expect(
      ((init as RequestInit).headers as Record<string, string>).Authorization
    ).toBeUndefined()
  })

  it("asks for the code when the substrate does, then signs in with it", async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, CODE_ASKED))
      .mockResolvedValueOnce(jsonResponse(201, MINT))
    render(<LoginPage />)
    signIn()
    // Being asked is not a failure: no alert, and the field takes the focus.
    const field = await screen.findByLabelText("One-time code")
    await waitFor(() => expect(document.activeElement).toBe(field))
    expect(screen.queryByRole("alert")).toBeNull()
    expect(getToken()).toBeNull()

    await enterCode()
    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    const body = JSON.parse(
      (fetchMock.mock.calls[1][1] as RequestInit).body as string
    )
    expect(body).toEqual({
      repository: "geoah",
      password: "correct horse battery",
      totpCode: "123456",
      label: "console",
    })
  })

  it("returns to an app's approval page after signing in", async () => {
    loginSearch.redirect = "/authorize/AbC123"
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<LoginPage />)
    signIn()
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/authorize/AbC123",
        replace: true,
      })
    )
  })

  it.each(["//evil.example/authorize/x", "https://evil.example", "/\\evil"])(
    "lands on Home instead of the return path %s",
    async (redirect) => {
      loginSearch.redirect = redirect
      fetchMock.mockResolvedValue(jsonResponse(201, MINT))
      render(<LoginPage />)
      signIn()
      await waitFor(() =>
        expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
      )
    }
  )

  it("carries the return path to registration, and only a local one", () => {
    loginSearch.redirect = "/authorize/AbC123"
    render(<LoginPage />)
    expect(
      screen
        .getByRole("link", { name: "Create your Substrate" })
        .getAttribute("href")
    ).toBe("/register?redirect=%2Fauthorize%2FAbC123")
    cleanup()
    loginSearch.redirect = "//evil.example"
    render(<LoginPage />)
    expect(
      screen
        .getByRole("link", { name: "Create your Substrate" })
        .getAttribute("href")
    ).toBe("/register")
  })

  it("offers no registration inside an app from the App Store", () => {
    vi.stubGlobal("substrateShell", {
      platform: "ios",
      server: "https://substrate.example.com",
      appStore: true,
    })
    render(<LoginPage />)
    expect(
      screen.queryByRole("link", { name: "Create your Substrate" })
    ).toBeNull()
  })

  it("names the address and password on a refused first step", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(401, {
        error: { code: "auth", message: "invalid credentials" },
      })
    )
    render(<LoginPage />)
    signIn()
    await screen.findByText(/address or password is wrong/i)
    expect(screen.queryByLabelText("One-time code")).toBeNull()
    expect(getToken()).toBeNull()
    expect(navigate).not.toHaveBeenCalled()
  })

  it("names the code too once it was asked for, and clears it", async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, CODE_ASKED))
      .mockResolvedValueOnce(
        jsonResponse(401, {
          error: { code: "auth", message: "invalid credentials" },
        })
      )
    render(<LoginPage />)
    signIn()
    await enterCode()
    await screen.findByText(/address, password or code is wrong/i)
    expect(screen.getByLabelText<HTMLInputElement>("One-time code").value).toBe(
      ""
    )
    expect(getToken()).toBeNull()
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

  it("rejects a non-six-digit code before the second request", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(401, CODE_ASKED))
    render(<LoginPage />)
    signIn()
    await enterCode("12345")
    await screen.findByText("Enter the current 6-digit code.")
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
