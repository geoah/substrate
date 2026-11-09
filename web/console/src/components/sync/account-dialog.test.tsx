// @vitest-environment jsdom
/** The account dialog writes whatever the account kind lets its owner write,
 * a secret included (a pasted token on a kind wearing `accountconfig`), and
 * nothing of a typed secret stays in the mutation cache: not after a refused
 * save, not after a save, not after Cancel. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { AccountDialog } from "@/components/sync/account-dialog"
import { Toaster } from "@/components/ui/toast"
import { ShippedUnits } from "@/components/units-provider.test-fixtures"
import { expectNoTrace } from "@/hooks/use-secret-mutation.test-fixtures"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

const ACCOUNT = "ada.example.com/feeds/account"

const accountKind: KindInfo = {
  identity: ACCOUNT,
  name: "account",
  authority: "ada.example.com",
  package: "feeds",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    traits: ["substrate.reamde.dev/core/accountconfig"],
    properties: {
      feedUrl: { type: "url" },
      token: { type: "secret" },
    },
  },
}

const stored: SubstrateRecord = {
  id: "main",
  kind: ACCOUNT,
  properties: { feedUrl: "https://feeds.example.com", token: "[redacted]" },
  labels: {},
  version: 2,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
}

const fetchMock = vi.fn<typeof fetch>()

beforeEach(() => vi.stubGlobal("fetch", fetchMock))
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

function sent(): unknown[] {
  return fetchMock.mock.calls
    .filter(([, init]) => init?.method === "PATCH")
    .map(([, init]) => JSON.parse(String(init?.body)))
}

describe("AccountDialog with a secret", () => {
  it("leaves nothing of a typed secret in the mutation cache after a refusal, a save or Cancel", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    client.setQueryData(["registry", "kinds"], [accountKind])
    const onOpenChange = vi.fn()
    render(
      <ShippedUnits>
        <QueryClientProvider client={client}>
          <Toaster>
            <AccountDialog
              kind={accountKind}
              providerName="feeds"
              oauth={false}
              configured
              record={stored}
              open
              onOpenChange={onOpenChange}
            />
          </Toaster>
        </QueryClientProvider>
      </ShippedUnits>
    )
    const token = screen.getByLabelText(/^Token/)
    expect(token.getAttribute("type")).toBe("password")
    const save = screen.getByRole("button", { name: "Save changes" })

    fetchMock.mockResolvedValueOnce(
      Response.json(
        { error: { code: "conflict", message: "version mismatch" } },
        { status: 409 }
      )
    )
    fireEvent.change(token, { target: { value: "tk-refused-secret" } })
    fireEvent.click(save)
    expect(await screen.findByText("Saving the account failed")).toBeTruthy()
    expect(sent()).toEqual([
      {
        properties: {
          feedUrl: "https://feeds.example.com",
          token: "tk-refused-secret",
        },
      },
    ])
    expectNoTrace(client, "tk-refused-secret")

    fetchMock.mockResolvedValueOnce(Response.json(stored))
    fireEvent.change(token, { target: { value: "tk-saved-secret" } })
    fireEvent.click(save)
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(await screen.findByText("Account updated")).toBeTruthy()
    expect(sent()).toHaveLength(2)
    expectNoTrace(client, "tk-saved-secret")

    onOpenChange.mockClear()
    fireEvent.change(token, { target: { value: "tk-cancelled-secret" } })
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
    expect(sent()).toHaveLength(2)
    expectNoTrace(client, "tk-cancelled-secret")
  })
})

describe("AccountDialog with switches that let agents act", () => {
  const googleKind: KindInfo = {
    identity: "providers.substrate.reamde.dev/google/account",
    name: "account",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    version: 1,
    source: "published",
    description: "",
    definition: {
      traits: ["substrate.reamde.dev/core/accountconfig"],
      properties: {
        enabledGmail: { type: "bool", writer: "owner", displayName: "Gmail" },
        enabledGmailWrite: {
          type: "bool",
          writer: "owner",
          displayName: "Gmail sending",
        },
      },
    },
  }

  it("puts a write switch under what your agents may do, apart from what it brings in", () => {
    const client = new QueryClient()
    client.setQueryData(["registry", "kinds"], [googleKind])
    render(
      <ShippedUnits>
        <QueryClientProvider client={client}>
          <AccountDialog
            kind={googleKind}
            providerName="Google"
            oauth
            configured
            features={[
              { toggle: "enabledGmail", scopes: ["r"], access: "read" },
              { toggle: "enabledGmailWrite", scopes: ["w"], access: "write" },
            ]}
            open
            onOpenChange={() => {}}
          />
        </QueryClientProvider>
      </ShippedUnits>
    )
    const bringIn = screen.getByText("What to bring in").closest("fieldset")
    const agents = screen
      .getByText("What your agents may do")
      .closest("fieldset")
    expect(bringIn).toBeTruthy()
    expect(agents).toBeTruthy()
    expect(within(bringIn!).getByText("Gmail")).toBeTruthy()
    expect(within(bringIn!).queryByText("Gmail sending")).toBeNull()
    expect(within(agents!).getByText("Gmail sending")).toBeTruthy()
  })
})
