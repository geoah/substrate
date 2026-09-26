// @vitest-environment jsdom
/** The config/account dialog's rendering contract (GUIDE §8 + rule 13): human
 * labels (schema displayName or humanized id, never a raw camelCase property),
 * an enum property as a real <select>, host-managed props (writer!=owner) never
 * offered, and — the layout fix — each field a full-width block where the
 * help/description flows beneath the control, NOT trapped in a narrow label
 * column beside it. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import { RecordConfigForm } from "./record-config-form"

// The account kind as the substrate serves it: email + grantedScopes are the
// OAuth facility's (writer: oauth), the toggles/cadence/backfill are the
// owner's, and syncFrequency is an enum with a displayName.
const accountKind: KindInfo = {
  identity: "providers.substrate.reamde.dev/google/account",
  name: "account",
  authority: "providers.substrate.reamde.dev",
  package: "google",
  version: 0,
  source: "installed",
  description: "",
  definition: {
    properties: {
      email: { type: "email", writer: "oauth" },
      grantedScopes: { type: "string", repeated: true, writer: "oauth" },
      enabledContacts: {
        type: "bool",
        displayName: "Sync contacts",
        description: "sync this account's Google Contacts",
      },
      syncFrequency: {
        type: "enum",
        values: [
          { value: "off", label: "Off" },
          { value: "hourly", label: "Every hour" },
          { value: "daily", label: "Once a day" },
        ],
        displayName: "Sync cadence",
        required: true,
        default: "daily",
      },
      backfillDepth: {
        type: "string",
        description: "how far back a first sync reaches",
      },
      tokenRef: { type: "secret", writer: "oauth" },
    },
  },
}

function renderForm() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <RecordConfigForm
        type={accountKind}
        title="Add account"
        description="Connect a Google account."
        open
        onOpenChange={vi.fn()}
      />
    </QueryClientProvider>
  )
}

describe("RecordConfigForm", () => {
  afterEach(cleanup)

  it("labels fields with displayName or a humanized id, never the raw property", () => {
    renderForm()
    expect(screen.getByText("Sync contacts")).toBeTruthy()
    expect(screen.getByText("Sync cadence")).toBeTruthy()
    expect(screen.getByText("Backfill depth")).toBeTruthy()
    // The raw camelCase ids never surface as labels.
    expect(screen.queryByText("enabledContacts")).toBeNull()
    expect(screen.queryByText("syncFrequency")).toBeNull()
    expect(screen.queryByText("backfillDepth")).toBeNull()
  })

  /** The open choice list's rows: stored value and the words it reads as. */
  function offered(): [string, string][] {
    return [...document.querySelectorAll("[cmdk-item]")].map((el) => [
      el.getAttribute("data-value") ?? "",
      el.textContent ?? "",
    ])
  }

  it("chooses an enum property from its values, seeded with the default", () => {
    renderForm()
    const choice = screen.getByLabelText(/Sync cadence/)
    expect(choice.tagName).toBe("BUTTON")
    // The declared default is the seeded value on create.
    expect(choice.textContent).toContain("Once a day")
    fireEvent.click(choice)
    expect(offered().map(([v]) => v)).toEqual(["off", "hourly", "daily"])
  })

  it("shows each option's authored label ({value, label}), submitting the raw value", () => {
    renderForm()
    fireEvent.click(screen.getByLabelText(/Sync cadence/))
    const byValue = Object.fromEntries(offered())
    // The visible text is the declared name; the stored value stays raw.
    expect(byValue.daily).toBe("Once a day")
    expect(byValue.hourly).toBe("Every hour")
    expect(byValue.off).toBe("Off")
  })

  it("a required enum seeded with a default offers NO empty choice (the 'two none' fix)", () => {
    renderForm()
    fireEvent.click(screen.getByLabelText(/Sync cadence/))
    // No clear row and none of the empty-choice copy: a required enum with a
    // value must not offer an empty pick.
    expect(offered().map(([v]) => v)).toEqual(["off", "hourly", "daily"])
    expect(screen.queryByText("Clear")).toBeNull()
    expect(screen.queryByText("None")).toBeNull()
  })

  it("never offers a host-managed (writer!=owner) property", () => {
    renderForm()
    // email + grantedScopes belong to the OAuth facility.
    expect(screen.queryByText("Email")).toBeNull()
    expect(screen.queryByText("email")).toBeNull()
    expect(screen.queryByText(/scope/i)).toBeNull()
    expect(screen.queryByText("Token ref")).toBeNull()
  })

  it("lays a field out as a full-width block — description below, not trapped in a label column", () => {
    renderForm()
    // The checkbox and its label sit on one line; its description is a SIBLING
    // block beneath, never a descendant of the <label> (the old two-column
    // wrap this replaces).
    const description = screen.getByText("sync this account's Google Contacts")
    expect(description.closest("label")).toBeNull()
  })

  it("with a `first` order, the trait's fields lead and the bundle's extras fold away", () => {
    const configKind: KindInfo = {
      identity: "providers.substrate.reamde.dev/google/config",
      name: "config",
      authority: "providers.substrate.reamde.dev",
      package: "google",
      version: 0,
      source: "published",
      description: "",
      definition: {
        traits: ["oauth2"],
        properties: {
          apiBase: { type: "url", displayName: "API base" },
          clientId: { type: "string", displayName: "Client ID" },
          clientSecret: { type: "secret", displayName: "Client secret" },
          drainBudgetMs: { type: "int", displayName: "Drain budget (ms)" },
        },
      },
    }
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <RecordConfigForm
          type={configKind}
          first={["clientId", "clientSecret"]}
          title="Set up google credentials"
          description="."
          open
          onOpenChange={vi.fn()}
        />
      </QueryClientProvider>
    )
    const labels = screen
      .getAllByText(/Client ID|Client secret/)
      .map((el) => el.textContent)
    expect(labels[0]).toContain("Client ID")
    expect(labels[1]).toContain("Client secret")
    // The two extras are behind the fold until asked for.
    expect(screen.queryByLabelText(/API base/)).toBeNull()
    expect(screen.queryByLabelText(/Drain budget/)).toBeNull()
    fireEvent.click(
      screen.getByRole("button", { name: /2 more settings, rarely needed/ })
    )
    expect(screen.getByLabelText(/API base/)).toBeTruthy()
    expect(screen.getByLabelText(/Drain budget/)).toBeTruthy()
  })
})
