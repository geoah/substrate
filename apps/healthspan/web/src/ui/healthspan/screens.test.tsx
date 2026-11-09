import { act } from "react"
import { createRoot, type Root } from "react-dom/client"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// The demo keeps its member in module memory for the page's life, as a
// page does: each test loads the modules afresh, as a new page would.
async function load() {
  vi.resetModules()
  const [kit, mode, screen, metric, how] = await Promise.all([
    import("@substrate/react"),
    import("../../data/mode"),
    import("../../screens/HealthspanScreen"),
    import("../../screens/HealthspanMetricScreen"),
    import("../../screens/HowScreen"),
  ])
  return {
    ToastHost: kit.ToastHost,
    ModeProvider: mode.ModeProvider,
    readMode: mode.readMode,
    HealthspanScreen: screen.HealthspanScreen,
    HealthspanMetricScreen: metric.HealthspanMetricScreen,
    HowScreen: how.HowScreen,
  }
}
let M: Awaited<ReturnType<typeof load>>

declare global {
  var IS_REACT_ACT_ENVIRONMENT: boolean | undefined
}

let host: HTMLDivElement
let root: Root

beforeEach(async () => {
  M = await load()
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
  window.history.replaceState(null, "", "/")
})

function render(node: React.ReactNode, search: string, hash: string) {
  // The whole address, as a page opened on it has: a domain may read it.
  window.history.replaceState(null, "", `/${search}${hash}`)
  act(() =>
    root.render(
      <M.ModeProvider mode={M.readMode(search)}>
        <M.ToastHost>{node}</M.ToastHost>
      </M.ModeProvider>
    )
  )
}

const text = () => host.textContent ?? ""

describe("the Healthspan screens on the demo member", () => {
  it("draw the hero, the focus list and every section", () => {
    render(<M.HealthspanScreen />, "?demo&today=2026-10-25", "#/healthspan")
    expect(text()).toContain("Age score")
    expect(text()).toContain("Pace of aging")
    expect(text()).toContain("What to focus on")
    expect(text()).toContain("Age score contributors")
    expect(text()).toContain("Signals")
    expect(host.querySelectorAll("article[data-metric]").length).toBe(20)
    expect(text()).not.toMatch(/WHOOP Age|Mellified/)
  })

  it("filters by status from the hash's query", () => {
    render(
      <M.HealthspanScreen />,
      "?demo&today=2026-10-25",
      "#/healthspan?status=slipping&group=strain"
    )
    const ids = [...host.querySelectorAll("article[data-metric]")].map(
      (a) => a.id
    )
    expect(ids).toEqual(["contributor-zones13", "contributor-strength"])
  })

  it("asks for getting started without a birth date and sex", () => {
    render(
      <M.HealthspanScreen />,
      "?demo&today=2026-10-25&profile=none",
      "#/healthspan"
    )
    expect(text()).toContain("Finish getting started")
    expect(text()).toContain("No target")
  })

  it("says the score is locked until it has its nights", () => {
    render(
      <M.HealthspanScreen />,
      "?demo&today=2026-10-25&healthspan=locked",
      "#/healthspan"
    )
    expect(text()).toMatch(/unlocks after 21 nights/)
  })

  it("draw a contributor's weeks, a signal's days and a body reading's scans", () => {
    render(
      <M.HealthspanMetricScreen id="strength" />,
      "?demo&today=2026-10-25",
      "#/healthspan/strength"
    )
    expect(text()).toContain("Week by week")
    act(() => root.unmount())
    root = createRoot(host)
    render(
      <M.HealthspanMetricScreen id="hrv" />,
      "?demo&today=2026-10-25",
      "#/healthspan/hrv"
    )
    expect(text()).toContain("Day by day")
    act(() => root.unmount())
    root = createRoot(host)
    render(
      <M.HealthspanMetricScreen id="bodyMass" />,
      "?demo&today=2026-10-25",
      "#/healthspan/bodyMass"
    )
    expect(text()).toContain("Demo DXA")
  })

  it("explains the score as this app's estimate, with what changes on purpose", () => {
    render(<M.HowScreen />, "?demo&today=2026-10-25", "#/healthspan/how")
    expect(text()).toMatch(/estimate this app works out/)
    expect(text()).toContain("What changes on purpose")
    expect(text()).toContain("Every source's strength workouts count")
  })
})
