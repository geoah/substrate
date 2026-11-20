import { cx } from "@substrate/react"
import type { ReactNode } from "react"
import { Badge } from "../Badge"
import type { Tone } from "../tone"

export interface ResultRow {
  key: string
  /** The first column: a date, or what was measured. */
  lead: ReactNode
  /** Under the lead on a phone, where its own column is not drawn: the
   * source. */
  leadNote?: string
  value: string
  /** A twin in another unit, after the value: "6.0%". */
  twin?: string
  /** The lab's range as it applied. */
  range?: string
  flag?: { word: string; tone: Tone }
  source?: string
  fasting?: boolean
  /** Where the row's report opens, a route's hash. */
  report?: string
}

/** Lab results as a table: bounded facts in columns. From 640 px every
 * column; on a phone the lead (with its source under it), the value (with
 * its flag and range under it) and the report. */
export function ResultTable(props: {
  label: string
  leadHeader: string
  rows: ResultRow[]
  /** Draw the Source and Fasting columns. */
  withSource?: boolean
  empty: ReactNode
}) {
  if (!props.rows.length) return <>{props.empty}</>
  const withReport = props.rows.some((r) => r.report)
  const th = "px-3 py-2 text-left text-[13px] font-normal text-secondary"
  const wide = "hidden sm:table-cell"
  return (
    <div className="overflow-hidden rounded-[10px] bg-card">
      <table className="w-full table-auto border-collapse text-[15px]">
        <caption className="sr-only">{props.label}</caption>
        <thead>
          <tr className="border-b border-separator/60">
            <th scope="col" className={cx(th, "pl-4")}>
              {props.leadHeader}
            </th>
            <th scope="col" className={cx(th, "text-right")}>
              Value
            </th>
            <th scope="col" className={cx(th, wide)}>
              Lab range
            </th>
            <th scope="col" className={cx(th, wide)}>
              Flag
            </th>
            {props.withSource && (
              <th scope="col" className={cx(th, "hidden md:table-cell")}>
                Source
              </th>
            )}
            {props.withSource && (
              <th scope="col" className={cx(th, "hidden lg:table-cell")}>
                Fasting
              </th>
            )}
            {withReport && (
              <th scope="col" className={cx(th, "pr-4")}>
                <span className="sr-only">Report</span>
              </th>
            )}
          </tr>
        </thead>
        <tbody>
          {props.rows.map((r) => (
            <tr
              key={r.key}
              className="border-b border-separator/40 align-top last:border-b-0"
            >
              <td className="py-2.5 pr-3 pl-4">
                <span className="block">{r.lead}</span>
                {r.leadNote && (
                  <span className="block text-[13px] text-secondary sm:hidden">
                    {r.leadNote}
                  </span>
                )}
              </td>
              <td className="px-3 py-2.5 text-right">
                <span className="block tabular-nums">{r.value}</span>
                {r.twin && (
                  <span className="block text-[13px] text-secondary tabular-nums">
                    {r.twin}
                  </span>
                )}
                <span className="flex flex-col items-end gap-0.5 sm:hidden">
                  {r.flag && <Badge label={r.flag.word} tone={r.flag.tone} />}
                  {r.range && (
                    <span className="text-[13px] text-secondary">
                      Lab {r.range}
                    </span>
                  )}
                </span>
              </td>
              <td className={cx("px-3 py-2.5 text-secondary", wide)}>
                {r.range}
              </td>
              <td className={cx("px-3 py-2.5", wide)}>
                {r.flag && <Badge label={r.flag.word} tone={r.flag.tone} />}
              </td>
              {props.withSource && (
                <td className="hidden px-3 py-2.5 text-secondary md:table-cell">
                  {r.source}
                </td>
              )}
              {props.withSource && (
                <td className="hidden px-3 py-2.5 text-secondary lg:table-cell">
                  {r.fasting === undefined ? "" : r.fasting ? "Yes" : "No"}
                </td>
              )}
              {withReport && (
                <td className="py-2.5 pr-4 pl-3 text-right">
                  {r.report && (
                    <a
                      href={r.report}
                      className="text-accent-text underline-offset-2 hover:underline"
                    >
                      Report
                    </a>
                  )}
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
