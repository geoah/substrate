// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { UnverifiedMark } from "@/components/identity/unverified-mark"
import { clearSession, saveSession } from "@/lib/api/session"

import { VerifyDomainRow } from "./verify-domain-row"

const RECORD =
  "v=substrate1 server=https://substrate.example.com proof=k3j9x2abcd"

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status })
}

function repositoryPage(authority: string, verifiedAt?: string) {
  return {
    records: [
      {
        kind: "substrate.reamde.dev/core/repository",
        id: authority,
        properties: {
          authority,
          ...(verifiedAt ? { domainVerifiedAt: verifiedAt } : {}),
        },
      },
    ],
  }
}

describe("VerifyDomainRow", () => {
  const fetchMock = vi.fn<typeof fetch>()
  let verifiedAt: string | undefined
  let authority: string
  let published: boolean
  const calls: string[] = []

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "ada.example.org", "tok-1")
    verifiedAt = undefined
    authority = "ada.example.org"
    published = false
    calls.length = 0
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      const path = String(url)
      calls.push(`${method} ${path.split("?")[0]}`)
      if (path.startsWith("/api/v1/records") && method === "GET") {
        return json(200, repositoryPage(authority, verifiedAt))
      }
      if (path === "/api/v1/repository/domain" && method === "POST") {
        return json(200, {
          name: "_substrate.ada.example.org",
          record: RECORD,
          proof: "k3j9x2abcd",
        })
      }
      if (path === "/api/v1/repository/domain/verify" && method === "POST") {
        if (!published) {
          return json(200, {
            verified: false,
            reason:
              'no TXT record opening with "v=substrate1" at _substrate.ada.example.org',
          })
        }
        verifiedAt = "2026-11-02T12:00:00Z"
        return json(200, { verified: true, verifiedAt })
      }
      return json(404, { error: { code: "not_found", message: path } })
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  function renderRow() {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    return render(
      <QueryClientProvider client={client}>
        <VerifyDomainRow />
        <p data-testid="name">
          ada.example.org <UnverifiedMark />
        </p>
      </QueryClientProvider>
    )
  }

  it("shows the record to copy, the failed check's reason, then the verified name", async () => {
    renderRow()
    // Unverified until a check passes, beside the name only: the row under
    // it does not say it twice.
    expect(await screen.findAllByText("Unverified")).toHaveLength(1)

    fireEvent.click(screen.getByRole("button", { name: "Show record" }))
    expect(await screen.findByText("_substrate.ada.example.org")).toBeTruthy()
    expect(screen.getByText(RECORD)).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Copy the record value" })
    ).toBeTruthy()
    // The steps name where the record goes, and the name a provider that
    // appends the zone wants.
    expect(screen.getByText(/In the DNS settings for/)).toBeTruthy()
    expect(screen.getByText("example.org")).toBeTruthy()
    expect(screen.getByText("_substrate.ada")).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "Check" }))
    expect((await screen.findByRole("alert")).textContent).toContain(
      "no TXT record opening with"
    )
    expect(screen.getAllByText("Unverified")).toHaveLength(1)

    published = true
    fireEvent.click(screen.getByRole("button", { name: "Check" }))
    expect(await screen.findByRole("status")).toBeTruthy()
    // The check refreshes the repository record, so the mark follows.
    await waitFor(() => expect(screen.queryByText("Unverified")).toBeNull())
    expect(screen.getByText("Verified")).toBeTruthy()
    expect(calls).toContain("POST /api/v1/repository/domain/verify")
  })

  it("checks a name under the server's own domain by itself, and never calls it unverified", async () => {
    // jsdom serves the console from localhost, so ada.localhost is one label
    // under the server's host: the check needs no record.
    authority = "ada.localhost"
    published = true
    renderRow()
    expect(await screen.findByText("Verified")).toBeTruthy()
    expect(screen.queryByText("Unverified")).toBeNull()
    expect(calls).toContain("POST /api/v1/repository/domain/verify")
  })

  it("marks nothing while the repository record has not loaded", () => {
    fetchMock.mockImplementation(() => new Promise<Response>(() => {}))
    renderRow()
    expect(screen.queryByText("Unverified")).toBeNull()
  })
})
