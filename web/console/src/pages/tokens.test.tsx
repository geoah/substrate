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

import { ApiTokens, SignedInRows, tokenWords } from "./tokens"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

const TOKENS = [
  { id: "tok-1", label: "this browser", createdAt: "2026-08-12T00:00:00Z" },
  { id: "tok-2", label: "ci runner", createdAt: "2026-08-10T00:00:00Z" },
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
      const method = (init as RequestInit | undefined)?.method ?? "GET"
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

  it("names this browser and the command line in everyday words", async () => {
    renderWithClient(<SignedInRows />)
    expect(await screen.findByText("This browser")).toBeTruthy()
    expect(screen.getByText("You are here")).toBeTruthy()
    // A token with a label of its own reads by it.
    expect(screen.getByText("ci runner")).toBeTruthy()
  })

  it("marks this session in the developer table", async () => {
    renderWithClient(<ApiTokens />)
    const current = await screen.findByText("this browser")
    const row = current.closest("tr") as HTMLElement
    expect(within(row).getByText("this session")).toBeTruthy()
    expect(screen.getByText("ci runner")).toBeTruthy()
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

  const deletes = () =>
    fetchMock.mock.calls.filter(
      ([, init]) => (init as RequestInit | undefined)?.method === "DELETE"
    )

  it("signs another token out, after asking", async () => {
    renderWithClient(<SignedInRows />)
    await screen.findByText("ci runner")
    fireEvent.click(screen.getByRole("button", { name: "Sign out ci runner" }))
    // The consequence is named before anything is sent.
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Sign out ci runner?")
    expect(dialog.textContent).toContain(
      "Anything using this token stops working"
    )
    expect(deletes()).toHaveLength(0)
    fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }))
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
    expect(dialog.textContent).toContain("Sign out this browser?")
    expect(dialog.textContent).toContain("signs you out of this browser")
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
