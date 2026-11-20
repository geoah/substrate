import { act, type ReactNode } from "react"
import { createRoot, type Root } from "react-dom/client"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  DEFAULT_DISPLAY,
  displayStore,
  useDisplayControl,
} from "../lib/display"
import { ChartKey } from "./ChartKey"
import { DisplaySettings } from "./DisplaySettings"
import { MetricCard } from "./MetricCard"
import { TrendChart, type TrendPoint } from "./TrendChart"
import { ZoneBar } from "./ZoneBar"

declare global {
  var IS_REACT_ACT_ENVIRONMENT: boolean | undefined
}

let host: HTMLDivElement
let root: Root

beforeEach(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true
  // jsdom lays nothing out: give every element a width, so charts draw.
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(400)
  host = document.createElement("div")
  document.body.append(host)
  root = createRoot(host)
})

afterEach(() => {
  act(() => root.unmount())
  host.remove()
  localStorage.clear()
})

function draw(node: ReactNode) {
  act(() => root.render(node))
}

function key(el: Element, k: string) {
  act(() => {
    el.dispatchEvent(new KeyboardEvent("keydown", { key: k, bubbles: true }))
  })
}

const data: TrendPoint[] = [
  { at: "2026-01-10T12:00:00Z", value: 2.8, series: "a" },
  { at: "2026-04-10T12:00:00Z", value: 3.2, series: "b", tone: "danger" },
  { at: "2026-07-10T12:00:00Z", value: 3.0, series: "a", detail: ["Lab A"] },
]

describe("TrendChart", () => {
  it("is a named group with a text alternative of its values", () => {
    draw(
      <TrendChart
        label="LDL over time"
        data={data}
        unit="mmol/L"
        formatValue={(v) => v.toFixed(2)}
        formatTime={(d) => d.toISOString().slice(0, 10)}
        bands={[{ to: 3, tone: "success", label: "Target: good" }]}
      />
    )
    const chart = host.querySelector("[role=group]")!
    expect(chart.getAttribute("aria-label")).toBe("LDL over time")
    const desc = document.getElementById(
      chart.getAttribute("aria-describedby")!
    )!
    expect(desc.textContent).toContain(
      "3 values from 2026-01-10 to 2026-07-10; latest 3.00 mmol/L on 2026-07-10; Target: good up to 3.00 mmol/L"
    )
    expect(chart.getAttribute("tabindex")).toBe("0")
  })

  it("steps with the arrow keys, reports each point, pins and clears", () => {
    const seen: [number | null, boolean][] = []
    draw(
      <TrendChart
        label="LDL"
        data={data}
        readout="none"
        onActiveChange={(p, pinned) => seen.push([p?.value ?? null, pinned])}
      />
    )
    const chart = host.querySelector("[role=group]")!
    key(chart, "ArrowRight")
    key(chart, "ArrowRight")
    key(chart, "End")
    key(chart, "ArrowLeft")
    key(chart, "Enter")
    key(chart, "Escape")
    expect(seen).toEqual([
      [2.8, false],
      [3.2, false],
      [3.0, false],
      [3.2, false],
      [3.2, true],
      [null, false],
    ])
    // The point the keys reach is read out.
    expect(host.querySelector("[aria-live=polite]")?.textContent).toBe("")
  })

  it("reads out the point the keys reach", () => {
    draw(
      <TrendChart
        label="LDL"
        data={data}
        unit="mmol/L"
        formatValue={(v) => v.toFixed(1)}
        formatTime={(d) => d.toISOString().slice(0, 10)}
      />
    )
    const chart = host.querySelector("[role=group]")!
    key(chart, "End")
    expect(host.querySelector("[aria-live=polite]")?.textContent).toBe(
      "3.0 mmol/L, 2026-07-10, Lab A"
    )
  })

  it("rings a toned point, dashes the trend and hatches the empty span", () => {
    draw(
      <TrendChart
        label="LDL"
        data={data}
        timeRange={{
          start: "2025-01-01T12:00:00Z",
          end: "2026-10-01T12:00:00Z",
        }}
        emptyBefore={{ label: () => "No results before Jan 2026" }}
        trendline={{ from: data[0], to: data[2], label: "Flat" }}
      />
    )
    // Three markers and one ring.
    expect(host.querySelectorAll("[data-marker=circle]")).toHaveLength(4)
    expect(host.querySelector("[data-trend]")).not.toBeNull()
    expect(host.querySelector("[data-empty-span]")).not.toBeNull()
    expect(host.textContent).toContain("No results before Jan 2026")
  })

  it("shows the date one chart points at on every chart of its sync key", () => {
    draw(
      <>
        <TrendChart label="One" data={data} syncKey="t" />
        <TrendChart label="Two" data={data} syncKey="t" />
      </>
    )
    const [one, two] = host.querySelectorAll("[role=group][data-points]")
    const overlays = (el: Element) =>
      el.querySelectorAll("svg.pointer-events-none").length
    expect(overlays(two)).toBe(0)
    key(one, "ArrowRight")
    expect(overlays(one)).toBe(1)
    expect(overlays(two)).toBe(1)
    key(one, "Escape")
    expect(overlays(two)).toBe(0)
  })

  it("says so when it has nothing to draw", () => {
    draw(
      <TrendChart label="Empty" data={[]} empty="No results in this range" />
    )
    expect(host.textContent).toContain("No results in this range")
    expect(host.querySelector("[role=group]")?.hasAttribute("tabindex")).toBe(
      false
    )
  })
})

describe("ZoneBar", () => {
  it("names the bar, its pointers and its scale, and a pointer beyond it", () => {
    draw(
      <ZoneBar
        label="Strain"
        min={0}
        max={21}
        segments={[{ tone: "accent" }, { tone: "accent", level: 2 }]}
        markers={[
          { at: 9.3, side: "above", label: "Latest", valueText: "9.3" },
          { at: 24, side: "below", label: "Peak", valueText: "24.0" },
        ]}
      />
    )
    expect(host.querySelector("[role=img]")?.getAttribute("aria-label")).toBe(
      "Strain, Latest 9.3, Peak 24.0 beyond the scale, scale from 0 to 21"
    )
  })

  it("walks the labelled segments with the arrow keys and reads each", () => {
    draw(
      <ZoneBar
        label="Hours of sleep"
        min={5}
        max={8}
        segments={[
          { tone: "danger", label: "Under 6 h, adds 0.6 years" },
          { tone: "neutral", label: "6 to 7 h, no effect" },
          { tone: "success", label: "7 h or more, takes 0.2 years" },
        ]}
      />
    )
    const scale = host.querySelector("[aria-roledescription=scale]")!
    key(scale, "ArrowRight")
    key(scale, "ArrowRight")
    expect(host.querySelector("[aria-live=polite]")?.textContent).toBe(
      "6 to 7 h, no effect"
    )
    key(scale, "Escape")
    expect(host.querySelector("[aria-live=polite]")?.textContent).toBe("")
  })
})

describe("ChartKey", () => {
  it("is a named list of glyphs and labels, the current one marked", () => {
    draw(
      <ChartKey
        label="Chart key"
        current="b"
        items={[
          {
            key: "a",
            label: "Lab A",
            glyph: { kind: "marker", shape: "circle" },
          },
          {
            key: "b",
            label: "Lab B",
            glyph: { kind: "marker", shape: "square" },
          },
          { key: "empty", label: "No results yet", glyph: { kind: "hatch" } },
        ]}
      />
    )
    const list = host.querySelector("[role=list]")!
    expect(list.getAttribute("aria-label")).toBe("Chart key")
    expect(
      [...list.querySelectorAll("li")].map((li) => li.textContent)
    ).toEqual(["Lab A", "Lab B", "No results yet"])
    expect(list.querySelector("[data-key=b]")?.className).toContain(
      "text-label"
    )
    expect(list.querySelector("[data-marker=square]")).not.toBeNull()
  })
})

describe("DisplaySettings", () => {
  it("switches a setting, keeps it in the store and resets the screen", () => {
    displayStore.set(DEFAULT_DISPLAY)
    function Panel() {
      return (
        <DisplaySettings
          page="labs"
          control={useDisplayControl()}
          variant="inline"
        />
      )
    }
    draw(<Panel />)
    const trendline = host.querySelector<HTMLButtonElement>(
      "[role=switch][aria-label=Trendline]"
    )!
    expect(trendline.getAttribute("aria-checked")).toBe("true")
    act(() => trendline.click())
    expect(trendline.getAttribute("aria-checked")).toBe("false")
    expect(
      JSON.parse(localStorage.getItem("healthspan.display") ?? "{}").labs
        .trendline
    ).toBe(false)
    const reset = [...host.querySelectorAll("button")].find(
      (b) => b.textContent === "Reset to defaults"
    )!
    act(() => reset.click())
    expect(trendline.getAttribute("aria-checked")).toBe("true")
  })
})

describe("MetricCard", () => {
  it("lays out its parts and opens its info as a panel from 768 px", () => {
    vi.stubGlobal(
      "matchMedia",
      (query: string) =>
        ({
          matches: query.includes("min-width: 768px"),
          addEventListener: () => {},
          removeEventListener: () => {},
        }) as unknown as MediaQueryList
    )
    draw(
      <MetricCard
        id="ldl"
        title="LDL cholesterol"
        edge="danger"
        info={{ label: "About LDL cholesterol", content: <p>What it is.</p> }}
        history={{ label: "LDL cholesterol history", href: "#/labs/ldl" }}
        headline={{
          value: "3.42",
          unit: "mmol/L",
          context: "25 Sept 2026, Lab A",
        }}
        visual={<p>chart</p>}
        note="Keep it low."
      />
    )
    const card = host.querySelector("article")!
    expect(card.getAttribute("aria-labelledby")).toBe("ldl-title")
    expect(card.textContent).toContain("3.42 mmol/L")
    expect(
      host
        .querySelector("a[aria-label='LDL cholesterol history']")
        ?.getAttribute("href")
    ).toBe("#/labs/ldl")
    const info = host.querySelector<HTMLButtonElement>(
      "button[aria-label='About LDL cholesterol']"
    )!
    act(() => info.click())
    const panel = host.querySelector("[role=dialog]")!
    expect(panel.getAttribute("aria-label")).toBe("About LDL cholesterol")
    expect(panel.textContent).toBe("What it is.")
    expect(info.getAttribute("aria-expanded")).toBe("true")
    key(panel, "Escape")
    expect(host.querySelector("[role=dialog]")).toBeNull()
    expect(document.activeElement).toBe(info)
    vi.unstubAllGlobals()
  })
})
