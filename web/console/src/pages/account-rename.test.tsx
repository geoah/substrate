// @vitest-environment jsdom
/** Renaming the repository, where the server lists `auth` at revision 5
 * (decision 0287): the form presents the person and their factors and no
 * token, completes a bare label under the server's name host, and leaves this
 * browser signed in under the new name, the old one's last day said in the
 * toast. */
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

import {
  clearSession,
  getRepository,
  getToken,
  saveSession,
} from "@/lib/api/session"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
  useNavigate: () => vi.fn(),
}))

const offered = vi.hoisted(() => ({ revision: 5 }))
vi.mock("@/lib/api/discovery", () => ({
  useAuthPolicy: () => ({
    inviteRequired: true,
    totpRequired: true,
    nameHost: "substrate.example.com",
  }),
  discoveryQueryOptions: { queryKey: ["discovery"], queryFn: () => null },
  hasFeature: (_doc: unknown, name: string, atLeast = 0) =>
    name === "auth" && atLeast <= offered.revision,
  doorNamesAPerson: () => true,
}))
vi.mock("@/components/identity/unverified-mark", () => ({
  UnverifiedMark: () => null,
}))

import { AccountRows } from "./account"

function client(): QueryClient {
  const c = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity, retry: false } },
  })
  c.setQueryData(["identity"], {
    id: "idn-0000000000000000000000000",
    name: "ada.substrate.example.com",
    secondFactor: true,
    repositories: [
      { id: "rnd-0123456789abcdef", name: "ada.substrate.example.com" },
    ],
  })
  return c
}

describe("renaming the repository", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession(
      "substrate_tok_current",
      "ada.substrate.example.com",
      "tok-1",
      "ada.substrate.example.com"
    )
    offered.revision = 5
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("renames as the person with their factors and keeps this browser signed in under the new name", async () => {
    fetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({
          id: "rnd-0123456789abcdef",
          name: "lovelace.substrate.example.com",
          formerNames: [
            {
              name: "ada.substrate.example.com",
              until: "2027-02-11T12:00:00Z",
            },
          ],
        }),
        { status: 200 }
      )
    )
    render(
      <QueryClientProvider client={client()}>
        <AccountRows />
      </QueryClientProvider>
    )
    fireEvent.click(screen.getByRole("button", { name: "Rename…" }))
    const form = within(
      screen.getByRole("form", { name: "Rename your Substrate" })
    )
    fireEvent.change(form.getByLabelText("New name"), {
      target: { value: "lovelace" },
    })
    expect(form.getByText("lovelace.substrate.example.com")).toBeTruthy()
    fireEvent.change(form.getByLabelText("Current password"), {
      target: { value: "hunter2hunter2" },
    })
    fireEvent.change(form.getByLabelText("Current code"), {
      target: { value: "123456" },
    })
    fireEvent.click(form.getByRole("button", { name: "Rename" }))

    await waitFor(() =>
      expect(getRepository()).toBe("lovelace.substrate.example.com")
    )
    const call = fetchMock.mock.calls.find(
      ([url]) => String(url) === "/repository/rename"
    )!
    const init = call[1] as RequestInit
    expect(JSON.parse(init.body as string)).toEqual({
      identity: "ada.substrate.example.com",
      repository: "ada.substrate.example.com",
      password: "hunter2hunter2",
      totpCode: "123456",
      name: "lovelace",
    })
    expect(new Headers(init.headers).get("Authorization")).toBeNull()
    expect(getToken()).toBe("substrate_tok_current")
    expect(
      await screen.findByText("lovelace.substrate.example.com")
    ).toBeTruthy()
  })

  it("is not offered where the server cannot rename", () => {
    offered.revision = 4
    render(
      <QueryClientProvider client={client()}>
        <AccountRows />
      </QueryClientProvider>
    )
    expect(screen.queryByRole("button", { name: "Rename…" })).toBeNull()
  })
})
