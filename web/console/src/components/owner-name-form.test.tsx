// @vitest-environment jsdom
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
import type { OwnerContact } from "@/lib/api/owner-contact"
import type { SubstrateRecord } from "@/lib/api/types"

import { OwnerNameForm } from "./owner-name-form"

const CONTACT = "substrate.reamde.dev/core/contact"
const SETTING = "substrate.reamde.dev/core/setting"

function rec(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    kind,
    id,
    version: 1,
    properties,
    labels: {},
    createdAt: "2026-11-06T00:00:00Z",
    updatedAt: "2026-11-06T00:00:00Z",
  }
}

function type(name: string) {
  fireEvent.change(screen.getByLabelText("Your name"), {
    target: { value: name },
  })
}

describe("OwnerNameForm", () => {
  const fetchMock = vi.fn<typeof fetch>()
  const onSaved = vi.fn()
  const onSecondary = vi.fn()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "ada.example.org", "tok-1")
    onSaved.mockReset()
    onSecondary.mockReset()
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("creates the contact, then points the setting at it, then calls back", async () => {
    const server = fakeOwnerServer()
    fetchMock.mockImplementation(server.impl)
    render(
      <OwnerNameForm
        repository="ada.example.org"
        onSaved={onSaved}
        onSecondary={onSecondary}
      />
    )
    const submit = screen.getByRole<HTMLButtonElement>("button", {
      name: "Continue",
    })
    expect(submit.disabled).toBe(true)
    type("  Ada Lovelace ")
    fireEvent.click(submit)

    await waitFor(() => expect(onSaved).toHaveBeenCalledWith("c-minted-1"))
    const [create, point] = server.writes()
    expect(create).toEqual({
      method: "POST",
      path: "/api/v1/records",
      body: {
        kind: CONTACT,
        properties: {
          name: "Ada Lovelace",
          contactType: "person",
          prominence: "known",
          substrate: "ada.example.org",
        },
      },
    })
    expect(point.method).toBe("PUT")
    expect(point.path).toBe(OWNER_SETTING_PATH)
    expect(point.body).toEqual({
      properties: {
        displayName: "Your own contact",
        description:
          "The id of the contact that is you. Agents and apps read it to know which contact is the owner.",
        value: "c-minted-1",
      },
    })
    expect(server.writes()).toHaveLength(2)
    expect(onSecondary).not.toHaveBeenCalled()
  })

  it("leaves `substrate` off where the repository name is outside the kind's pattern", async () => {
    const server = fakeOwnerServer()
    fetchMock.mockImplementation(server.impl)
    render(
      <OwnerNameForm
        repository="geoah"
        onSaved={onSaved}
        onSecondary={onSecondary}
      />
    )
    type("Ada")
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(server.writes()[0].body?.properties).toEqual({
      name: "Ada",
      contactType: "person",
      prominence: "known",
    })
  })

  it("skips without writing anything", () => {
    const server = fakeOwnerServer()
    fetchMock.mockImplementation(server.impl)
    render(
      <OwnerNameForm
        repository="ada.example.org"
        onSaved={onSaved}
        onSecondary={onSecondary}
      />
    )
    type("Ada")
    fireEvent.click(screen.getByRole("button", { name: "Skip for now" }))
    expect(onSecondary).toHaveBeenCalledTimes(1)
    expect(fetchMock).not.toHaveBeenCalled()
    expect(onSaved).not.toHaveBeenCalled()
  })

  it("shows a failed setting write, and a retry reuses the contact it created", async () => {
    const server = fakeOwnerServer({ failSettingWrites: 1 })
    fetchMock.mockImplementation(server.impl)
    render(
      <OwnerNameForm
        repository="ada.example.org"
        onSaved={onSaved}
        onSecondary={onSecondary}
      />
    )
    type("Ada")
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toBe(
      "Saving your name failed: the server is busy. Try again, or skip for now."
    )
    expect(onSaved).not.toHaveBeenCalled()
    // Both ways out stay open.
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Skip for now" })
        .disabled
    ).toBe(false)

    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith("c-minted-1"))
    expect(screen.queryByRole("alert")).toBeNull()
    expect(server.writes().map((c) => `${c.method} ${c.path}`)).toEqual([
      "POST /api/v1/records",
      `PUT ${OWNER_SETTING_PATH}`,
      `PATCH ${contactPath("c-minted-1")}`,
      `PUT ${OWNER_SETTING_PATH}`,
    ])
    expect(server.contacts.size).toBe(1)
    expect(server.setting()?.properties.value).toBe("c-minted-1")
  })

  it("renames the contact the setting already names instead of creating one", async () => {
    const server = fakeOwnerServer({
      settingValue: "c-own",
      contacts: [{ id: "c-own", name: "Ada" }],
    })
    fetchMock.mockImplementation(server.impl)
    const current: OwnerContact = {
      setting: rec(SETTING, "substrate.reamde.dev/core/ownerContact", {
        value: "c-own",
      }),
      contact: rec(CONTACT, "c-own", { name: "Ada" }),
    }
    render(
      <OwnerNameForm
        repository="ada.example.org"
        current={current}
        layout="page"
        submitLabel="Save"
        secondaryLabel="Cancel"
        onSaved={onSaved}
        onSecondary={onSecondary}
      />
    )
    expect(screen.getByLabelText<HTMLInputElement>("Your name").value).toBe(
      "Ada"
    )
    type("Ada King")
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith("c-own"))
    expect(server.writes()).toEqual([
      {
        method: "PATCH",
        path: contactPath("c-own"),
        body: { properties: { name: "Ada King" } },
      },
    ])
    expect(server.contacts.size).toBe(1)
  })

  it("creates a contact where the setting names a deleted one, keeping the setting's own words", async () => {
    const server = fakeOwnerServer({ settingValue: "c-gone" })
    fetchMock.mockImplementation(server.impl)
    const current: OwnerContact = {
      setting: rec(SETTING, "substrate.reamde.dev/core/ownerContact", {
        value: "c-gone",
      }),
      contact: null,
    }
    render(
      <OwnerNameForm
        repository="ada.example.org"
        current={current}
        onSaved={onSaved}
        onSecondary={onSecondary}
      />
    )
    type("Ada")
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith("c-minted-1"))
    // The setting exists, so the PUT carries the value alone.
    expect(server.writes()[1].body).toEqual({
      properties: { value: "c-minted-1" },
    })
  })
})
