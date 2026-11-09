// What the Healthspan screens and Today read, worked out once per data,
// owner and day: the age score (only with a birth date and sex), the
// weekly averages either way, and the findings.

import type { Sex } from "../engine/curves"
import { computeReport } from "../engine/engine"
import type { Report } from "../engine/report"
import { evaluateHealthspan, syncFindings } from "../rules/healthspan"
import { sortFindings, type Finding } from "../rules/types"
import type { HealthspanSeries } from "./read"

/** The owner as the age score reads them. */
export interface HealthspanProfile {
  birthDate?: string
  sex?: Sex
}

export interface HealthspanView {
  series: HealthspanSeries
  /** The age score; undefined without a birth date and sex. */
  report?: Report
  /** The weekly averages, with or without a birth date and sex, so the
   * cards show values when there are no targets. */
  averages: Report
  profile: HealthspanProfile
  /** Every Healthspan finding, the sync ones included, most urgent
   * first. */
  findings: Finding[]
  today: Date
}

/** A stand-in owner for the averages alone, which do not depend on age or
 * sex; never shown as anyone's. */
const STAND_IN = { birthDate: "2000-01-01", sex: "male" } as const

export function healthspanView(
  series: HealthspanSeries,
  profile: HealthspanProfile,
  today: Date
): HealthspanView {
  const st =
    profile.birthDate && profile.sex
      ? { birthDate: profile.birthDate, sex: profile.sex }
      : undefined
  const report = st ? computeReport(series.input, st, today) : undefined
  const averages = report ?? computeReport(series.input, STAND_IN, today)
  const findings = sortFindings([
    ...evaluateHealthspan(series, report, today, profile.sex),
    ...syncFindings(series, today),
  ])
  return { series, report, averages, profile, findings, today }
}
