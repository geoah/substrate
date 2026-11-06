// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { clearSession, getToken, saveSession } from "@/lib/api/session"

const navigate = vi.hoisted(() => vi.fn())
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
  useNavigate: () => navigate,
}))

/** What GET /.well-known/substrate/server.json said about the door; discovery.test.ts covers the fetching. */
const policy = vi.hoisted(() => ({ inviteRequired: true, totpRequired: true }))
vi.mock("@/lib/api/discovery", () => ({ useAuthPolicy: () => policy }))
// The unverified mark beside the name reads the repository record; its own
// test is verify-domain-row.test.tsx.
vi.mock("@/components/identity/unverified-mark", () => ({
  UnverifiedMark: () => null,
}))

import { AccountRows, DeleteRepositoryRow } from "./account"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

/** Open a row's form by its button and hand back the form. */
function open(button: string, form: string): HTMLElement {
  fireEvent.click(screen.getByRole("button", { name: button }))
  return screen.getByRole("form", { name: form })
}

describe("AccountRows", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "geoah", "tok-1")
    policy.totpRequired = true
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("shows the signed-in repository", () => {
    render(<AccountRows />)
    expect(screen.getByText("geoah")).toBeTruthy()
  })

  it("changes the password with the password factor in the body", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { repository: "geoah" }))
    render(<AccountRows />)
    const card = within(open("Change…", "Change your password"))
    fireEvent.change(card.getByLabelText("Current password"), {
      target: { value: "old-passphrase" },
    })
    fireEvent.change(card.getByLabelText("Current code"), {
      target: { value: "123 456" },
    })
    fireEvent.change(card.getByLabelText("New password"), {
      target: { value: "hunter2hunter2" },
    })
    fireEvent.change(card.getByLabelText("Confirm new password"), {
      target: { value: "hunter2hunter2" },
    })
    fireEvent.click(card.getByRole("button", { name: "Change password" }))

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([url]) => String(url) === "/password")
      ).toBe(true)
    )
    const call = fetchMock.mock.calls.find(
      ([url]) => String(url) === "/password"
    )!
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({
      repository: "geoah",
      password: "old-passphrase",
      totpCode: "123456",
      newPassword: "hunter2hunter2",
    })
    // The password factor never rides a bearer.
    expect(
      ((call[1] as RequestInit).headers as Record<string, string>).Authorization
    ).toBeUndefined()
  })

  it("re-enrolls TOTP in two proven steps", async () => {
    render(<AccountRows />)
    const card = within(
      open("Replace authenticator…", "Replace your authenticator")
    )

    fetchMock.mockResolvedValueOnce(
      jsonResponse(200, { totpSecret: "NEWSEED", otpauthUri: "otpauth://x" })
    )
    fireEvent.change(card.getByLabelText("Current password"), {
      target: { value: "old-passphrase" },
    })
    fireEvent.change(card.getByLabelText("Current code"), {
      target: { value: "111 111" },
    })
    fireEvent.click(card.getByRole("button", { name: "Continue" }))

    await screen.findByText("NEWSEED")
    expect(
      fetchMock.mock.calls.some(([url]) => String(url) === "/totp/enroll")
    ).toBe(true)

    fetchMock.mockResolvedValueOnce(jsonResponse(200, { repository: "geoah" }))
    fireEvent.change(card.getByLabelText("Code from the new secret"), {
      target: { value: "222 222" },
    })
    fireEvent.click(card.getByRole("button", { name: "Replace authenticator" }))

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([url]) => String(url) === "/totp")
      ).toBe(true)
    )
    const change = fetchMock.mock.calls.find(
      ([url]) => String(url) === "/totp"
    )!
    expect(JSON.parse((change[1] as RequestInit).body as string)).toEqual({
      repository: "geoah",
      password: "old-passphrase",
      totpCode: "111111",
      newTotpSecret: "NEWSEED",
      newTotpCode: "222222",
    })
  })

  it("drops the code field and the re-enrollment where no factor is verified", async () => {
    policy.totpRequired = false
    fetchMock.mockResolvedValue(jsonResponse(200, { repository: "geoah" }))
    render(<AccountRows />)
    // Nothing to replace, so the re-enrollment is not offered at all.
    expect(
      screen.queryByRole("button", { name: "Replace authenticator…" })
    ).toBeNull()
    expect(
      screen.getByText(
        /Off\. This substrate signs you in with a password alone\./
      )
    ).toBeTruthy()
    // the operator's remedy is technical detail
    expect(screen.queryByText(/substratectl/)).toBeNull()

    const card = within(open("Change…", "Change your password"))
    expect(card.queryByLabelText("Current code")).toBeNull()
    fireEvent.change(card.getByLabelText("Current password"), {
      target: { value: "old-passphrase" },
    })
    fireEvent.change(card.getByLabelText("New password"), {
      target: { value: "hunter2hunter2" },
    })
    fireEvent.change(card.getByLabelText("Confirm new password"), {
      target: { value: "hunter2hunter2" },
    })
    fireEvent.click(card.getByRole("button", { name: "Change password" }))

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([url]) => String(url) === "/password")
      ).toBe(true)
    )
    const call = fetchMock.mock.calls.find(
      ([url]) => String(url) === "/password"
    )!
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({
      repository: "geoah",
      password: "old-passphrase",
      totpCode: "",
      newPassword: "hunter2hunter2",
    })
  })
})

describe("DeleteRepositoryRow", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "geoah", "tok-1")
    policy.totpRequired = true
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    navigate.mockReset()
  })

  function renderRow() {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <DeleteRepositoryRow />
      </QueryClientProvider>
    )
  }

  it("deletes only once the name is typed, with both factors in the body", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    renderRow()
    const card = within(open("Delete…", "Delete your Substrate"))
    const submit = card.getByRole("button", { name: "Delete forever" })
    fireEvent.change(card.getByLabelText("Current password"), {
      target: { value: "old-passphrase" },
    })
    fireEvent.change(card.getByLabelText("Current code"), {
      target: { value: "123 456" },
    })
    fireEvent.change(card.getByLabelText("Confirm the name"), {
      target: { value: "geoa" },
    })
    expect((submit as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(card.getByLabelText("Confirm the name"), {
      target: { value: "geoah" },
    })
    fireEvent.click(submit)

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/register", replace: true })
    )
    const call = fetchMock.mock.calls.find(
      ([url]) => String(url) === "/repository/delete"
    )!
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({
      repository: "geoah",
      password: "old-passphrase",
      totpCode: "123456",
    })
    expect(
      ((call[1] as RequestInit).headers as Record<string, string>).Authorization
    ).toBeUndefined()
    expect(getToken()).toBeNull()
  })

  it("keeps the session when the factors are refused", async () => {
    fetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({ error: { code: "auth", message: "bad" } }),
        { status: 401 }
      )
    )
    renderRow()
    const card = within(open("Delete…", "Delete your Substrate"))
    fireEvent.change(card.getByLabelText("Confirm the name"), {
      target: { value: "geoah" },
    })
    fireEvent.change(card.getByLabelText("Current password"), {
      target: { value: "wrong" },
    })
    fireEvent.change(card.getByLabelText("Current code"), {
      target: { value: "123456" },
    })
    fireEvent.click(card.getByRole("button", { name: "Delete forever" }))

    await card.findByRole("alert")
    expect(navigate).not.toHaveBeenCalled()
    expect(getToken()).toBe("substrate_tok_current")
  })
})
