// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
  type Mock,
} from "vitest"

import {
  OWNER_SETTING_PATH,
  fakeOwnerServer,
} from "@/lib/api/owner-contact.test-fixtures"
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

/** The page's `?redirect=`, as the route validated it. */
const registerSearch = vi.hoisted(() => ({
  redirect: undefined as string | undefined,
}))
vi.mock("@/router", () => ({
  registerRoute: { useSearch: () => registerSearch },
}))

/** What GET /.well-known/substrate/server.json said about the door;
 * discovery.test.ts covers the fetching. `policy.live` hands the hook back to
 * the real module for the last describe, which is about the answer arriving
 * after the reader has already submitted the first step. */
const policy = vi.hoisted(() => ({
  inviteRequired: true,
  totpRequired: true,
  /** The host the server completes a short name under: jsdom's own. */
  nameHost: "localhost",
  live: false,
  /** The features server.json lists. */
  features: [] as string[],
}))
vi.mock("@/lib/api/discovery", async (importOriginal) => {
  const real = await importOriginal<typeof import("@/lib/api/discovery")>()
  return {
    ...real,
    useAuthPolicy: () => (policy.live ? real.useAuthPolicy() : policy),
    useDoorFeature: (name: string) =>
      policy.live ? real.useDoorFeature(name) : policy.features.includes(name),
  }
})

/** The deployment's site (lib/site.ts): none unless a case sets one. */
const door = vi.hoisted(() => ({
  site: null as null | { name: string; tagline?: string; logo?: string },
}))
vi.mock("@/lib/site", async (importOriginal) => {
  const real = await importOriginal<typeof import("@/lib/site")>()
  return { ...real, useSite: () => door.site }
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
  fireEvent.change(screen.getByLabelText("Choose your address"), {
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

/** The name step comes before landing and has no skip, so a test about where
 * registration lands types a name and continues. The contact and setting
 * writes go to a fake owner server; every other request keeps the answer the
 * test gave. */
async function giveName(fetchMock: Mock<typeof fetch>) {
  const own = fetchMock.getMockImplementation()
  const owner = fakeOwnerServer()
  fetchMock.mockImplementation((input, init) => {
    const url =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.href
          : input.url
    if (url.startsWith("/api/v1/")) return owner.impl(input, init)
    if (own) return own(input, init)
    return Promise.reject(new Error(`unexpected request ${url}`))
  })
  fireEvent.change(await screen.findByLabelText("Your name"), {
    target: { value: "Ada" },
  })
  fireEvent.click(screen.getByRole("button", { name: "Continue" }))
}

describe("RegisterPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    navigate.mockClear()
    policy.inviteRequired = true
    policy.totpRequired = true
    policy.nameHost = "localhost"
    policy.live = false
    policy.features = []
    door.site = null
    registerSearch.redirect = undefined
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("collects the credentials first, and shows no one-time code until enrolled", async () => {
    render(<RegisterPage />)
    expect(screen.getByLabelText("Invite code")).toBeTruthy()
    expect(screen.getByLabelText("Choose your address")).toBeTruthy()
    // Password is on the FIRST step now, so a manager saves the login before
    // any QR appears.
    expect(screen.getByLabelText("Password")).toBeTruthy()
    expect(screen.getByLabelText("Confirm password")).toBeTruthy()
    // The authenticator secret and the code prove-it field come only after enroll.
    expect(screen.queryByLabelText("One-time code")).toBeNull()
    expect(screen.queryByText("SEED")).toBeNull()
  })

  it("shows the full address the name becomes, as it is typed", async () => {
    render(<RegisterPage />)
    const name = screen.getByLabelText("Choose your address")
    expect(
      screen.getByText(/Your Substrate’s address and sign-in name/)
    ).toBeTruthy()
    fireEvent.change(name, { target: { value: "Mum" } })
    // jsdom serves the console from localhost, so a bare name lands there.
    expect(screen.getByText("mum.localhost")).toBeTruthy()
    expect(screen.getByText(".localhost")).toBeTruthy()
    // The server's host stays fixed: a domain is the advanced option's, so
    // typing one here claims nothing.
    fireEvent.change(name, { target: { value: "ada.example.org" } })
    expect(screen.getByText(".localhost")).toBeTruthy()
    expect(screen.queryByText("ada.example.org")).toBeNull()
    expect(
      screen.getByText("To use your own domain, choose Advanced below.")
    ).toBeTruthy()
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Continue" })
        .disabled
    ).toBe(true)
    fireEvent.change(name, { target: { value: "-ada" } })
    expect(
      screen.getByText(
        "Use letters, numbers and hyphens, starting and ending with a letter or number."
      )
    ).toBeTruthy()
  })

  it("asks for the whole name where the server completes no short one", async () => {
    // Discovery answered, naming no host: there is nothing to complete a
    // label under, so the page never offers one.
    policy.nameHost = ""
    render(<RegisterPage />)
    fireEvent.change(await screen.findByLabelText("Your domain"), {
      target: { value: "ada.example.org" },
    })
    expect(screen.queryByLabelText("Choose your address")).toBeNull()
    expect(screen.queryByText(".localhost")).toBeNull()
    expect(screen.getByText("ada.example.org")).toBeTruthy()
  })

  it("takes the reader's own domain only through the advanced option, and says what owning it means", async () => {
    render(<RegisterPage />)
    expect(screen.queryByText(/Only use a domain you own/)).toBeNull()
    fireEvent.change(screen.getByLabelText("Choose your address"), {
      target: { value: "Ada.Example.org" },
    })
    const advanced = screen.getByRole("button", {
      name: "Advanced: use your own domain",
    })
    expect(advanced.getAttribute("aria-expanded")).toBe("false")
    fireEvent.click(advanced)

    // The pasted domain carries over, and the suffix is gone.
    const domain = screen.getByLabelText<HTMLInputElement>("Your domain")
    expect(domain.value).toBe("ada.example.org")
    expect(screen.queryByText(".localhost")).toBeNull()
    expect(screen.getByText("ada.example.org")).toBeTruthy()
    expect(screen.getByText(/Only use a domain you own/)).toBeTruthy()
    // The record Settings will ask for, in its shape: no proof yet.
    expect(screen.getByText("_substrate.ada.example.org")).toBeTruthy()
    expect(screen.getByText("TXT")).toBeTruthy()
    expect(
      screen.getByText("v=substrate1 server=http://localhost:3000 proof=…")
    ).toBeTruthy()

    // Half a domain is not an error until the reader leaves the field.
    fireEvent.change(domain, { target: { value: "ada" } })
    expect(screen.queryByText(/Enter a full domain/)).toBeNull()
    fireEvent.blur(domain)
    expect(
      screen.getByText("Enter a full domain, like home.example.org.")
    ).toBeTruthy()

    // Back to this server's address: a domain does not cross back.
    fireEvent.click(
      screen.getByRole("button", {
        name: "Use an address on localhost instead",
      })
    )
    expect(
      screen.getByLabelText<HTMLInputElement>("Choose your address").value
    ).toBe("ada")
    expect(screen.queryByText(/Only use a domain you own/)).toBeNull()
  })

  it("registers the reader's own domain and lands on Settings' Verify domain", async () => {
    policy.totpRequired = false
    fetchMock.mockResolvedValue(
      jsonResponse(201, { ...MINT, repository: "ada.example.org" })
    )
    render(<RegisterPage />)
    fireEvent.change(screen.getByLabelText("Invite code"), {
      target: { value: "INV-1" },
    })
    fireEvent.click(
      screen.getByRole("button", { name: "Advanced: use your own domain" })
    )
    fireEvent.change(screen.getByLabelText("Your domain"), {
      target: { value: "ada.example.org." },
    })
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: PASSWORD },
    })
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: PASSWORD },
    })
    fireEvent.click(
      screen.getByRole("button", { name: "Create your Substrate" })
    )
    await giveName(fetchMock)

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/settings",
        hash: "verify-domain",
        replace: true,
      })
    )
    const [, init] = fetchMock.mock.calls[0]
    expect(JSON.parse((init as RequestInit).body as string).identity).toBe(
      "ada.example.org"
    )
    expect(getRepository()).toBe("ada.example.org")
  })

  it("returns to an app's approval page before Verify domain", async () => {
    registerSearch.redirect = "/authorize/AbC123"
    policy.totpRequired = false
    fetchMock.mockResolvedValue(
      jsonResponse(201, { ...MINT, repository: "ada.example.org" })
    )
    render(<RegisterPage />)
    fireEvent.change(screen.getByLabelText("Invite code"), {
      target: { value: "INV-1" },
    })
    fireEvent.click(
      screen.getByRole("button", { name: "Advanced: use your own domain" })
    )
    fireEvent.change(screen.getByLabelText("Your domain"), {
      target: { value: "ada.example.org." },
    })
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: PASSWORD },
    })
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: PASSWORD },
    })
    fireEvent.click(
      screen.getByRole("button", { name: "Create your Substrate" })
    )
    await giveName(fetchMock)

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/authorize/AbC123",
        replace: true,
      })
    )
  })

  it("shows the deployment's brand, or the substrate's without a site", async () => {
    door.site = { name: "Crane", logo: "/site/logo.svg" }
    render(<RegisterPage />)
    expect(screen.getByText("Crane")).toBeTruthy()
    expect(screen.getByText("A Substrate provider")).toBeTruthy()
    expect(
      screen.getByRole("heading", { name: "Create your Substrate" })
    ).toBeTruthy()
    expect(
      screen.getByText("Choose your address on Crane and a password.")
    ).toBeTruthy()
    expect(document.querySelector('img[src="/site/logo.svg"]')).not.toBeNull()
    cleanup()
    door.site = null
    render(<RegisterPage />)
    expect(screen.getByText("substrate")).toBeTruthy()
    expect(screen.queryByText(/provider/i)).toBeNull()
    expect(document.querySelector('img[src="/substrate.svg"]')).not.toBeNull()
  })

  it("enrolls only after the password is set, then reveals the secret and the code step", async () => {
    render(<RegisterPage />)
    // With no password, Continue cannot enroll.
    fireEvent.change(screen.getByLabelText("Invite code"), {
      target: { value: "INV-1" },
    })
    fireEvent.change(screen.getByLabelText("Choose your address"), {
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
      identity: "geoah.localhost",
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
      screen.getByRole("button", { name: "Create your Substrate" })
    )

    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    expect(getRepository()).toBe("geoah.localhost")
    expect(getTokenId()).toBe("tok-1")
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    )
    const [url, init] = fetchMock.mock.calls[1]
    expect(url).toBe("/register")
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      inviteCode: "INV-1",
      identity: "geoah.localhost",
      password: PASSWORD,
      totpSecret: "SEED",
      totpCode: "123456",
      label: "console",
    })
  })

  it("skips the second factor from the authenticator step, sending no seed", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, ENROLLMENT))
    render(<RegisterPage />)
    await enroll()
    // A half-typed code does not ride the skip.
    fireEvent.change(screen.getByLabelText("One-time code"), {
      target: { value: "123" },
    })

    fetchMock.mockResolvedValueOnce(jsonResponse(201, MINT))
    fireEvent.click(
      screen.getByRole("button", { name: "Skip two-factor for now" })
    )
    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    const [url, init] = fetchMock.mock.calls[1]
    expect(url).toBe("/register")
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      inviteCode: "INV-1",
      identity: "geoah.localhost",
      password: PASSWORD,
      totpSecret: "",
      totpCode: "",
      skipTotp: true,
      label: "console",
    })
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    )
  })

  it("returns to an app's approval page once registered, and the sign-in link carries it", async () => {
    registerSearch.redirect = "/authorize/AbC123"
    fetchMock.mockResolvedValueOnce(jsonResponse(200, ENROLLMENT))
    render(<RegisterPage />)
    expect(
      screen.getByRole("link", { name: "Sign in" }).getAttribute("href")
    ).toBe("/login?redirect=%2Fauthorize%2FAbC123")
    await enroll()

    fetchMock.mockResolvedValueOnce(jsonResponse(201, MINT))
    fireEvent.change(screen.getByLabelText("One-time code"), {
      target: { value: "123456" },
    })
    fireEvent.click(
      screen.getByRole("button", { name: "Create your Substrate" })
    )
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/authorize/AbC123",
        replace: true,
      })
    )
  })

  it("lands on Home when the return path leaves the console", async () => {
    registerSearch.redirect = "//evil.example"
    fetchMock.mockResolvedValueOnce(jsonResponse(200, ENROLLMENT))
    render(<RegisterPage />)
    expect(
      screen.getByRole("link", { name: "Sign in" }).getAttribute("href")
    ).toBe("/login")
    await enroll()
    fetchMock.mockResolvedValueOnce(jsonResponse(201, MINT))
    fireEvent.change(screen.getByLabelText("One-time code"), {
      target: { value: "123456" },
    })
    fireEvent.click(
      screen.getByRole("button", { name: "Create your Substrate" })
    )
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    )
  })

  it("has no key step: the commit sends no recovery recipient and goes straight on to the name", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, ENROLLMENT))
    render(<RegisterPage />)
    await enroll()

    fetchMock.mockResolvedValueOnce(jsonResponse(201, MINT))
    fireEvent.change(screen.getByLabelText("One-time code"), {
      target: { value: "123456" },
    })
    fireEvent.click(
      screen.getByRole("button", { name: "Create your Substrate" })
    )

    // The screen after the commit is the name step: there is no key to keep
    // first, and nothing on any step says there is one.
    await screen.findByRole("heading", { name: "What should we call you?" })
    const sent = JSON.parse(
      (fetchMock.mock.calls[1][1] as RequestInit).body as string
    )
    expect(sent).not.toHaveProperty("recoveryPublicKey")
    expect(screen.queryByRole("button", { name: "Saved, continue" })).toBeNull()
    expect(document.body.textContent).not.toMatch(/recovery|AGE-SECRET-KEY/i)
    expect(navigate).not.toHaveBeenCalled()
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    )
  })

  it("shows the key an older server minted, once, before the name", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, ENROLLMENT))
    render(<RegisterPage />)
    await enroll()

    // A server below auth revision 2 mints a key for a registration that
    // names no recipient, wraps the repository's key to it, and answers with
    // it on this one response: the only copy there will ever be.
    fetchMock.mockResolvedValueOnce(
      jsonResponse(201, {
        ...MINT,
        recoveryKey: "AGE-SECRET-KEY-1OLDER",
        recoveryPublicKey: "age1older",
      })
    )
    fireEvent.change(screen.getByLabelText("One-time code"), {
      target: { value: "123456" },
    })
    fireEvent.click(
      screen.getByRole("button", { name: "Create your Substrate" })
    )

    await screen.findByRole("heading", { name: "Save your recovery key" })
    expect(screen.getByLabelText<HTMLInputElement>("Recovery key").value).toBe(
      "AGE-SECRET-KEY-1OLDER"
    )
    const sent = JSON.parse(
      (fetchMock.mock.calls[1][1] as RequestInit).body as string
    )
    expect(sent).not.toHaveProperty("recoveryPublicKey")
    expect(navigate).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Saved, continue" }))
    await screen.findByRole("heading", { name: "What should we call you?" })
    expect(document.body.textContent).not.toMatch(/AGE-SECRET-KEY/)
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    )
  })

  it("asks the owner's name once registered, and lands only after it", async () => {
    policy.totpRequired = false
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<RegisterPage />)
    registerInOneStep()

    await screen.findByRole("heading", { name: "What should we call you?" })
    expect(screen.getByLabelText("Your name")).toBeTruthy()
    expect(getToken()).toBe("substrate_tok_minted")
    expect(navigate).not.toHaveBeenCalled()

    // There is no way past the step but saving a name.
    expect(screen.getAllByRole("button").map((b) => b.textContent)).toEqual([
      "Continue",
    ])
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    )
    expect(fetchMock.mock.calls.map(([url]) => String(url))).toEqual([
      "/register",
      "/api/v1/records",
      OWNER_SETTING_PATH,
    ])
  })

  it("registers in ONE step where no second factor is verified", async () => {
    policy.totpRequired = false
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<RegisterPage />)
    // No enrollment step at all: no QR, no seed, no code field, ever.
    fireEvent.change(screen.getByLabelText("Invite code"), {
      target: { value: "INV-1" },
    })
    fireEvent.change(screen.getByLabelText("Choose your address"), {
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
      screen.getByRole("button", { name: "Create your Substrate" })
    )

    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    expect(fetchMock.mock.calls.length).toBe(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/register")
    // A door that verifies no code has no factor to enroll, so the console
    // says it skips one rather than sending an empty seed.
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      inviteCode: "INV-1",
      identity: "geoah.localhost",
      password: PASSWORD,
      totpSecret: "",
      totpCode: "",
      skipTotp: true,
      label: "console",
    })
  })

  it("asks for no invite code where the substrate reads none", async () => {
    // The local substrate: no SUBSTRATE_INVITE_CODE and no second factor.
    // The field is not rendered, the form is complete without it, and the
    // commit carries an empty code the door ignores.
    policy.inviteRequired = false
    policy.totpRequired = false
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<RegisterPage />)
    expect(screen.queryByLabelText("Invite code")).toBeNull()
    fireEvent.change(screen.getByLabelText("Choose your address"), {
      target: { value: "geoah" },
    })
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: PASSWORD },
    })
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: PASSWORD },
    })
    fireEvent.click(
      screen.getByRole("button", { name: "Create your Substrate" })
    )

    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/register")
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      inviteCode: "",
      identity: "geoah.localhost",
      password: PASSWORD,
      totpSecret: "",
      totpCode: "",
      skipTotp: true,
      label: "console",
    })
  })

  it("names the factors this door has in the password hint", async () => {
    render(<RegisterPage />)
    expect(
      screen.getByText(
        /If you lose it and your authenticator, only whoever runs this server can reset them/
      )
    ).toBeTruthy()
    cleanup()

    // With no second factor there is only the password to lose, so the hint
    // that counts two of them is wrong.
    policy.totpRequired = false
    render(<RegisterPage />)
    expect(
      screen.getByText(
        /If you lose it, only whoever runs this server can reset it/
      )
    ).toBeTruthy()
    expect(screen.queryByText(/your authenticator/)).toBeNull()
  })

  it("still asks for the invite code where one is read, with the factor off", async () => {
    // The two answers are independent: a gated door with no second factor
    // keeps the invite field and drops the code step.
    policy.totpRequired = false
    render(<RegisterPage />)
    expect(screen.getByLabelText("Invite code")).toBeTruthy()
    expect(
      screen
        .getByRole("button", { name: "Create your Substrate" })
        .hasAttribute("disabled")
    ).toBe(true)
  })
})

/** Fill the one-step form (no second factor) and submit it. */
function registerInOneStep(domain?: string) {
  fireEvent.change(screen.getByLabelText("Invite code"), {
    target: { value: "INV-1" },
  })
  if (domain) {
    fireEvent.click(
      screen.getByRole("button", { name: "Advanced: use your own domain" })
    )
    fireEvent.change(screen.getByLabelText("Your domain"), {
      target: { value: domain },
    })
  } else {
    fireEvent.change(screen.getByLabelText("Choose your address"), {
      target: { value: "geoah" },
    })
  }
  fireEvent.change(screen.getByLabelText("Password"), {
    target: { value: PASSWORD },
  })
  fireEvent.change(screen.getByLabelText("Confirm password"), {
    target: { value: PASSWORD },
  })
  fireEvent.click(screen.getByRole("button", { name: "Create your Substrate" }))
}

/** Where a new repository lands on a deployment whose operator supplies
 * keys (`operatorDefaults` in server.json): Providers, whose welcome says
 * what works now, unless an app's approval page or Verify domain is
 * waiting. */
describe("RegisterPage where the operator supplies keys", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    navigate.mockClear()
    policy.inviteRequired = true
    policy.totpRequired = false
    policy.live = false
    policy.features = ["operatorDefaults"]
    door.site = null
    registerSearch.redirect = undefined
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("lands on Providers", async () => {
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<RegisterPage />)
    registerInOneStep()
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/providers", replace: true })
    )
    expect(navigate).toHaveBeenCalledTimes(1)
  })

  it("lands on Home where server.json lists no operatorDefaults", async () => {
    policy.features = []
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<RegisterPage />)
    registerInOneStep()
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/", replace: true })
    )
  })

  it("returns to an app's approval page instead", async () => {
    registerSearch.redirect = "/authorize/AbC123"
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    render(<RegisterPage />)
    registerInOneStep()
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/authorize/AbC123",
        replace: true,
      })
    )
    expect(navigate).toHaveBeenCalledTimes(1)
  })

  it("lands on Verify domain instead for the reader's own domain", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(201, { ...MINT, repository: "ada.example.org" })
    )
    render(<RegisterPage />)
    registerInOneStep("ada.example.org")
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/settings",
        hash: "verify-domain",
        replace: true,
      })
    )
    expect(navigate).toHaveBeenCalledTimes(1)
  })

  it("saves the name as the owner's contact, points the setting at it, then lands on Providers", async () => {
    const server = fakeOwnerServer({
      other: (path) =>
        path === "/register" ? jsonResponse(201, MINT) : undefined,
    })
    fetchMock.mockImplementation(server.impl)
    render(<RegisterPage />)
    registerInOneStep()
    fireEvent.change(await screen.findByLabelText("Your name"), {
      target: { value: "Ada Lovelace" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/providers", replace: true })
    )
    expect(navigate).toHaveBeenCalledTimes(1)
    const writes = server.writes()
    expect(writes.map((c) => `${c.method} ${c.path}`)).toEqual([
      "POST /register",
      "POST /api/v1/records",
      `PUT ${OWNER_SETTING_PATH}`,
    ])
    expect(writes[1].body).toEqual({
      kind: "substrate.reamde.dev/core/contact",
      properties: {
        name: "Ada Lovelace",
        contactType: "person",
        prominence: "known",
        substrate: "geoah.localhost",
      },
    })
    expect(writes[2].body?.properties).toMatchObject({ value: "c-minted-1" })
    // The landing waits for the setting.
    expect(navigate.mock.invocationCallOrder[0]).toBeGreaterThan(
      fetchMock.mock.invocationCallOrder.at(-1)!
    )
  })

  it("shows a failed write, and lands once a retry saves", async () => {
    const server = fakeOwnerServer({
      failSettingWrites: 1,
      other: (path) =>
        path === "/register" ? jsonResponse(201, MINT) : undefined,
    })
    fetchMock.mockImplementation(server.impl)
    render(<RegisterPage />)
    registerInOneStep()
    fireEvent.change(await screen.findByLabelText("Your name"), {
      target: { value: "Ada" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    expect((await screen.findByRole("alert")).textContent).toMatch(
      /^Saving your name failed: the server is busy\./
    )
    expect(navigate).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/providers", replace: true })
    )
    // The retry reuses the contact the first attempt created, under the name
    // the field holds now, and creates no second one.
    expect(server.writes().map((c) => `${c.method} ${c.path}`)).toEqual([
      "POST /register",
      "POST /api/v1/records",
      `PUT ${OWNER_SETTING_PATH}`,
      "PATCH /api/v1/substrate.reamde.dev/core/contact/c-minted-1",
      `PUT ${OWNER_SETTING_PATH}`,
    ])
  })
})

/** The same page against the REAL discovery hook: the reader (or a password
 * manager) can complete and submit the first step before
 * `GET /.well-known/substrate/server.json` answers, and the answer decides
 * whether there is a second step at all. */
describe("RegisterPage and the door's own answer", () => {
  const fetchMock = vi.fn<typeof fetch>()
  /** The door of a server that names a person (decision 0274). */
  const AUTH = { name: "auth", stability: "stable", revision: 3 }

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
        return jsonResponse(200, {
          registration: { totpRequired: false, nameHost: "localhost" },
          features: [AUTH],
        })
      }
      if (url === "/register/enroll") return jsonResponse(200, ENROLLMENT)
      return jsonResponse(201, MINT)
    })

    render(<RegisterPage />)
    fireEvent.change(screen.getByLabelText("Invite code"), {
      target: { value: "INV-1" },
    })
    fireEvent.change(screen.getByLabelText("Choose your address"), {
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
      screen.getByRole("button", { name: "Create your Substrate" })
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
      identity: "geoah.localhost",
      password: PASSWORD,
      totpSecret: "",
      totpCode: "",
      skipTotp: true,
      label: "console",
    })
  })

  it("lands on Providers when discovery lists operatorDefaults", async () => {
    fetchMock.mockImplementation(async (input) => {
      const url = String(input)
      if (url === "/.well-known/substrate/server.json")
        return jsonResponse(200, {
          registration: { totpRequired: false, nameHost: "localhost" },
          features: [
            AUTH,
            {
              name: "operatorDefaults",
              stability: "experimental",
              revision: 1,
            },
          ],
        })
      return jsonResponse(201, MINT)
    })
    render(<RegisterPage />)
    // The one-step form is up once discovery said there is no second factor.
    await screen.findByRole("button", { name: "Create your Substrate" })
    registerInOneStep()
    await giveName(fetchMock)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/providers", replace: true })
    )
  })

  it("names the repository, not a person, to a server whose door predates identities", async () => {
    fetchMock.mockImplementation(async (input) => {
      const url = String(input)
      if (url === "/.well-known/substrate/server.json")
        return jsonResponse(200, {
          registration: { totpRequired: false },
          features: [{ ...AUTH, revision: 2 }],
        })
      return jsonResponse(201, MINT)
    })
    render(<RegisterPage />)
    await screen.findByRole("button", { name: "Create your Substrate" })
    registerInOneStep()
    await waitFor(() => expect(getToken()).toBe("substrate_tok_minted"))
    const call = fetchMock.mock.calls.find(
      ([url]) => String(url) === "/register"
    )!
    // A server that states no name host completes the bare label itself,
    // under the host it was reached at, as it always did.
    const body = JSON.parse((call[1] as RequestInit).body as string)
    expect(body.repository).toBe("geoah")
    expect(body.identity).toBeUndefined()
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
    fireEvent.change(screen.getByLabelText("Choose your address"), {
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
