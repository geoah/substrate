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

import { TokensPage } from "./tokens"

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

describe("TokensPage", () => {
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
        return jsonResponse(200, { tokens: TOKENS })
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

  it("lists tokens and marks this session", async () => {
    renderWithClient(<TokensPage />)
    const current = await screen.findByText("this browser")
    const row = current.closest("tr") as HTMLElement
    expect(within(row).getByText("this session")).toBeTruthy()
    expect(screen.getByText("ci runner")).toBeTruthy()
  })

  it("mints a token and reveals the secret once", async () => {
    renderWithClient(<TokensPage />)
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

  it("revokes a token that is not this session", async () => {
    renderWithClient(<TokensPage />)
    await screen.findByText("ci runner")
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }))
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
})
