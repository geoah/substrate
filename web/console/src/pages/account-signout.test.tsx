// @vitest-environment jsdom
/** Signing out everywhere, where the server lists `auth` at revision 2: the
 * password change's option and the Signed in section's own row both present
 * the factors and no token, then forget this browser's session, which the
 * sign-out ended with the rest. */
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

const offered = vi.hoisted(() => ({ value: true }))
vi.mock("@/lib/api/discovery", () => ({
  useAuthPolicy: () => ({ inviteRequired: true, totpRequired: true }),
  discoveryQueryOptions: { queryKey: ["discovery"], queryFn: () => null },
  hasFeature: (_doc: unknown, name: string, atLeast = 0) =>
    offered.value && name === "auth" && atLeast <= 2,
  doorNamesAPerson: () => true,
}))
vi.mock("@/components/identity/unverified-mark", () => ({
  UnverifiedMark: () => null,
}))

import { AccountRows, SignOutEverywhereRow } from "./account"

function client(): QueryClient {
  const c = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity, retry: false } },
  })
  c.setQueryData(["identity"], {
    id: "idn-0000000000000000000000000",
    name: "geoah",
    secondFactor: true,
    repositories: [{ id: "rnd-0000000000000000000000000", name: "geoah" }],
  })
  return c
}

function sentTo(fetchMock: ReturnType<typeof vi.fn>, path: string) {
  const call = fetchMock.mock.calls.find(([url]) => String(url) === path)
  if (!call) return undefined
  const init = call[1] as RequestInit
  return {
    body: JSON.parse(init.body as string),
    bearer: new Headers(init.headers).get("Authorization"),
  }
}

describe("signing out everywhere", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "geoah", "tok-1")
    offered.value = true
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    navigate.mockReset()
  })

  it("runs on its own with the factors, then forgets this browser", async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ identity: "geoah", signedOut: 3 }), {
        status: 200,
      })
    )
    render(
      <QueryClientProvider client={client()}>
        <SignOutEverywhereRow />
      </QueryClientProvider>
    )
    fireEvent.click(
      screen.getByRole("button", { name: "Sign out everywhere…" })
    )
    const form = within(
      screen.getByRole("form", { name: "Sign out everywhere" })
    )
    fireEvent.change(form.getByLabelText("Current password"), {
      target: { value: "old-passphrase" },
    })
    fireEvent.change(form.getByLabelText("Current code"), {
      target: { value: "123456" },
    })
    fireEvent.click(form.getByRole("button", { name: "Sign out everywhere" }))

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/login", replace: true })
    )
    const sent = sentTo(fetchMock, "/signout/everywhere")
    expect(sent?.body).toEqual({
      identity: "geoah",
      password: "old-passphrase",
      totpCode: "123456",
    })
    expect(sent?.bearer).toBeNull()
    expect(getToken()).toBeNull()
  })

  it("is the password change's option", async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ identity: "geoah", signedOut: 2 }), {
        status: 200,
      })
    )
    render(
      <QueryClientProvider client={client()}>
        <AccountRows />
      </QueryClientProvider>
    )
    fireEvent.click(screen.getByRole("button", { name: "Change…" }))
    const form = within(
      screen.getByRole("form", { name: "Change your password" })
    )
    fireEvent.change(form.getByLabelText("Current password"), {
      target: { value: "old-passphrase" },
    })
    fireEvent.change(form.getByLabelText("Current code"), {
      target: { value: "123456" },
    })
    fireEvent.change(form.getByLabelText("New password"), {
      target: { value: "hunter2hunter2" },
    })
    fireEvent.change(form.getByLabelText("Confirm new password"), {
      target: { value: "hunter2hunter2" },
    })
    fireEvent.click(form.getByLabelText("Also sign out everywhere"))
    fireEvent.click(form.getByRole("button", { name: "Change password" }))

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/login", replace: true })
    )
    expect(sentTo(fetchMock, "/password")?.body).toMatchObject({
      newPassword: "hunter2hunter2",
      signOutEverywhere: true,
    })
    expect(getToken()).toBeNull()
  })

  it("is not offered by a server that does not list it", () => {
    offered.value = false
    render(
      <QueryClientProvider client={client()}>
        <SignOutEverywhereRow />
        <AccountRows />
      </QueryClientProvider>
    )
    expect(
      screen.queryByRole("button", { name: "Sign out everywhere…" })
    ).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Change…" }))
    expect(screen.queryByLabelText("Also sign out everywhere")).toBeNull()
  })
})
