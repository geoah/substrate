// One result typed in (docs/plans/healthspan.md, Import: Enter a result), as
// an importer: no files, its questions the form's fields, and its plan one
// report and one result in the import's own namespace, written by the same
// runner as a file, so it can be undone like one.

import { Effect } from "effect"
import { CATALOGUE } from "../../../catalogue"
import { convert } from "../../../labs/units"
import type { ColumnRole } from "../../columns"
import { buildPlan } from "../../plan"
import { matchRows, pickKey, readRows } from "../../resolve"
import type { Context, ImportError, Importer, Input, Question } from "../types"

/** The answers a hand entry takes, by `Question.key`. */
export const MANUAL_ANSWERS = {
  /** The metric's id, such as `labs/ldl_cholesterol`. */
  metric: "metric",
  /** The day taken, `YYYY-MM-DD`. */
  day: "day",
  /** The value: a number, a bound such as `<0.5`, or words. */
  value: "value",
  unit: "unit",
  low: "low",
  high: "high",
  /** `H`, `L`, `A` or none. */
  flag: "flag",
  lab: "lab",
} as const

const QUESTIONS: readonly Question[] = [
  { key: "metric", label: "Test", kind: "text", required: true },
  { key: "day", label: "Date taken", kind: "text", required: true },
  {
    key: "value",
    label: "Value",
    help: "Such as 5.2, <0.5 or Not detected.",
    kind: "text",
    required: true,
  },
  { key: "unit", label: "Unit", kind: "text", required: false },
  { key: "low", label: "Low", kind: "text", required: false },
  { key: "high", label: "High", kind: "text", required: false },
  { key: "flag", label: "Flag", kind: "text", required: false },
  {
    key: "lab",
    label: "Lab",
    help: "The lab becomes a source of its own, or the one of that name you already have.",
    kind: "text",
    required: true,
  },
]

/** A hand entry takes no files. */
export const NO_INPUT: Input = { name: "Entered by hand", files: [] }

const ROLES: ColumnRole[] = [
  "date",
  "name",
  "value",
  "unit",
  "low",
  "high",
  "flag",
  "lab",
]

export const manual: Importer = {
  id: "manual",
  name: "Enter a result",
  description:
    "One result typed in: the test, the day it was taken, the value and the lab, with its unit, range and flag where you have them.",
  accepts: { kind: "files", extensions: [] },
  detect: () => Effect.succeed(undefined),
  questions: () => Effect.succeed(QUESTIONS),
  read: (_input: Input, ctx: Context) =>
    Effect.gen(function* () {
      const a = ctx.answers
      const missing = QUESTIONS.filter((q) => q.required && !a[q.key]?.trim())
      if (missing.length)
        return yield* Effect.fail<ImportError>({
          _tag: "NeedsAnswers",
          questions: missing,
        })
      const chosen = ctx.metrics.find((m) => m.id === a.metric)
      if (!chosen)
        return yield* Effect.fail<ImportError>({
          _tag: "Unreadable",
          path: "",
          field: "metric",
          message: "The repository has no such test.",
        })
      const lab = a.lab.trim()
      const table = {
        delimiter: "," as const,
        headers: ROLES,
        rows: [
          [
            a.day,
            chosen.name,
            a.value,
            a.unit ?? "",
            a.low ?? "",
            a.high ?? "",
            a.flag ?? "",
            lab,
          ],
        ],
      }
      const [row] = yield* Effect.promise(() =>
        matchRows(readRows(table, ROLES, "dmy", lab), {
          catalogue: CATALOGUE,
          metrics: [chosen],
          picks: new Map([[pickKey({ name: chosen.name }), chosen.id]]),
          convert: (value, from, to) => {
            const v = convert(ctx.units, Number(value), from, to)
            return Promise.resolve(v === undefined ? undefined : String(v))
          },
          held: () => Promise.resolve([]),
        })
      )
      if (row.problem)
        return yield* Effect.fail<ImportError>({
          _tag: "Unreadable",
          path: "",
          field: "value",
          message: row.problem,
        })
      const [planned] = buildPlan([row], new Set([row.line]), {
        name: "Entered by hand",
        importer: "manual",
      })
      return planned.plan
    }),
}
