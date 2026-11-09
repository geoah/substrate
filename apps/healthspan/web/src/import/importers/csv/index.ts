// A CSV or TSV of lab results, one row per result (docs/plans/healthspan.md,
// Import), as an importer: the file's columns recognised by their headers,
// each row matched to a catalogue entry and a metric, and the ticked rows
// planned as reports and results in the import's own namespace, one import
// per lab. The import screen reads it through its own table (the column
// picker, the day order, the metric picks and the ticks), which hands its
// state to `planRows`; `read` is the same reading with the screen's choices
// as answers, every row that can be written ticked.

import { Effect } from "effect"
import { CATALOGUE } from "../../../catalogue"
import { convert } from "../../../labs/units"
import { ROLES, detectColumns, missingRoles, roleLabel } from "../../columns"
import type { ColumnRole } from "../../columns"
import { readTable, type Table } from "../../csv"
import { buildPlan, type LabPlan } from "../../plan"
import {
  matchRows,
  readRows,
  tickedAtFirst,
  type ResolvedRow,
} from "../../resolve"
import { detectDayOrder, type DayOrder } from "../../values"
import type {
  Context,
  ImportError,
  Importer,
  Input,
  InputFile,
  Plan,
  Question,
} from "../types"

const EXTENSIONS = [".csv", ".tsv", ".txt"]

/** The keys of the answers `read` takes: the screen's choices. */
export const CSV_ANSWERS = {
  /** Each column's role, comma separated, in the file's order. */
  columns: "columns",
  /** `dmy` or `mdy`, where every date reads both ways. */
  dayOrder: "dayOrder",
  /** The lab of the rows that name none. */
  lab: "lab",
  /** The owner's metric for a test nothing knows: JSON, the test's name
   * (as `pickKey` keys it) to the metric's id. */
  picks: "picks",
} as const

/** The one file a CSV import reads: a single file of a table's type. */
export function tableFile(input: Input): InputFile | undefined {
  if (input.files.length !== 1) return undefined
  const f = input.files[0]
  const name = f.path.toLowerCase()
  if (EXTENSIONS.some((e) => name.endsWith(e))) return f
  if (/^text\/(csv|tab-separated-values|plain)/.test(f.type)) return f
  return undefined
}

/** A file's table, read in the browser. */
export async function tableOf(file: InputFile): Promise<Table> {
  return readTable(await file.text(), file.path)
}

/** The plans the rows the owner ticked make: one per lab, split at 2,000
 * rows. */
export function planRows(
  rows: readonly ResolvedRow[],
  ticked: ReadonlySet<number>,
  name: string
): LabPlan[] {
  return buildPlan(rows, ticked, { name, importer: "csv" })
}

function rolesOf(table: Table, answer: string | undefined): ColumnRole[] {
  if (!answer) return detectColumns(table.headers)
  const known = new Set(ROLES.map((r) => r.role))
  return answer
    .split(",")
    .map((r) => r.trim() as ColumnRole)
    .map((r) => (known.has(r) ? r : "ignore"))
}

const dayOrderQuestion: Question = {
  key: CSV_ANSWERS.dayOrder,
  label: "Which comes first in the dates",
  help: "Every date in the file reads both ways, such as 03/04/2026: dmy for the day, mdy for the month.",
  kind: "text",
  required: true,
}

const labQuestion: Question = {
  key: CSV_ANSWERS.lab,
  label: "Lab",
  help: "The file names no lab for some rows. Their results become a source of their own, or the one of that name you already have.",
  kind: "text",
  required: true,
}

function dayOrderOf(
  table: Table,
  roles: ColumnRole[],
  answer: string | undefined
): DayOrder | undefined {
  if (answer === "dmy" || answer === "mdy") return answer
  const at = roles.indexOf("date")
  return at < 0 ? "dmy" : detectDayOrder(table.rows.map((r) => r[at]))
}

function needsLab(table: Table, roles: ColumnRole[]): boolean {
  const at = roles.indexOf("lab")
  return at < 0 || table.rows.some((r) => !r[at]?.trim())
}

/** The lab plans a file makes with the answers given: every row that can
 * be written ticked, nothing marked as maybe already here. */
export function readAll(
  input: Input,
  ctx: Context
): Effect.Effect<LabPlan[], ImportError> {
  return Effect.gen(function* () {
    const file = tableFile(input)
    if (!file)
      return yield* Effect.fail<ImportError>({
        _tag: "NotMine",
        message: "It reads one CSV or TSV file.",
      })
    const table = yield* Effect.promise(() => tableOf(file))
    if (!table.headers.length || !table.rows.length)
      return yield* Effect.fail<ImportError>({
        _tag: "Unreadable",
        path: file.path,
        message: "It holds no rows under a header row.",
      })
    const roles = rolesOf(table, ctx.answers[CSV_ANSWERS.columns])
    const missing = missingRoles(roles)
    if (missing.length)
      return yield* Effect.fail<ImportError>({
        _tag: "Unreadable",
        path: file.path,
        field: missing[0],
        message: `No column holds the ${missing.map((r) => roleLabel(r).toLowerCase()).join(" or the ")}.`,
      })
    const order = dayOrderOf(table, roles, ctx.answers[CSV_ANSWERS.dayOrder])
    const lab = ctx.answers[CSV_ANSWERS.lab]?.trim() ?? ""
    const asks = [
      ...(order ? [] : [dayOrderQuestion]),
      ...(needsLab(table, roles) && !lab ? [labQuestion] : []),
    ]
    if (asks.length)
      return yield* Effect.fail<ImportError>({
        _tag: "NeedsAnswers",
        questions: asks,
      })
    let picks = new Map<string, string>()
    const pickText = ctx.answers[CSV_ANSWERS.picks]
    if (pickText) {
      try {
        picks = new Map(
          Object.entries(JSON.parse(pickText) as Record<string, string>)
        )
      } catch {
        // No picks: a test nothing knows is left out.
      }
    }
    const metrics = ctx.metrics.filter(
      (m) => m.category === "labs" || m.category === "body"
    )
    const rows = yield* Effect.promise(() =>
      matchRows(readRows(table, roles, order!, lab), {
        catalogue: CATALOGUE,
        metrics,
        picks,
        convert: (value, from, to) => {
          const v = convert(ctx.units, Number(value), from, to)
          return Promise.resolve(v === undefined ? undefined : String(v))
        },
        held: () => Promise.resolve([]),
      })
    )
    const plans = planRows(
      rows,
      new Set(rows.filter(tickedAtFirst).map((r) => r.line)),
      input.name || file.path
    )
    if (!plans.length)
      return yield* Effect.fail<ImportError>({
        _tag: "Unreadable",
        path: file.path,
        message: `None of its ${rows.length.toLocaleString("en-GB")} rows can be written.`,
      })
    return plans
  })
}

export const csv: Importer = {
  id: "csv",
  name: "A CSV of lab results",
  description:
    "A CSV or TSV file of lab results, one row per result: a date, the test's name or LOINC code and the value, with the unit, the lab's range and flag where it has them.",
  accepts: { kind: "file", extensions: EXTENSIONS },
  detect: (input) =>
    Effect.gen(function* () {
      const file = tableFile(input)
      if (!file) return undefined
      const table = yield* Effect.promise(() => tableOf(file))
      if (table.headers.length < 2 || !table.rows.length) return undefined
      const roles = detectColumns(table.headers)
      const rows = `${table.rows.length.toLocaleString("en-GB")} ${table.rows.length === 1 ? "row" : "rows"}`
      return missingRoles(roles).length
        ? {
            importer: "csv" as const,
            confidence: 0.3,
            why: `A table of ${rows} whose columns you name.`,
          }
        : {
            importer: "csv" as const,
            confidence: 0.6,
            why: `A table of ${rows} of lab results.`,
          }
    }),
  questions: (input) =>
    Effect.gen(function* () {
      const file = tableFile(input)
      if (!file) return []
      const table = yield* Effect.promise(() => tableOf(file))
      const roles = detectColumns(table.headers)
      return [
        ...(dayOrderOf(table, roles, undefined) ? [] : [dayOrderQuestion]),
        ...(needsLab(table, roles) ? [labQuestion] : []),
      ]
    }),
  read: (input, ctx) =>
    Effect.gen(function* () {
      const plans = yield* readAll(input, ctx)
      if (plans.length > 1)
        return yield* Effect.fail<ImportError>({
          _tag: "Unreadable",
          path: input.name,
          message: `It holds results of ${plans.length} labs or more than 2,000 rows, so it makes ${plans.length} imports: import it through its table.`,
        })
      return plans[0].plan satisfies Plan
    }),
}
