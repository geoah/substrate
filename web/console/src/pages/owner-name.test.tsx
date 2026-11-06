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

import {
  OWNER_SETTING_PATH,
  contactPath,
  fakeOwnerServer,
} from "@/lib/api/owner-contact.test-fixtures"
import { clearSession, saveSession } from "@/lib/api/session"

const navigate = vi.fn().mockResolvedValue(undefined)

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
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

import { OwnerNamePage } from "./owner-name"

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <OwnerNamePage />
    </QueryClientProvider>
  )
}

describe("OwnerNamePage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "ada.example.org", "tok-1")
    navigate.mockClear()
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("creates the owner's contact for a repository that has none", async () => {
    const server = fakeOwnerServer()
    fetchMock.mockImplementation(server.impl)
    renderPage()
    const field = await screen.findByLabelText<HTMLInputElement>("Your name")
    expect(field.value).toBe("")
    expect(screen.queryByText("Your contact")).toBeNull()

    fireEvent.change(field, { target: { value: "Ada Lovelace" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/settings" })
    )
    expect(server.writes().map((c) => `${c.method} ${c.path}`)).toEqual([
      "POST /api/v1/records",
      `PUT ${OWNER_SETTING_PATH}`,
    ])
    expect(server.setting()?.properties).toMatchObject({
      displayName: "Your own contact",
      value: "c-minted-1",
    })
  })

  it("shows the contact the setting names and renames it, creating none", async () => {
    const server = fakeOwnerServer({
      settingValue: "c-own",
      contacts: [{ id: "c-own", name: "Ada" }],
    })
    fetchMock.mockImplementation(server.impl)
    renderPage()
    const field = await screen.findByLabelText<HTMLInputElement>("Your name")
    expect(field.value).toBe("Ada")
    expect(screen.getByText("Your contact")).toBeTruthy()
    expect(screen.getByRole("link", { name: /Ada/ }).getAttribute("href")).toBe(
      "/data/substrate.reamde.dev/core/contact/c-own"
    )

    fireEvent.change(field, { target: { value: "Ada King" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/settings" })
    )
    expect(server.writes()).toEqual([
      {
        method: "PATCH",
        path: contactPath("c-own"),
        body: { properties: { name: "Ada King" } },
      },
    ])
  })

  it("goes back to Settings on Cancel without writing", async () => {
    const server = fakeOwnerServer()
    fetchMock.mockImplementation(server.impl)
    renderPage()
    fireEvent.change(await screen.findByLabelText("Your name"), {
      target: { value: "Ada" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(navigate).toHaveBeenCalledWith({ to: "/settings" })
    expect(server.writes()).toEqual([])
  })
})
