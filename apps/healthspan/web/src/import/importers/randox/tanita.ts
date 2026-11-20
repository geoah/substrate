// The body composition sheet a Randox visit publishes beside its health
// report: a Tanita bio-impedance printout whose PDF no reader opens whole,
// read from its raw content stream (shared/pdfstream) by the positions of
// its text next to fixed labels (apps/healthspan/docs/importers/
// 02-randox.md, The body composition sheet). A sheet whose layout is not
// this one stops the read rather than reading a wrong value.

import { Effect } from "effect"
import type { ImportError } from "../types"
import {
  glyphOffset,
  parseClock,
  readTextRuns,
  unreadable,
  zoned,
  type AnalyteRef,
  type TextRun,
  type WallClock,
} from "../shared"

/** The zone the sheet's printed time is in: the clinic's. */
export const SHEET_ZONE = "Europe/London"

/** One value of the sheet. */
export interface SheetRow {
  /** The measure, as the sheet labels it (`Muscle Mass`), which the
   * result's key names. */
  measure: string
  /** The row's own name, its region or limb included (`Muscle Mass Left
   * Arm`, `Phase Angle H-L (50 kHz)`), its coding. */
  name: string
  analyte: AnalyteRef
  /** The digits as printed. */
  value: string
  unit: string
  /** The scanner's desirable range, where the sheet prints one. */
  low?: string
  high?: string
  /** The body region (`trunk`, `left_arm`), or the limb or legs of a phase
   * angle (`right_leg`, `leg_to_leg`); none for the whole body. */
  channel?: string
}

/** What a sheet says. */
export interface Sheet {
  /** The scanner's id for the person, printed beside `ID`. */
  id: string
  /** When it was printed, as the clock showed it. */
  printed: WallClock
  /** The printed time with its London offset. */
  at: string
  /** The printed time as `YYYY-MM-DDTHH:MM`. */
  minute: string
  /** The scanner's model as the sheet prints it (`MC-780`). */
  model?: string
  rows: SheetRow[]
}

/** The sheet's fonts are subsets whose glyph ids follow the standard order
 * 29 below the ASCII code. */
const GLYPHS = glyphOffset(29)

const NUMBER = /^-?\d+(\.\d+)?$/
const health = (id: string): AnalyteRef => ({ metric: `health/${id}` })

/** The sheet's layout is not the one this reader reads. */
class Layout extends Error {}

/** The sheet's values from its text runs. Throws `Layout` where a label or
 * a value is not where the sheet puts it. */
export function sheetOf(runs: readonly TextRun[]): Sheet {
  const label = (
    text: string,
    o: { xmax?: number; xmin?: number; font?: string } = {}
  ): TextRun => {
    const hit = runs.find(
      (r) =>
        r.text === text &&
        (o.xmax === undefined || r.x < o.xmax) &&
        (o.xmin === undefined || r.x >= o.xmin) &&
        (o.font === undefined || r.font === o.font)
    )
    if (!hit) throw new Layout(`the label ${text} is not where it was`)
    return hit
  }
  const near = (
    what: string,
    y: number,
    x0: number,
    x1: number,
    o: { dy?: number; pattern?: RegExp; below?: number } = {}
  ): TextRun => {
    const dy = o.dy ?? 3
    const pattern = o.pattern ?? NUMBER
    const at = y - (o.below ?? 0)
    const hits = runs.filter(
      (r) =>
        Math.abs(r.y - at) <= dy &&
        x0 <= r.x &&
        r.x <= x1 &&
        pattern.test(r.text)
    )
    if (hits.length !== 1)
      throw new Layout(
        `${hits.length ? "more than one value" : "no value"} for ${what}`
      )
    return hits[0]
  }

  const stamp = runs.find((r) => /^\d\d\/\d\d\/\d{4} \d\d:\d\d$/.test(r.text))
  const printed = stamp && parseClock(stamp.text, "%d/%m/%Y %H:%M")
  if (!printed) throw new Layout("no printed time")
  const at = zoned(printed, SHEET_ZONE)
  if (!at) throw new Layout("no printed time")
  const minute = at.slice(0, 16)
  const id = near("the ID", label("ID", { xmax: 80 }).y, 80, 120, {
    pattern: /^\d+$/,
  }).text
  const model = runs.find((r) => /^MC-\d+[A-Za-z]*$/.test(r.text))?.text

  const rows: SheetRow[] = []
  const add = (
    measure: string,
    analyte: AnalyteRef,
    value: string,
    unit: string,
    more: Partial<SheetRow> = {}
  ) => rows.push({ measure, name: measure, analyte, value, unit, ...more })

  // The result box: a value right of each label, its desirable range
  // further right on the same line.
  const main: [string, string, AnalyteRef, string][] = [
    ["Weight", "Weight", { entry: "body_mass" }, "kg"],
    ["Fat", "Fat %", health("body_fat_fraction"), "%"],
    ["Fat Mass", "Fat Mass", health("fat_mass"), "kg"],
    ["FFM", "FFM", health("fat_free_mass"), "kg"],
    ["Muscle Mass", "Muscle Mass", health("muscle_mass"), "kg"],
    ["BMI", "BMI", { entry: "bmi" }, "kg/m2"],
  ]
  for (const [text, measure, analyte, unit] of main) {
    const l = label(text, { xmax: 100, font: "F2" })
    const v = near(measure, l.y, 125, 150, { dy: 2.5 })
    const range = runs.find(
      (r) =>
        Math.abs(r.y - v.y) <= 1.5 &&
        r.x >= 180 &&
        r.x <= 200 &&
        /^[\d.]+-[\d.]+$/.test(r.text)
    )
    const [low, high] = range ? range.text.split("-") : []
    add(measure, analyte, v.text, unit, low && high ? { low, high } : {})
  }
  add(
    "Metabolic Age",
    health("metabolic_age"),
    near("Metabolic Age", label("Metabolic Age", { xmax: 100 }).y, 125, 150)
      .text,
    "a"
  )
  const kcal = label("kcal")
  add(
    "BMR",
    health("basal_metabolic_rate"),
    near("BMR", kcal.y, 120, kcal.x - 1, { dy: 1 }).text,
    "kcal/d"
  )
  add(
    "Visceral Fat Rating",
    health("visceral_fat_rating"),
    near("Visceral Fat Rating", label("Rating", { xmax: 100 }).y, 125, 150, {
      dy: 1.5,
    }).text,
    "{score}"
  )
  // Total body water, in kg and in %, left of each unit within ten units
  // of the TBW label's line.
  const tbw = label("TBW", { xmax: 100, font: "F2" })
  const unitBeside = (unit: string) => {
    const u = runs.find(
      (r) => r.text === unit && Math.abs(r.y - tbw.y) < 10 && r.x < 180
    )
    if (!u) throw new Layout(`the label ${unit} is not beside TBW`)
    return u
  }
  const kg = unitBeside("kg")
  add(
    "TBW",
    health("body_water_mass"),
    near("TBW", kg.y, 120, kg.x - 1, { dy: 1 }).text,
    "kg"
  )
  const pct = unitBeside("%")
  add(
    "TBW %",
    health("body_water_fraction"),
    near("TBW %", pct.y, 120, pct.x - 1, { dy: 1 }).text,
    "%"
  )
  for (const [text, analyte] of [
    ["ECW", health("extracellular_water")],
    ["ICW", health("intracellular_water")],
  ] as const) {
    const l = label(text, { xmin: 150, xmax: 300, font: "F2" })
    add(text, analyte, near(text, l.y, l.x + 5, l.x + 45, { dy: 2 }).text, "kg")
  }
  add(
    "ECW/TBW",
    health("extracellular_water_fraction"),
    near("ECW/TBW", label("ECW/TBW", { xmax: 100 }).y, 125, 150).text,
    "%"
  )
  // The top box: a value about ten units under each label.
  for (const [text, analyte] of [
    ["Bone Mass", health("bone_mass")],
    ["Protein", health("protein_mass")],
  ] as const) {
    const l = label(text, { xmin: 300, font: "F4" })
    add(
      text,
      analyte,
      near(text, l.y, l.x - 5, l.x + 20, { dy: 12, below: 10.3 }).text,
      "kg"
    )
  }
  const lms = label("Leg Muscle Score")
  const scores = runs.filter(
    (r) =>
      lms.y - r.y >= 60 &&
      lms.y - r.y <= 130 &&
      Math.abs(r.x - lms.x) < 40 &&
      /^\d+$/.test(r.text)
  )
  if (scores.length !== 1)
    throw new Layout(
      `${scores.length ? "more than one value" : "no value"} for Leg Muscle Score`
    )
  add("Leg Muscle Score", health("leg_muscle_score"), scores[0].text, "{score}")

  // The segmental analysis: each region's muscle mass on the left half,
  // its fat % and fat mass on the right, under the region's label.
  const muscle = /^(\d+\.\d)kg$/
  const fat = /^(\d+\.\d)% \/ (\d+\.\d)kg$/
  for (const region of [
    "Trunk",
    "Left Arm",
    "Right Arm",
    "Left Leg",
    "Right Leg",
  ]) {
    const channel = region.toLowerCase().replace(/ /g, "_")
    for (const [left, pattern] of [
      [true, muscle],
      [false, fat],
    ] as const) {
      const half = (x: number) => (left ? x < 300 : x >= 300)
      const labels = runs.filter(
        (r) => r.text === region && r.font === "F4" && half(r.x)
      )
      if (labels.length !== 1)
        throw new Layout(`the segment label ${region} is not where it was`)
      const l = labels[0]
      const hit = runs
        .filter(
          (r) =>
            l.y - r.y >= 5 &&
            l.y - r.y <= 15 &&
            half(r.x) &&
            pattern.test(r.text)
        )
        .map((r, i) => ({ r, i }))
        .sort(
          (a, b) => Math.abs(a.r.x - l.x) - Math.abs(b.r.x - l.x) || a.i - b.i
        )[0]?.r
      if (!hit) throw new Layout(`no segment value under ${region}`)
      const m = pattern.exec(hit.text)!
      if (left)
        add("Muscle Mass", health("muscle_mass"), m[1], "kg", {
          name: `Muscle Mass ${region}`,
          channel,
        })
      else {
        add("Fat %", health("body_fat_fraction"), m[1], "%", {
          name: `Fat % ${region}`,
          channel,
        })
        add("Fat Mass", health("fat_mass"), m[2], "kg", {
          name: `Fat Mass ${region}`,
          channel,
        })
      }
    }
  }

  // Phase angles at 50 kHz: hand to leg (the whole body), each limb, and
  // leg to leg, the degrees far right on each code's line.
  const limbs: [string, string | undefined, string][] = [
    ["H-L", undefined, "H-L (50 kHz)"],
    ["RL", "right_leg", "Right Leg"],
    ["LL", "left_leg", "Left Leg"],
    ["RH", "right_arm", "Right Arm"],
    ["LH", "left_arm", "Left Arm"],
    ["L-L", "leg_to_leg", "Leg-Leg"],
  ]
  for (const [code, channel, words] of limbs) {
    const l = label(code, { xmin: 380, xmax: 410 })
    const v = runs.filter((r) => Math.abs(r.y - l.y) <= 1.5 && r.x > 540)
    const deg = v.length === 1 ? /^(\d+\.\d)/.exec(v[0].text)?.[1] : undefined
    if (!deg) throw new Layout(`no phase angle for ${code}`)
    add("Phase Angle", health("phase_angle"), deg, "deg", {
      name: `Phase Angle ${words}`,
      ...(channel ? { channel } : {}),
    })
  }
  return { id, printed, at, minute, ...(model ? { model } : {}), rows }
}

/** A body composition sheet's values, read from its PDF; `Unreadable`
 * naming the file where it shows no text or its layout is not this one. */
export function readSheet(
  bytes: Uint8Array,
  path: string
): Effect.Effect<Sheet, ImportError> {
  return readTextRuns(bytes, path, GLYPHS).pipe(
    Effect.flatMap((runs) =>
      Effect.try({
        try: () => sheetOf(runs),
        catch: (e) =>
          unreadable(
            path,
            `the body composition sheet is not in the layout this importer reads: ${
              e instanceof Layout ? e.message : String(e)
            }`
          ),
      })
    )
  )
}
