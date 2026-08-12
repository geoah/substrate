/** The config/account dialog's rendering contract (GUIDE §8 + rule 13): human
 * labels (schema displayName or humanized id, never a raw camelCase property),
 * an enum property as a real <select>, host-managed props (writer!=owner) never
 * offered, and — the layout fix — each field a full-width block where the
 * help/description flows beneath the control, NOT trapped in a narrow label
 * column beside it. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import { RecordConfigForm } from "./record-config-form"

// The account kind as the substrate serves it: email + grantedScopes are the
// OAuth facility's (writer: oauth), the toggles/cadence/backfill are the
// owner's, and syncFrequency is an enum with a displayName.
const accountKind: KindInfo = {
  identity: "google.bundles.substrate.reamde.dev/account",
  name: "account",
  authority: "google.bundles.substrate.reamde.dev",
  version: "",
  plural: "accounts",
  source: "installed",
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

  it("renders an enum property as a <select> carrying its options", () => {
    renderForm()
    const select = screen.getByLabelText(/Sync cadence/) as HTMLSelectElement
    expect(select.tagName).toBe("SELECT")
    const options = within(select)
      .getAllByRole("option")
      .map((o) => (o as HTMLOptionElement).value)
    expect(options).toEqual(expect.arrayContaining(["off", "hourly", "daily"]))
    // The declared default is the seeded value on create.
    expect(select.value).toBe("daily")
  })

  it("shows each option's authored label ({value, label}), submitting the raw value", () => {
    renderForm()
    const select = screen.getByLabelText(/Sync cadence/) as HTMLSelectElement
    const options = within(select).getAllByRole("option") as HTMLOptionElement[]
    const byValue = Object.fromEntries(options.map((o) => [o.value, o.textContent]))
    // The visible text is the declared name; the submitted value stays raw.
    expect(byValue.daily).toBe("Once a day")
    expect(byValue.hourly).toBe("Every hour")
    expect(byValue.off).toBe("Off")
  })

  it("a required enum seeded with a default offers NO empty option (the 'two none' fix)", () => {
    renderForm()
    const select = screen.getByLabelText(/Sync cadence/) as HTMLSelectElement
    const options = within(select).getAllByRole("option") as HTMLOptionElement[]
    // No empty placeholder (its value is ""), and none of the empty-choice
    // copy — a required select with a value must not offer an empty pick.
    expect(options.some((o) => o.value === "")).toBe(false)
    expect(within(select).queryByText("— none —")).toBeNull()
    expect(within(select).queryByText("— select —")).toBeNull()
    // Exactly the three declared options remain.
    expect(options.map((o) => o.value)).toEqual(["off", "hourly", "daily"])
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
})
