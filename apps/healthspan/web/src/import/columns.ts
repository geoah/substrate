// What each column of a lab file holds, recognised by its header and
// reassignable by the owner (docs/plans/healthspan.md, Import).

import { csvField } from "./csv"

export type ColumnRole =
  | "date"
  | "name"
  | "loinc"
  | "value"
  | "unit"
  | "range"
  | "low"
  | "high"
  | "flag"
  | "lab"
  | "accession"
  | "ignore"

/** The roles in the order the column picker lists them, with their words. */
export const ROLES: { role: ColumnRole; label: string }[] = [
  { role: "date", label: "Date" },
  { role: "name", label: "Test" },
  { role: "loinc", label: "LOINC code" },
  { role: "value", label: "Value" },
  { role: "unit", label: "Unit" },
  { role: "range", label: "Range" },
  { role: "low", label: "Range low" },
  { role: "high", label: "Range high" },
  { role: "flag", label: "Flag" },
  { role: "lab", label: "Lab" },
  { role: "accession", label: "Accession" },
  { role: "ignore", label: "Not used" },
]

export function roleLabel(role: ColumnRole): string {
  return ROLES.find((r) => r.role === role)?.label ?? role
}

/** Lowercase letters and digits, words one space apart: how a header, a
 * test's name and an alias compare. */
export function normalise(s: string): string {
  return s
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .trim()
}

// Each role's headers, normalised; the first role whose list holds a
// header takes it, so "result date" is a date before it is a value.
const HEADERS: [ColumnRole, string[]][] = [
  [
    "date",
    [
      "date",
      "datetime",
      "date time",
      "collected",
      "collection date",
      "collected at",
      "collected on",
      "date collected",
      "sample date",
      "specimen date",
      "test date",
      "result date",
      "observation date",
      "taken",
      "taken at",
      "drawn",
      "date drawn",
      "when",
    ],
  ],
  ["loinc", ["loinc", "loinc code", "loinc number", "loinc id"]],
  [
    "name",
    [
      "test",
      "test name",
      "analyte",
      "analyte name",
      "name",
      "marker",
      "biomarker",
      "parameter",
      "component",
      "investigation",
      "measurement",
      "observation",
      "description",
      "item",
    ],
  ],
  [
    "value",
    [
      "value",
      "result",
      "result value",
      "results",
      "reading",
      "numeric value",
      "measured value",
      "level",
    ],
  ],
  ["unit", ["unit", "units", "uom", "unit of measure", "result unit"]],
  [
    "range",
    [
      "range",
      "reference range",
      "ref range",
      "reference",
      "normal range",
      "reference interval",
      "ref interval",
      "normal",
      "normal values",
      "reference values",
    ],
  ],
  [
    "low",
    [
      "low",
      "ref low",
      "range low",
      "reference low",
      "lower",
      "lower limit",
      "lower bound",
      "min",
      "minimum",
      "normal low",
    ],
  ],
  [
    "high",
    [
      "high",
      "ref high",
      "range high",
      "reference high",
      "upper",
      "upper limit",
      "upper bound",
      "max",
      "maximum",
      "normal high",
    ],
  ],
  [
    "flag",
    [
      "flag",
      "flags",
      "abnormal flag",
      "abnormal",
      "interpretation",
      "h l",
      "result flag",
    ],
  ],
  [
    "lab",
    [
      "lab",
      "laboratory",
      "lab name",
      "performing lab",
      "provider",
      "source",
      "clinic",
    ],
  ],
  [
    "accession",
    [
      "accession",
      "accession number",
      "accession no",
      "report",
      "report id",
      "report number",
      "report no",
      "specimen id",
      "sample id",
      "order number",
      "order id",
      "requisition",
    ],
  ],
]

/** One role per column from its header, each role taken by its first
 * column only; anything unknown is not used. */
export function detectColumns(headers: string[]): ColumnRole[] {
  const taken = new Set<ColumnRole>()
  return headers.map((h) => {
    const n = normalise(h)
    for (const [role, names] of HEADERS) {
      if (taken.has(role)) continue
      if (names.includes(n)) {
        taken.add(role)
        return role
      }
    }
    // A header naming its unit in brackets ("Value (mmol/L)") is still a
    // value.
    const stem = normalise(h.replace(/\(.*\)|\[.*\]/g, ""))
    for (const [role, names] of HEADERS) {
      if (taken.has(role)) continue
      if (stem && names.includes(stem)) {
        taken.add(role)
        return role
      }
    }
    return "ignore"
  })
}

/** What the file still lacks for a row to be read: a date, a test and a
 * value, each by some column. */
export function missingRoles(roles: ColumnRole[]): ColumnRole[] {
  const need: ColumnRole[] = ["date", "value"]
  const missing = need.filter((r) => !roles.includes(r))
  if (!roles.includes("name") && !roles.includes("loinc")) missing.push("name")
  return missing
}

/** The downloadable template: one row per column the import reads, every
 * form a range takes shown once. */
export function templateCsv(): string {
  const rows = [
    [
      "Date",
      "Test",
      "LOINC",
      "Value",
      "Unit",
      "Range",
      "Flag",
      "Lab",
      "Accession",
    ],
    [
      "2026-03-12",
      "LDL cholesterol",
      "22748-8",
      "3.4",
      "mmol/L",
      "0 - 3.0",
      "H",
      "Example Lab",
      "A1001",
    ],
    [
      "2026-03-12",
      "HDL cholesterol",
      "",
      "1.6",
      "mmol/L",
      ">1.0",
      "",
      "Example Lab",
      "A1001",
    ],
    [
      "2026-03-12",
      "C-reactive protein",
      "",
      "<0.6",
      "mg/L",
      "<5",
      "",
      "Example Lab",
      "A1001",
    ],
  ]
  return rows.map((r) => r.map(csvField).join(",")).join("\r\n") + "\r\n"
}
