// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import type { ReactElement } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { clearSession, saveSession } from "@/lib/api/session"

const navigate = vi.fn().mockResolvedValue(undefined)

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
}))

import { ApiTokens, AppTokenRows, SignedInRows, tokenWords } from "./tokens"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

/** An app's token: the owner approved it through the authorization flow, so
 * it carries scopes and the app it was minted for. */
const APP_TOKEN = {
  id: "tok-app",
  label: "notes.example.com",
  createdAt: "2026-08-11T00:00:00Z",
  expiresAt: "2026-09-10T00:00:00Z",
  scopes: [
    "read:ada.example.com/tasks/*",
    "write:ada.example.com/notes/note",
    "agents:ada.example.com/llm/*",
    "decide:ada.example.com/llm/substrate",
  ],
  clientId: "https://notes.example.com/oauth/client.json",
  clientName: "Notes for Ada",
}

const TOKENS = [
  { id: "tok-1", label: "this browser", createdAt: "2026-08-12T00:00:00Z" },
  { id: "tok-2", label: "ci runner", createdAt: "2026-08-10T00:00:00Z" },
  APP_TOKEN,
]
const MINT = {
  token: { id: "tok-3", label: "laptop", createdAt: "2026-08-12T09:00:00Z" },
  secret: "substrate_tok_fresh_secret",
}

function renderWithClient(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

describe("Signed in and API tokens", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    // This browser holds tok-1.
    saveSession("substrate_tok_current", "geoah", "tok-1")
    navigate.mockClear()
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      const path = String(url)
      if (path === "/tokens" && method === "GET") {
        return jsonResponse(200, { items: TOKENS })
      }
      if (path === "/tokens" && method === "POST") {
        return jsonResponse(201, MINT)
      }
      if (method === "DELETE") return jsonResponse(204, undefined)
      return jsonResponse(200, {})
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  const deletes = () =>
    fetchMock.mock.calls.filter(([, init]) => init?.method === "DELETE")

  it("names this browser and the command line in everyday words", async () => {
    renderWithClient(<SignedInRows />)
    expect(await screen.findByText("This browser")).toBeTruthy()
    expect(screen.getByText("You are here")).toBeTruthy()
    // A token with a label of its own reads by it.
    expect(screen.getByText("ci runner")).toBeTruthy()
    // An app's token is not a sign-in: it is listed under Apps.
    expect(screen.queryByText("notes.example.com")).toBeNull()
    expect(
      screen.queryByRole("button", { name: "Sign out notes.example.com" })
    ).toBeNull()
  })

  it("marks this session in the developer table", async () => {
    renderWithClient(<ApiTokens />)
    const current = await screen.findByText("this browser")
    const row = current.closest("tr") as HTMLElement
    expect(within(row).getByText("this session")).toBeTruthy()
    expect(screen.getByText("ci runner")).toBeTruthy()
    // The table lists full-access tokens only.
    expect(screen.queryByText("notes.example.com")).toBeNull()
    expect(screen.queryByText("tok-app")).toBeNull()
  })

  it("lists an app with its host, claimed name, scopes and dates", async () => {
    renderWithClient(<AppTokenRows />)
    expect(await screen.findByText("notes.example.com")).toBeTruthy()
    expect(
      screen.getByText("https://notes.example.com/oauth/client.json")
    ).toBeTruthy()
    const facts = screen.getByLabelText("What notes.example.com can do")
    expect(within(facts).getByText("Calls itself")).toBeTruthy()
    expect(within(facts).getByText("Notes for Ada")).toBeTruthy()
    expect(within(facts).getByText("ada.example.com/tasks/*")).toBeTruthy()
    expect(within(facts).getByText("Read records of")).toBeTruthy()
    expect(within(facts).getByText("ada.example.com/notes/note")).toBeTruthy()
    expect(within(facts).getByText("Read and write records of")).toBeTruthy()
    expect(within(facts).getByText("Talk to the agents matching")).toBeTruthy()
    expect(within(facts).getByText("ada.example.com/llm/*")).toBeTruthy()
    expect(
      within(facts).getByText(
        "Apply or dismiss changes suggested by the agents matching"
      )
    ).toBeTruthy()
    expect(within(facts).getByText("Approved")).toBeTruthy()
    expect(within(facts).getByText("Expires")).toBeTruthy()
    expect(within(facts).getByTitle(/^2026-08-1[01] /)).toBeTruthy()
    expect(within(facts).getByTitle(/^2026-09-(09|10) /)).toBeTruthy()
    // Full-access tokens are not apps.
    expect(screen.queryByText("ci runner")).toBeNull()
    expect(screen.queryByText("this browser")).toBeNull()
  })

  it("says so when no app holds a token", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(200, { items: TOKENS.filter((t) => !("scopes" in t)) })
    )
    renderWithClient(<AppTokenRows />)
    expect(await screen.findByText("No apps")).toBeTruthy()
  })

  it("revokes an app's token, after asking", async () => {
    renderWithClient(<AppTokenRows />)
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke notes.example.com" })
    )
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Revoke notes.example.com’s access?")
    expect(dialog.textContent).toContain(
      "The app stops reaching your repository straight away."
    )
    expect(deletes()).toHaveLength(0)
    fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }))
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([url, init]) =>
            String(url) === "/tokens/tok-app" &&
            (init as RequestInit).method === "DELETE"
        )
      ).toBe(true)
    )
    expect(navigate).not.toHaveBeenCalled()
  })

  it("mints a token and reveals the secret once", async () => {
    renderWithClient(<ApiTokens />)
    await screen.findByText("ci runner")
    fireEvent.change(screen.getByLabelText("Label"), {
      target: { value: "laptop" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Mint" }))
    await screen.findByText("substrate_tok_fresh_secret")
    const post = fetchMock.mock.calls.find(
      ([url, init]) =>
        String(url) === "/tokens" && (init as RequestInit).method === "POST"
    )
    expect(post).toBeTruthy()
    expect(JSON.parse((post![1] as RequestInit).body as string)).toEqual({
      label: "laptop",
    })
  })

  it("signs another token out, after asking", async () => {
    renderWithClient(<SignedInRows />)
    await screen.findByText("ci runner")
    fireEvent.click(screen.getByRole("button", { name: "Sign out ci runner" }))
    // The consequence is named before anything is sent.
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Sign out ci runner?")
    expect(dialog.textContent).toContain(
      "It stops working straight away and has to sign in again."
    )
    expect(dialog.textContent).not.toMatch(/token|revok/i)
    expect(deletes()).toHaveLength(0)
    fireEvent.click(within(dialog).getByRole("button", { name: "Sign out" }))
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([url, init]) =>
            String(url) === "/tokens/tok-2" &&
            (init as RequestInit).method === "DELETE"
        )
      ).toBe(true)
    )
    // Not the current session, so it does not bounce to the login door.
    expect(navigate).not.toHaveBeenCalled()
  })

  it("warns that signing this browser out ends the session, and cancelling sends nothing", async () => {
    renderWithClient(<SignedInRows />)
    await screen.findByText("ci runner")
    fireEvent.click(
      screen.getByRole("button", { name: "Sign out this browser" })
    )
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Sign out?")
    expect(dialog.textContent).toContain(
      "This browser will need your password again."
    )
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(deletes()).toHaveLength(0)
    expect(navigate).not.toHaveBeenCalled()
  })

  it("signs this browser out and returns to the door", async () => {
    renderWithClient(<SignedInRows />)
    await screen.findByText("ci runner")
    fireEvent.click(
      screen.getByRole("button", { name: "Sign out this browser" })
    )
    const dialog = await screen.findByRole("dialog")
    fireEvent.click(within(dialog).getByRole("button", { name: "Sign out" }))
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/login", replace: true })
    )
  })
})

describe("tokenWords", () => {
  const now = Date.parse("2026-08-12T12:00:00Z")
  it("reads a command-line token by its host", () => {
    expect(
      tokenWords(
        {
          id: "t",
          label: "substratectl@laptop.local",
          createdAt: "2026-08-10T12:00:00Z",
        },
        false,
        now
      )
    ).toEqual({
      title: "Command line",
      description: "substratectl on laptop.local · signed in 2d ago",
    })
  })
  it("reads the console's own sign-ins as browsers", () => {
    expect(
      tokenWords(
        { id: "t", label: "console", createdAt: "2026-08-12T11:00:00Z" },
        false,
        now
      ).title
    ).toBe("Another browser")
  })
})
