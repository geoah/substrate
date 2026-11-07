// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { fakeOwnerServer } from "@/lib/api/owner-contact.test-fixtures"
import { clearSession, saveSession } from "@/lib/api/session"

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({
    to,
    params,
    children,
  }: {
    to: string
    params?: Record<string, string>
    children?: React.ReactNode
  }) => (
    <a href={to.replace(/\$(\w+)/g, (_, key: string) => params?.[key] ?? "")}>
      {children}
    </a>
  ),
}))

import { OwnerContactRow } from "./owner-contact-row"

function renderRow() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <OwnerContactRow />
    </QueryClientProvider>
  )
}

describe("OwnerContactRow", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "ada.example.org", "tok-1")
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("links to the name form where no setting names a contact", async () => {
    fetchMock.mockImplementation(fakeOwnerServer().impl)
    renderRow()
    const ask = await screen.findByRole("link", { name: "Tell us your name" })
    expect(ask.getAttribute("href")).toBe("/settings/name")
    expect(screen.getByText("None yet")).toBeTruthy()
  })

  it("links the contact the setting names, and the form to change it", async () => {
    fetchMock.mockImplementation(
      fakeOwnerServer({
        settingValue: "c-own",
        contacts: [{ id: "c-own", name: "Ada Lovelace" }],
      }).impl
    )
    renderRow()
    const contact = await screen.findByRole("link", { name: /Ada Lovelace/ })
    expect(contact.getAttribute("href")).toBe(
      "/data/substrate.reamde.dev/core/contact/c-own"
    )
    expect(
      screen.getByRole("link", { name: "Change name" }).getAttribute("href")
    ).toBe("/settings/name")
    expect(screen.queryByText("Tell us your name")).toBeNull()
    // Which contact is the reader's is explained and can be chosen here.
    expect(
      screen.getByText(/The card in your contacts that is you/)
    ).toBeTruthy()
    expect(screen.getByRole("button", { name: "Choose another" })).toBeTruthy()
  })

  it("asks again where the setting names a deleted contact", async () => {
    fetchMock.mockImplementation(
      fakeOwnerServer({
        settingValue: "c-gone",
        contacts: [
          { id: "c-gone", name: "Ada", deletedAt: "2026-11-06T00:00:00Z" },
        ],
      }).impl
    )
    renderRow()
    expect(
      await screen.findByRole("link", { name: "Tell us your name" })
    ).toBeTruthy()
    expect(screen.queryByRole("link", { name: /Ada/ })).toBeNull()
  })
})
