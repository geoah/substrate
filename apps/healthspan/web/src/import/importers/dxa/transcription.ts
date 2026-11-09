// A trend page typed by hand from a photo of the printout
// (apps/healthspan/docs/importers/04-dxa.md, A transcribed trend page): a
// JSON file beside the photo it names, its cells as the printout prints
// them. One Schema decodes it, every cell held to the printout's format.

import { Schema } from "effect"
import type { WallClock } from "../shared"
import type { Day } from "./measures"
import { decimal, grams, printedClock, printedDate } from "./printout"
import type { TrendRow } from "./printout"

const cell = (pattern: RegExp, message: string) =>
  Schema.String.pipe(Schema.check(Schema.isPattern(pattern, { message })))

const Date_ = cell(
  /^\d{1,2}\/\d{1,2}\/\d{4}$/,
  "is not a date as the printout writes one (D/M/YYYY)"
)
const Decimal_ = cell(
  /^\d+(,\d+)?$/,
  "is not a number as the printout writes one (a comma before the decimals)"
)
const Grams = cell(
  /^\d+(\.\d{3})*$/,
  "is not a mass in grams as the printout writes one (a point between thousands)"
)
const Centile = cell(/^(\d+|-)$/, "is not a centile or a dash")
const Time = cell(
  /^\d{1,2}:\d\d:\d\d (πμ|μμ)$/,
  "is not a 12-hour time as the printout writes one (πμ or μμ after it)"
)
const Stamp = cell(
  /^\d{1,2}\/\d{1,2}\/\d{4} \d{1,2}:\d\d:\d\d (πμ|μμ)$/,
  "is not a date and a 12-hour time as the printout writes them"
)

/** The transcribed page: its photo's file name, the scan's date, measured
 * and analysed times and scanner file name, and the two trend tables'
 * rows. `note`, the ages, the tissue grams and the distribution's total
 * %fat are not read. */
export const Transcription = Schema.Struct({
  photo: Schema.String,
  note: Schema.optional(Schema.String),
  scan: Date_,
  measured: Time,
  analyzed: Stamp,
  file: Schema.String,
  composition: Schema.Array(
    Schema.Struct({
      date: Date_,
      age: Schema.optional(Schema.String),
      tissue_fat: Decimal_,
      centile: Centile,
      body_mass: Decimal_,
      body_fat: Decimal_,
      tissue: Schema.optional(Schema.String),
      fat_mass: Grams,
      lean_mass: Grams,
      bone_mass: Grams,
      fat_free_mass: Grams,
    })
  ),
  distribution: Schema.Array(
    Schema.Struct({
      date: Date_,
      age: Schema.optional(Schema.String),
      android_fat: Decimal_,
      gynoid_fat: Decimal_,
      ag_ratio: Decimal_,
      tissue_fat: Schema.optional(Schema.String),
    })
  ),
})
export type Transcription = typeof Transcription.Type

/** Whether a parsed JSON file is a transcribed trend page by its marks:
 * the scan's date and the two trend tables. */
export function looksTranscribed(value: unknown): boolean {
  return (
    !!value &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    "scan" in value &&
    "composition" in value &&
    "distribution" in value
  )
}

/** A decoded page's scan and rows, or the field a date or a time is not
 * real in (the 30th of February). */
export function transcribed(t: Transcription):
  | {
      day: Day
      measured: WallClock
      analyzed: WallClock
      trend: TrendRow[]
    }
  | { field: string } {
  const day = printedDate(t.scan)
  if (!day) return { field: "scan" }
  const [time, half] = t.measured.split(" ")
  const [scanDate] = t.scan.split(" ")
  const measured = printedClock(scanDate, time, half)
  if (!measured) return { field: "measured" }
  const [aDate, aTime, aHalf] = t.analyzed.split(" ")
  const analyzed = printedClock(aDate, aTime, aHalf)
  if (!analyzed) return { field: "analyzed" }
  const trend: TrendRow[] = []
  for (const [i, r] of t.composition.entries()) {
    const date = printedDate(r.date)
    if (!date) return { field: `composition[${i}].date` }
    trend.push({
      date,
      values: {
        body_mass: decimal(r.body_mass),
        body_fat: decimal(r.body_fat),
        fat_centile: r.centile === "-" ? null : r.centile,
        tissue_fat: decimal(r.tissue_fat),
        fat_mass: grams(r.fat_mass),
        bone_mass: grams(r.bone_mass),
        lean_mass: grams(r.lean_mass),
        fat_free_mass: grams(r.fat_free_mass),
      },
    })
  }
  for (const [i, r] of t.distribution.entries()) {
    const date = printedDate(r.date)
    if (!date) return { field: `distribution[${i}].date` }
    trend.push({
      date,
      values: {
        android_fat: decimal(r.android_fat),
        gynoid_fat: decimal(r.gynoid_fat),
        ag_ratio: decimal(r.ag_ratio),
      },
    })
  }
  return { day, measured, analyzed, trend }
}
