import { describe, expect, it } from "vitest"
import { demoSeries, type DemoState } from "../demo/healthspan"
import { DEMO_PERSON } from "../demo/shared"
import { atStake, focusList, isSlipping } from "../engine/focus"
import { formatImpact } from "../engine/format"
import {
  badgeWord,
  focusCards,
  healthspanCards,
  healthspanKey,
  nightContext,
  selectedHeadline,
  totalAtStake,
  trendChip,
} from "./healthspanCards"
import { healthspanView } from "./view"

const today = new Date("2026-10-25T12:00:00Z")
const ME = { birthDate: DEMO_PERSON.birthDate, sex: DEMO_PERSON.sex }

/** The demo's cards with a state knob, no profile, or no height. */
function demoWith(state?: DemoState, profile?: "none", noHeight = false) {
  const series = demoSeries(today, state)
  if (noHeight) series.heightM = undefined
  const v = healthspanView(series, profile === "none" ? {} : ME, today)
  return {
    report: v.report,
    cards: healthspanCards({
      data: v.series,
      report: v.report,
      averages: v.averages,
      profile: v.profile,
      findings: v.findings,
      today,
    }),
  }
}
const demo = (state?: DemoState, profile?: "none") => demoWith(state, profile)

const { cards, report } = demo()
const card = (id: string) => {
  const c = cards.find((x) => x.id === id)
  if (!c) throw new Error(id)
  return c
}

describe("Healthspan cards", () => {
  it("has nine contributors in the engine's order, the eight signals the member's sources report and three body cards", () => {
    expect(
      cards.filter((c) => c.kind === "contributor").map((c) => c.key)
    ).toEqual([
      "sleepConsistency",
      "sleepHours",
      "zones13",
      "zones45",
      "strength",
      "steps",
      "vo2max",
      "rhr",
      "leanMass",
    ])
    expect(cards.filter((c) => c.kind === "signal")).toHaveLength(8)
    expect(cards.filter((c) => c.kind === "body").map((c) => c.label)).toEqual([
      "Weight",
      "Body fat",
      "Visceral fat rating",
    ])
  })

  it("prints each headline with its value, unit and when", () => {
    const h = (id: string) => {
      const x = card(id).headline
      return [x.value, x.unit, x.context]
    }
    expect(h("contributor-sleepConsistency")[2]).toBe("last night")
    expect(h("contributor-sleepHours")).toEqual(["6:46", "h", "last night"])
    expect(h("contributor-zones13")[2]).toBe("this week so far")
    expect(h("contributor-steps")[1]).toBe("steps")
    expect(h("contributor-steps")[2]).toBe("yesterday")
    expect(h("contributor-vo2max")).toEqual([
      "41.0",
      "ml/kg/min",
      "22 Oct 2026, Demo Watch",
    ])
    expect(h("contributor-rhr")).toEqual(["62", "bpm", "last night"])
    expect(h("contributor-leanMass")).toEqual([
      "80.4%",
      undefined,
      "11 Oct 2026, Demo DXA",
    ])
    expect(h("signal-recovery")).toEqual(["53%", undefined, "today"])
    expect(h("signal-hrv")[1]).toBe("ms")
    expect(h("signal-respiratoryRate")[0]).toMatch(/\/min$/)
    expect(h("signal-skinTemperature")[1]).toBe("°C")
    expect(h("body-bodyMass")).toEqual(["78.4", "kg", "11 Oct 2026, Demo DXA"])
    expect(h("body-visceralFat")).toEqual([
      "7",
      undefined,
      "17 Sept 2026, Demo Scale",
    ])
    expect(nightContext("2026-10-25", "2026-10-25")).toBe("last night")
  })

  it("gives contributors a zone bar with 6mo and 30d markers and a target tick", () => {
    const b = card("contributor-sleepHours").bar
    expect(b?.markers.map((m) => `${m.prefix} ${m.valueText}`)).toEqual([
      "6mo 7:23 h",
      "30d 7:07 h",
    ])
    expect(b?.ticks?.[0].label).toBe("target 7:00 h")
    expect(card("contributor-zones13").bar?.markers[0].at).toBeGreaterThan(3)
    expect(card("contributor-sleepHours").headline.impact?.text).toBe(
      "\u22120.1 years"
    )
  })

  it("gives every card a labelled scale or a stated reason, and a suggestion", () => {
    for (const c of cards) {
      if (c.bar) {
        expect(
          c.bar.segments.every((x) => !!x.label),
          c.id
        ).toBe(true)
      } else {
        expect(c.line, c.id).toMatch(/^Needs|^No readings/)
      }
      expect(c.note.text.length, c.id).toBeLessThan(90)
      expect(c.note.basis, c.id).not.toBe("")
      const rows = c.info.map((i) => i.label)
      expect(rows.slice(0, 6), c.id).toEqual([
        "What it is",
        "Why it matters",
        "What moves it",
        "Measured by",
        "Your standing",
        "Suggestion",
      ])
      if (c.kind !== "contributor") {
        expect(rows, c.id).toContain("Age score")
        expect(
          c.info.find((i) => i.label === "Age score")?.value,
          c.id
        ).toMatch(/^Not in the score\./)
      }
    }
  })

  it("puts each bar's pointers on the signal and body bars", () => {
    const prefixes = (id: string) => card(id).bar?.markers.map((m) => m.prefix)
    expect(prefixes("signal-recovery")).toEqual(["6mo", "30d"])
    expect(prefixes("signal-hrv")).toEqual(["last", "7n"])
    expect(prefixes("signal-skinTemperature")).toEqual(["last", "7n"])
    expect(card("signal-skinTemperature").bar?.markers[0].valueText).toMatch(
      /^[+\u2212]\d\.\d\d °C$/
    )
    expect(prefixes("body-bodyMass")).toEqual(["latest", "previous"])
    expect(prefixes("body-visceralFat")).toEqual(["latest"])
    expect(card("body-bodyMass").bar?.ticks?.[1].label).toBe("BMI 25")
    expect(card("signal-hrv").headlineChip?.label).toBeDefined()
  })

  it("draws the bar's zones on the detail chart at the bar's levels", () => {
    const rec = card("signal-recovery")
    expect(rec.chart.bands.map((b) => [b.tone, b.level])).toEqual([
      ["danger", 3],
      ["warning", 3],
      ["success", 3],
    ])
    // Personal baselines draw no zones; breathing's absolute range does.
    expect(card("signal-hrv").chart.bands).toEqual([])
    expect(card("signal-respiratoryRate").chart.bands).toHaveLength(1)
    // Outer zones run on past the bar, so a value beyond it is still banded.
    expect(card("contributor-zones13").chart.bands.length).toBeGreaterThan(0)
    expect(
      card("contributor-vo2max").chart.bands.some(
        (b) => b.variant === "outline"
      )
    ).toBe(true)
  })

  it("says one VO2 max reading honestly", () => {
    const one = demoWith("onevo2").cards.find((c) => c.key === "vo2max")
    expect(one?.headline.context).toMatch(/^1 reading, /)
    expect(one?.chart.data).toHaveLength(1)
    expect(one?.chart.caption).toBe("1 reading")
    expect(one?.note.text).toMatch(/^One reading only/)
    expect(one?.info.find((i) => i.label === "Measured by")?.value).toMatch(
      /one point/
    )
  })

  it("asks for a height and a profile where a scale needs them", () => {
    const d = demoWith(undefined, "none", true).cards
    expect(d.find((c) => c.id === "body-bodyMass")?.line).toMatch(
      /^Needs your height/
    )
    expect(d.find((c) => c.id === "body-bodyFat")?.line).toBe(
      "Needs your birth date and sex for a range."
    )
  })

  it("badges and edges cards by their findings", () => {
    expect(card("signal-recovery").badges[0]).toMatchObject({
      label: "Yellow",
      tone: "warning",
    })
    expect(card("signal-recovery").edge).toBe("warning")
    expect(card("contributor-rhr").badges.map((b) => b.label)).toContain(
      "Elevated this week"
    )
    expect(
      card("contributor-sleepConsistency").badges.map((b) => b.label)
    ).toEqual(["Slipping", "Sleep"])
    expect(card("contributor-strength").badges.map((b) => b.label)).toEqual([
      "Slipping",
      "Strain",
    ])
    expect(card("contributor-zones45").badges.map((b) => b.label)).toEqual([
      "On track",
      "Strain",
    ])
    expect(card("signal-strain").badges).toEqual([])
    expect(badgeWord({ subject: "hrv", word: "Depressed" })).toBe(
      "Depressed this week"
    )
  })

  it("puts in the focus list exactly the cards with adds or slipping, ranked by years at stake", () => {
    const focus = focusCards(cards)
    const statuses = cards.filter(
      (c) => c.statuses.has("adds") || c.statuses.has("slipping")
    )
    expect(new Set(focus.map((c) => c.id))).toEqual(
      new Set(statuses.map((c) => c.id))
    )
    expect(focus[0].key).toBe("strength")
    expect(focus).toHaveLength(4)
    const stakes = focus.map((c) => atStake(c.metric as never))
    expect([...stakes].sort((a, b) => b - a)).toEqual(stakes)
    expect(formatImpact(totalAtStake(cards))).toBe("+1.7")
    // Sleep consistency is slipping without adding years now: in the list,
    // with the badge.
    const sc = card("contributor-sleepConsistency")
    expect(isSlipping(sc.metric as never)).toBe(true)
    expect(focus.map((c) => c.key)).toContain("sleepConsistency")
    void focusList
  })

  it("swaps the headline for a selected point", () => {
    const c = card("contributor-sleepHours")
    const p = c.chart.data[10]
    const s = selectedHeadline(c.chart, { at: p.at, value: p.value })
    expect(s.context).toMatch(
      /^Selected: \d+ \w+ 2026, 30-day average \d:\d\d h$/
    )
    expect(s.unit).toBe("h")
  })

  it("marks every contributor No target without a profile, with no impacts", () => {
    const none = demo(undefined, "none").cards.filter(
      (c) => c.kind === "contributor"
    )
    expect(none).toHaveLength(9)
    for (const c of none) {
      expect(c.badges[0].label).toBe("No target")
      expect(c.headline.impact).toBeUndefined()
      expect(c.bar?.segments.every((s) => s.tone === "neutral") ?? true).toBe(
        true
      )
      expect(c.bar?.ticks).toBeUndefined()
    }
  })

  it("says no data this week rather than 0:00 h when WHOOP was not worn", () => {
    const paused = demo("paused").cards.find(
      (c) => c.id === "contributor-strength"
    )
    expect(paused?.headline).toMatchObject({
      value: "No data",
      context: "no data this week",
    })
  })

  it("tones a trend chip only where a direction is better", () => {
    expect(trendChip(110, 100, "up")).toMatchObject({
      direction: "up",
      tone: "success",
    })
    expect(trendChip(90, 100, "up")).toMatchObject({
      direction: "down",
      tone: "danger",
    })
    expect(trendChip(101, 100)).toMatchObject({
      direction: "flat",
      tone: "neutral",
    })
    expect(report).toBeDefined()
  })
})

describe("the Healthspan key", () => {
  const labels = (cs: typeof cards, details = false, trendView = true) =>
    healthspanKey(cs, { details, trendView }).map((i) => i.label)
  it("keys a baseline tick apart from a target, each only when drawn", () => {
    const hrv = card("signal-hrv")
    expect(hrv.bar?.ticks?.map((t) => t.variant)).toEqual(["baseline"])
    expect(card("signal-respiratoryRate").bar?.ticks?.[0].variant).toBe(
      "baseline"
    )
    expect(card("signal-skinTemperature").bar?.ticks?.[0].variant).toBe(
      "baseline"
    )
    expect(
      card("contributor-sleepHours").bar?.ticks?.[0].variant
    ).toBeUndefined()
    const key = (cs: typeof cards) =>
      healthspanKey(cs, { details: false, trendView: false })
    expect(key([hrv]).map((i) => i.label)).toContain("Your baseline")
    expect(key([hrv]).map((i) => i.label)).not.toContain("Target or threshold")
    expect(key([card("signal-spo2")]).map((i) => i.label)).not.toContain(
      "Your baseline"
    )
    expect(key([hrv]).find((i) => i.key === "baseline")?.glyph).toEqual({
      kind: "tick",
      variant: "baseline",
    })
  })
  it("names only what the cards draw, with words true of every bar", () => {
    expect(labels(cards, false)).toEqual([
      "6-month average",
      "Last night",
      "Latest reading",
      "30-day average",
      "7-night average",
      "Previous reading",
      "Target or threshold",
      "Your baseline",
      "Better",
      "Worse",
      "Poor",
      "No effect or usual",
      "Strain level",
      "Weekly snapshot",
    ])
  })
  it("names the details lines by chart type, each label once", () => {
    const l = labels(cards, true)
    expect(l).toEqual(
      expect.arrayContaining([
        "30-day average by week",
        "6-month average by week",
        "Each night or day",
        "Rolling 30-day average",
        "30-day mean and one deviation",
        "Zones from the bar",
        "VO2 max target",
        "Demo DXA",
        "Demo Scale",
      ])
    )
    expect(new Set(l).size).toBe(l.length)
  })
  it("lists no snapshot, and no contributor zones, without a profile", () => {
    const none = demo(undefined, "none").cards
    const contributors = none.filter((c) => c.kind === "contributor")
    expect(labels(contributors, false, false)).toEqual([
      "6-month average",
      "30-day average",
    ])
    expect(labels(contributors, true, false)).not.toContain(
      "Zones from the bar"
    )
  })
  it("lists only the signals' layers when the page is filtered to them", () => {
    const signals = cards.filter((c) => c.kind === "signal")
    expect(labels(signals, true, false)).not.toContain("30-day average by week")
    expect(labels(signals, true, false)).toContain(
      "30-day mean and one deviation"
    )
  })
})

describe("what the cards claim", () => {
  it("claims no target and no years without a birth date and sex", () => {
    const none = demoWith(undefined, "none").cards.filter(
      (c) => c.kind === "contributor"
    )
    for (const c of none) {
      expect(`${c.note.text} ${c.note.basis}`, c.id).not.toMatch(
        /target of|for your age|years/
      )
      for (const seg of c.bar?.segments ?? [])
        expect(seg.label, c.id).not.toMatch(/years/)
      expect(c.chart.bands, c.id).toEqual([])
      const standing =
        c.info.find((i) => i.label === "Your standing")?.value ?? ""
      expect(standing, c.id).not.toMatch(/slipping|improving|target of/)
    }
  })

  it("says slipping in the note and info exactly where the badge does", () => {
    for (const c of cards.filter((x) => x.kind === "contributor")) {
      const badge = c.badges.some((b) => b.label === "Slipping")
      const standing =
        c.info.find((i) => i.label === "Your standing")?.value ?? ""
      expect(/; slipping\./.test(standing), c.id).toBe(badge)
      expect(/; slipping\.$/.test(c.note.basis), c.id).toBe(badge)
      if (badge)
        expect(c.note.text, c.id).not.toMatch(/helping|on track|at target/)
    }
  })

  it("gives a stale signal a Stale badge and an old-reading note", () => {
    const paused = demoWith("paused").cards
    const rec = paused.find((c) => c.id === "signal-recovery")
    expect(rec?.badges.map((b) => b.label)).toEqual(["Stale"])
    expect(rec?.note.text).toMatch(/^The newest reading is from \d+ \w+ 2026/)
    expect(rec?.statuses.has("ok")).toBe(false)
    for (const c of paused.filter((x) => x.kind === "signal"))
      expect(
        c.badges.map((b) => b.label),
        c.id
      ).not.toContain("On track")
  })

  it("shows no trend chip with fewer than two readings, and pads one reading's axis", () => {
    const one = demoWith("onevo2").cards.find((c) => c.key === "vo2max")
    expect(one?.chart.chip).toBeUndefined()
    expect(one?.chart.valueRange?.min).toBeCloseTo(41.2 * 0.9)
    const series = demoSeries(today)
    series.body.visceralFat = series.body.visceralFat.slice(-1)
    const v = healthspanView(series, ME, today)
    const visceral = healthspanCards({
      data: series,
      report: v.report,
      averages: v.averages,
      profile: ME,
      findings: v.findings,
      today,
    }).find((c) => c.id === "body-visceralFat")
    expect(visceral?.chart.chip).toBeUndefined()
    expect(card("body-visceralFat").chart.chip?.label).toMatch(/vs previous$/)
  })

  it("links sources on every card", () => {
    for (const c of cards) expect(c.sources.length, c.id).toBeGreaterThan(0)
  })
})

describe("a selected body reading", () => {
  it("names its source, as Labs does", () => {
    const c = cards.find((x) => x.id === "body-bodyMass")
    const p = c?.chart.data[0]
    if (!c || !p) throw new Error("no reading")
    expect(selectedHeadline(c.chart, p).context).toMatch(
      /^Selected: \d+ \w+ 2026, Demo/
    )
  })
})
