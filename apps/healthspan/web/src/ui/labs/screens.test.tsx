import { ToastHost } from "@substrate/react"
import { act, type ReactNode } from "react"
import { createRoot, type Root } from "react-dom/client"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ModeProvider, readMode } from "../../data/mode"
import { LabScreen } from "../../screens/LabScreen"
import { LabsScreen } from "../../screens/LabsScreen"
import { ReportScreen } from "../../screens/ReportScreen"
import { ReportsScreen } from "../../screens/ReportsScreen"
import { TodayScreen } from "../../screens/TodayScreen"
import { ShellContext } from "../shell"

declare global {
  var IS_REACT_ACT_ENVIRONMENT: boolean | undefined
}

let host: HTMLDivElement
let root: Root

beforeEach(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(400)
  // jsdom has no media queries: a wide window.
  vi.stubGlobal(
    "matchMedia",
    (query: string) =>
      ({
        matches: query.includes("min-width"),
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
      }) as unknown as MediaQueryList
  )
  host = document.createElement("div")
  document.body.append(host)
  root = createRoot(host)
})

afterEach(() => {
  vi.unstubAllGlobals()
  act(() => root.unmount())
  host.remove()
  window.location.hash = ""
})

const DEMO = "?demo&today=2026-10-25"

function render(node: ReactNode, hash: string) {
  window.location.hash = hash
  act(() =>
    root.render(
      <ModeProvider mode={readMode(DEMO)}>
        <ShellContext.Provider value={{ wide: true }}>
          <ToastHost>{node}</ToastHost>
        </ShellContext.Provider>
      </ModeProvider>
    )
  )
}

const text = () => host.textContent ?? ""
const cards = () =>
  [...host.querySelectorAll("article[data-metric]")].map((a) => a.id)

describe("the lab screens on the demo member", () => {
  it("draws the important cards in panels, attention first", () => {
    render(<LabsScreen />, "#/labs")
    expect(cards()).toHaveLength(23)
    expect(cards().slice(0, 3)).toEqual([
      "total_cholesterol",
      "triglycerides",
      "ldl_cholesterol",
    ])
    expect(host.querySelector('[aria-label="Chart key"]')).not.toBeNull()
    expect(text()).toMatch(/\d+ results from 3 sources/)
    expect(text()).not.toMatch(/Mellified|WHOOP Age/)
  })

  it("filters by status from the hash's query", () => {
    render(<LabsScreen />, "#/labs?status=range")
    expect(cards().sort()).toEqual(["ldl_cholesterol", "vitamin_d"])
  })

  it("says so when no card matches, and clears", () => {
    render(<LabsScreen />, "#/labs?q=nothing-like-this")
    expect(cards()).toEqual([])
    expect(text()).toContain("No metrics match")
  })

  it("draws a history with its tiles and every result", () => {
    render(
      <LabScreen entry="ldl_cholesterol" query="" />,
      "#/labs/ldl_cholesterol"
    )
    expect(text()).toContain("LDL cholesterol")
    expect(text()).toContain("Latest")
    expect(text()).toContain("of 9 in all")
    expect(host.querySelectorAll("tbody tr")).toHaveLength(9)
  })

  it("says when a test has no results", () => {
    render(<LabScreen entry="folate" query="" />, "#/labs/folate")
    expect(text()).toContain("No results for Folate yet")
  })

  it("lists the reports and opens one", () => {
    render(<ReportsScreen />, "#/labs/reports")
    expect(text()).toContain("12 reports")
    const first = host.querySelector<HTMLAnchorElement>(
      'a[href^="#/labs/reports/"]'
    )
    expect(first).not.toBeNull()
    render(
      <ReportScreen id="demo/2026-09-24" />,
      "#/labs/reports/demo%2F2026-09-24"
    )
    expect(text()).toContain("Demo Lab")
    expect(text()).toContain("Accession")
    expect(host.querySelectorAll("tbody tr").length).toBeGreaterThan(20)
    // Vitamin B12 is shown converted, with the value the lab wrote.
    expect(text()).toMatch(/Reported as \d+ pg\/mL/)
  })

  it("lists Today's findings, the lab ones first by rule", () => {
    render(<TodayScreen />, "#/")
    const rows = [...host.querySelectorAll('a[data-section="labs"]')].map((a) =>
      a.getAttribute("href")
    )
    expect(rows.slice(0, 2).sort()).toEqual([
      "#/labs/ldl_cholesterol",
      "#/labs/vitamin_d",
    ])
    expect(rows).toContain("#/labs/psa")
  })
})
