import { act, type ReactNode } from "react"
import { createRoot, type Root } from "react-dom/client"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { ModeProvider, readMode } from "../data/mode"
import { resetDemoProfile } from "../demo/profile"
import { GettingStartedScreen } from "../screens/GettingStartedScreen"
import { RecordScreen } from "../screens/RecordScreen"
import { SettingsScreen } from "../screens/SettingsScreen"

declare global {
  var IS_REACT_ACT_ENVIRONMENT: boolean | undefined
}

let host: HTMLDivElement
let root: Root

beforeEach(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true
  resetDemoProfile()
  host = document.createElement("div")
  document.body.append(host)
  root = createRoot(host)
})

afterEach(() => {
  act(() => root.unmount())
  host.remove()
  window.location.hash = ""
})

async function show(search: string, screen: ReactNode) {
  await act(async () => {
    root.render(<ModeProvider mode={readMode(search)}>{screen}</ModeProvider>)
  })
}

function button(name: string): HTMLButtonElement {
  const b = [...host.querySelectorAll("button")].find(
    (x) => x.textContent?.trim() === name
  )
  if (!b) throw new Error(`no button ${name}`)
  return b
}

const text = () => host.textContent ?? ""

describe("getting started in the demo", () => {
  it("greets the owner, offers the registration's value and saves only once confirmed", async () => {
    await show("?demo&start&today=2026-11-09", <GettingStartedScreen />)
    expect(text()).toContain("Hello, Alex Demo.")
    expect(text()).toContain("From your contact")
    expect(text()).toContain(
      "Your patient registration with Demo Health Service records male as your gender."
    )
    const male = () =>
      host.querySelector<HTMLButtonElement>('[role="radio"][data-value="male"]')
    expect(male()?.getAttribute("aria-checked")).toBe("false")
    await act(async () => button("Use it").click())
    expect(male()?.getAttribute("aria-checked")).toBe("true")
    await act(async () => button("Finish").click())
    expect(window.location.hash).toBe("#/")
    // Settings now reads what was saved.
    await show("?demo&start&today=2026-11-09", <SettingsScreen />)
    expect(text()).toContain("Kept by Healthspan.")
    expect(
      host
        .querySelector('[role="radio"][data-value="male"]')
        ?.getAttribute("aria-checked")
    ).toBe("true")
  })

  it("says so where no contact is marked as the owner", async () => {
    await show("?demo&start&profile=none", <GettingStartedScreen />)
    expect(text()).toContain("No contact is marked as you yet")
    expect(text()).not.toContain("patient registration")
    expect(host.querySelector('input[type="date"]')).not.toBeNull()
  })
})

describe("the record in the demo", () => {
  it("lists every section and marks everything read", async () => {
    await show("?demo&today=2026-11-09", <RecordScreen />)
    for (const h of [
      "Needs attention",
      "Conditions",
      "Medicines",
      "Allergies",
      "Vaccinations",
      "Appointments",
      "Referrals",
      "Documents",
      "Consultations",
      "Messages",
    ])
      expect(text()).toContain(h)
    expect(text()).toContain(
      "From Demo General Hospital and Demo Health Service"
    )
    const news = () =>
      [...host.querySelectorAll("span")].filter((s) => s.textContent === "New")
        .length
    expect(news()).toBeGreaterThan(0)
    await act(async () => button("Mark all read").click())
    expect(news()).toBe(0)
  })
})
