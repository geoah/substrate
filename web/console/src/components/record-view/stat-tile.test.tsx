// @vitest-environment jsdom
/** The stat tile and its figures: the dial's arcs as path text, the band
 * word beside the value, the delta in its tone, and the range bar's value
 * placement. Invented values only. */

import { cleanup, render } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { bandOf, toneColor, type Band } from "@/lib/record-view"

import { Dial, RangeBar, StatTile, arcPath } from "./stat-tile"

afterEach(cleanup)

const thirds: Band[] = [
  { low: 0, high: 34, label: "Low", tone: "poor" },
  { low: 34, high: 67, label: "Middle", tone: "fair" },
  { low: 67, high: 100, label: "High", tone: "good" },
]

describe("arcPath", () => {
  it("draws the upper half circle from the left end to the right", () => {
    expect(arcPath(60, 62, 50, 180, 0)).toBe("M10 62 A50 50 0 0 1 110 62")
  })
})

describe("Dial", () => {
  it("draws the track, one arc per band and the value's arc to 75 of 100", () => {
    const view = render(
      <Dial
        value={75}
        min={0}
        max={100}
        bands={thirds}
        band={{ label: "High", tone: "good" }}
        text="75"
      />
    )
    const value = view.container.querySelector("path[data-slot=dial-value]")!
    // 75 of 0..100 sits at 45 degrees: (60 + 50 cos 45, 62 - 50 sin 45).
    expect(value.getAttribute("d")).toBe("M10 62 A50 50 0 0 1 95.36 26.64")
    expect(value.getAttribute("stroke")).toBe("var(--ok)")
    const bands = [...view.container.querySelectorAll("path[data-band]")]
    expect(bands.map((b) => b.getAttribute("data-band"))).toEqual([
      "Low",
      "Middle",
      "High",
    ])
    expect(bands[1].getAttribute("stroke")).toBe("var(--warn-soft)")
    expect(view.container.textContent).toContain("High")
  })

  it("draws the track alone and says no reading without a value", () => {
    const view = render(
      <Dial value={null} min={0} max={100} bands={[]} text="" />
    )
    expect(view.container.querySelector("path[data-slot=dial-value]")).toBe(
      null
    )
    expect(view.container.textContent).toContain("no reading")
  })
})

describe("StatTile", () => {
  it("puts the band word beside the value and the delta in its tone", () => {
    const view = render(
      <StatTile
        label="Example score"
        value="72"
        unit="%"
        band={{ label: "Steady", tone: "good" }}
        delta={{ text: "+4 (6%) against 30 days", tone: "good" }}
        show="number"
      />
    )
    const band = view.container.querySelector("[data-slot=stat-band]")!
    expect(band.textContent).toBe("Steady")
    expect((band as HTMLElement).style.color).toBe("var(--ok)")
    const delta = view.container.querySelector("[data-slot=stat-delta]")!
    expect(delta.textContent).toBe("+4 (6%) against 30 days")
    expect((delta as HTMLElement).style.color).toBe("var(--ok)")
    expect(view.container.textContent).toContain("72")
  })

  it("prints its unit's symbol beside the number, never the UCUM code", () => {
    const view = render(
      <StatTile
        label="Skin temperature"
        value="33.4"
        unit="Cel"
        show="number"
      />
    )
    expect(view.container.textContent).toContain("33.4°C")
    expect(view.container.textContent).not.toContain("Cel")
  })

  it("says its words where there is no value", () => {
    const view = render(
      <StatTile
        label="Example score"
        words="Not measured during this cycle"
        show="number"
      />
    )
    expect(view.container.textContent).toContain(
      "Not measured during this cycle"
    )
    expect(view.container.querySelector("[data-slot=stat-band]")).toBe(null)
  })
})

describe("a value on a shared band bound", () => {
  // WHOOP's recovery bands share their bounds: 34 is the first Yellow and 67
  // the first Green. The tile judges its band with bandOf; the dial and the
  // bar draw that band and never judge the value again.
  const recovery: Band[] = [
    { high: 34, label: "Red", tone: "poor" },
    { low: 34, high: 67, label: "Yellow", tone: "fair" },
    { low: 67, label: "Green", tone: "good" },
  ]
  for (const value of [34, 67]) {
    it(`says and colours ${value} by the tile's band on a dial`, () => {
      const band = bandOf(value, recovery)!
      const view = render(
        <StatTile
          label="Recovery"
          value={String(value)}
          numeric={value}
          band={{ label: band.label!, tone: band.tone }}
          bands={recovery}
          show="dial"
        />
      )
      const word = view.container.querySelector("[data-slot=stat-band]")!
      expect(word.textContent).toBe(band.label)
      expect((word as HTMLElement).style.color).toBe(toneColor(band.tone))
      expect(
        view.container
          .querySelector("path[data-slot=dial-value]")!
          .getAttribute("stroke")
      ).toBe(toneColor(band.tone))
    })

    it(`marks ${value} on a bar in the band bandOf holds it in`, () => {
      const view = render(
        <RangeBar value={value} min={0} max={100} bands={recovery} />
      )
      const marker = view.container.querySelector(
        "[data-slot=range-value]"
      ) as HTMLElement
      expect(marker.style.background).toBe(
        toneColor(bandOf(value, recovery)!.tone)
      )
    })
  }

  it("names the dial's number outside the decorative drawing", () => {
    const view = render(
      <StatTile
        label="Recovery"
        value="67"
        numeric={67}
        band={{ label: "Green", tone: "good" }}
        bands={recovery}
        show="dial"
      />
    )
    const svg = view.container.querySelector("svg")!
    expect(svg.getAttribute("aria-hidden")).toBe("true")
    const heard = [...view.container.querySelectorAll("span")]
      .filter((s) => !s.closest("[aria-hidden=true]"))
      .map((s) => s.textContent)
      .join(" ")
    expect(heard).toContain("67")
    expect(heard).toContain("Green")
  })
})

describe("RangeBar", () => {
  it("places a value of 60 halfway along 40 to 80", () => {
    const view = render(
      <RangeBar
        value={60}
        min={40}
        max={80}
        bands={[]}
        baseline={{ avg: 55, low: 48, high: 62 }}
      />
    )
    const bar = view.container.querySelector("[data-slot=range-bar]")!
    expect(bar.getAttribute("data-value-at")).toBe("50")
    const base = view.container.querySelector(
      "[data-slot=range-baseline]"
    ) as HTMLElement
    expect(base.style.left).toBe("20%")
    expect(base.style.width).toBe("35%")
    // A scale the caller names labels its ends.
    expect(bar.textContent).toContain("40")
    expect(bar.textContent).toContain("80")
  })

  it("labels no padded end, and never pads a non-negative scale below zero", () => {
    // A result of 1.2 mg/L against a range of 0 to 5 mg/L.
    const view = render(
      <RangeBar
        value={1.2}
        bands={[{ low: 0, high: 5, label: "reference range", tone: "none" }]}
        unit="mg/L"
        precision={1}
      />
    )
    const bar = view.container.querySelector("[data-slot=range-bar]")!
    expect(bar.textContent).toBe("")
    const band = bar.querySelector("[data-band]") as HTMLElement
    expect(band.style.left).toBe("0%")
  })

  it("places 2.8 under 3 near the bound of a scale from zero and prints the bound", () => {
    const view = render(
      <RangeBar
        value={2.8}
        min={0}
        bands={[{ high: 3, label: "reference range", tone: "good" }]}
        unit="ng/mL"
        marked
      />
    )
    const bar = view.container.querySelector("[data-slot=range-bar]")!
    // 0 to 3 padded by 5% at the top: 2.8 sits at 2.8 / 3.15.
    expect(Number(bar.getAttribute("data-value-at"))).toBeCloseTo(88.89, 1)
    const band = bar.querySelector("[data-band]") as HTMLElement
    expect(band.style.left).toBe("0%")
    expect(band.style.background).toBe("var(--ok-soft)")
    const edges = bar.querySelectorAll("[data-slot=range-edge]")
    expect(edges).toHaveLength(1)
    expect((edges[0] as HTMLElement).style.borderColor).toBe("var(--ok)")
    expect(
      [...bar.querySelectorAll("[data-slot=range-bound]")].map(
        (b) => b.textContent
      )
    ).toEqual(["0 ng/mL", "3 ng/mL"])
  })

  it("draws no edge and prints no bound for a stats bar's bands", () => {
    const view = render(<RangeBar value={50} bands={thirds} />)
    const bar = view.container.querySelector("[data-slot=range-bar]")!
    expect(bar.querySelectorAll("[data-slot=range-edge]")).toHaveLength(0)
    expect(bar.querySelectorAll("[data-slot=range-bound]")).toHaveLength(0)
  })
})

describe("StatTile so far", () => {
  it("says the value so far beside the number and under a dial", () => {
    const number = render(
      <StatTile label="Strain" value="8.2" show="number" qualifier="so far" />
    )
    expect(
      number.container.querySelector("[data-slot=stat-qualifier]")?.textContent
    ).toBe("so far")
    cleanup()
    const dial = render(
      <StatTile
        label="Recovery"
        value="70"
        numeric={70}
        show="dial"
        min={0}
        max={100}
        qualifier="so far"
      />
    )
    expect(dial.container.textContent).toContain("so far")
  })
})
