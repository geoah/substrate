import { describe, expect, it } from "vitest"
import * as C from "./curves"
import { tenths } from "./format"

// Every (value, impact) pair readable in published screenshots of WHOOP's
// Healthspan screen. Ages come from WHOOP Age minus the shown delta; sex is
// assumed male (the VO2 max pair needs it). The fitted curves are not
// WHOOP's; they must land within a tenth of a year of each shown value.
const PAIRS: { src: string; age: number; got: number; want: number }[] = [
  {
    src: "cs consistency 68%",
    age: 24.1,
    got: C.sleepConsistencyYears(68),
    want: 0.3,
  },
  { src: "cs hours 7:03", age: 24.1, got: C.sleepHoursYears(7.05), want: 0 },
  {
    src: "cs zones 1-3 1:29",
    age: 24.1,
    got: C.zones13Years(89, 24.1),
    want: 0.1,
  },
  {
    src: "cs zones 4-5 0:01",
    age: 24.1,
    got: C.zones45Years(1, 24.1),
    want: 0.1,
  },
  { src: "cs strength 2:05", age: 24.1, got: C.strengthYears(125), want: -1.6 },
  {
    src: "cs steps 8,443",
    age: 24.1,
    got: C.stepsYears(8443, 24.1),
    want: -0.1,
  },
  { src: "cs vo2 45", age: 24.1, got: C.vo2Years(45, 24.1, "male"), want: 0.5 },
  { src: "cs rhr 69", age: 24.1, got: C.rhrYears(69, "male"), want: 1.0 },
  {
    src: "cs lean 80.3%",
    age: 24.1,
    got: C.leanYears(80.3, 24.1, "male"),
    want: 0,
  },
  {
    src: "comm1 zones 1-3 2:24",
    age: 46,
    got: C.zones13Years(144, 46),
    want: -0.4,
  },
  {
    src: "comm1 zones 4-5 0:14",
    age: 46,
    got: C.zones45Years(14, 46),
    want: 0,
  },
  {
    src: "comm2 zones 1-3 2:19",
    age: 46,
    got: C.zones13Years(139, 46),
    want: -0.3,
  },
  {
    src: "comm2 zones 4-5 0:19",
    age: 46,
    got: C.zones45Years(19, 46),
    want: 0,
  },
  {
    src: "comm2 strength 2:39",
    age: 46,
    got: C.strengthYears(159),
    want: -1.6,
  },
  {
    src: "comm2 steps 10,369",
    age: 46,
    got: C.stepsYears(10369, 46),
    want: -0.4,
  },
  // One member's app, one week, age 42.9, male.
  {
    src: "member consistency 71%",
    age: 42.9,
    got: C.sleepConsistencyYears(71),
    want: -0.1,
  },
  {
    src: "member hours 7:52",
    age: 42.9,
    got: C.sleepHoursYears(7 + 52 / 60),
    want: -0.2,
  },
  {
    src: "member zones 1-3 2:04",
    age: 42.9,
    got: C.zones13Years(124, 42.9),
    want: -0.2,
  },
  {
    src: "member zones 4-5 0:23",
    age: 42.9,
    got: C.zones45Years(23, 42.9),
    want: 0,
  },
  {
    src: "member strength 0:36",
    age: 42.9,
    got: C.strengthYears(36),
    want: 0.2,
  },
  {
    src: "member steps 7,700",
    age: 42.9,
    got: C.stepsYears(7700, 42.9),
    want: 0.1,
  },
  {
    src: "member vo2 44",
    age: 42.9,
    got: C.vo2Years(44, 42.9, "male"),
    want: -1.7,
  },
  { src: "member rhr 56", age: 42.9, got: C.rhrYears(56, "male"), want: -0.5 },
  {
    src: "member lean 86.4%",
    age: 42.9,
    got: C.leanYears(86.4, 42.9, "male"),
    want: 0,
  },
]

describe("screenshot calibration", () => {
  for (const p of PAIRS) {
    it(`${p.src} → ${p.want}`, () => {
      expect(Math.abs(p.got - p.want)).toBeLessThanOrEqual(0.1 + 1e-9)
    })
  }

  it("sums to the 2.4 years younger that member's app shows", () => {
    const own = PAIRS.filter((p) => p.src.startsWith("member"))
    expect(tenths(own.reduce((a, p) => a + tenths(p.got), 0))).toBe(-2.4)
  })

  it("sums to the 0.3 years older the same screenshot shows", () => {
    const cs = PAIRS.filter((p) => p.src.startsWith("cs"))
    expect(tenths(cs.reduce((a, p) => a + tenths(p.got), 0))).toBe(0.3)
  })
})

// White paper Table 2: 30-year-olds and their WHOOP Age delta.
function delta(sex: C.Sex, v: number[]): number {
  const [hrs, cons, steps, z13, z45, str, vo2, rhr, lean] = v
  return (
    C.sleepHoursYears(hrs) +
    C.sleepConsistencyYears(cons) +
    C.stepsYears(steps, 30) +
    C.zones13Years(z13, 30) +
    C.zones45Years(z45, 30) +
    C.strengthYears(str) +
    C.vo2Years(vo2, 30, sex) +
    C.rhrYears(rhr, sex) +
    C.leanYears(lean, 30, sex)
  )
}

describe("white paper anchors", () => {
  it("WHOOP member, male: about -1.6", () => {
    expect(
      delta("male", [7.0, 67, 10900, 141, 6, 46, 46.8, 58, 81])
    ).toBeCloseTo(-1.6, 0)
  })
  it("average US adult, male: about +6", () => {
    expect(
      Math.abs(delta("male", [6.35, 65, 5200, 69, 0, 0, 42.4, 67, 73.9]) - 6)
    ).toBeLessThan(1)
  })
  it("WHOOP member, female: about -1.6", () => {
    expect(
      delta("female", [7.3, 68, 11500, 124, 7, 56, 40.0, 63, 73])
    ).toBeCloseTo(-1.6, 0)
  })
  it("average US adult, female: about +7.5", () => {
    expect(
      Math.abs(
        delta("female", [6.35, 65, 5000, 46, 0, 0, 30.2, 72, 62.2]) - 7.5
      )
    ).toBeLessThan(1)
  })
})

describe("curve shape", () => {
  it("is neutral at each documented target", () => {
    expect(C.sleepConsistencyYears(70)).toBe(0)
    expect(C.sleepHoursYears(7)).toBeCloseTo(0)
    expect(C.zones13Years(C.zones13Target(30), 30)).toBeCloseTo(0)
    expect(C.zones45Years(C.zones45Target(30), 30)).toBeCloseTo(0)
    expect(C.strengthYears(40)).toBeCloseTo(0)
    expect(C.stepsYears(8000, 30)).toBeCloseTo(0)
    expect(C.stepsYears(5600, 65)).toBeCloseTo(0)
    expect(C.vo2Years(C.vo2Target(30, "male"), 30, "male")).toBeCloseTo(0)
    expect(C.rhrYears(60, "male")).toBe(0)
    expect(C.leanYears(80, 30, "male")).toBe(0)
  })

  it("never rewards less activity or worse fitness", () => {
    const mono = (
      f: (x: number) => number,
      from: number,
      to: number,
      step: number
    ) => {
      for (let x = from; x < to; x += step)
        expect(f(x + step)).toBeLessThanOrEqual(f(x) + 1e-12)
    }
    mono((x) => C.zones13Years(x, 35), 0, 900, 5)
    mono((x) => C.zones45Years(x, 35), 0, 200, 1)
    mono(C.strengthYears, 0, 300, 5)
    mono((x) => C.stepsYears(x, 35), 0, 25000, 100)
    mono((x) => C.vo2Years(x, 35, "female"), 10, 80, 0.5)
    mono((x) => -C.rhrYears(x, "male"), 30, 110, 1)
  })

  it("treats sleep past nine hours as neutral", () => {
    expect(C.sleepHoursYears(9.5)).toBe(0)
    expect(C.sleepHoursYears(11)).toBe(0)
  })

  it("caps the credits nothing published constrains", () => {
    expect(C.vo2Years(70, 25, "male")).toBe(-3)
    expect(C.rhrYears(35, "male")).toBe(-1.5)
    expect(C.leanYears(95, 30, "male")).toBe(0)
    expect(C.stepsYears(0, 30)).toBeLessThan(3.5)
  })
})
