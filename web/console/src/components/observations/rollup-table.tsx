/** The numbers under a chart: one line per row of the answer (a bucket, or
 * one group of a bucket) with its window, one column per reducer with its
 * unit, the count, the covered time, the sources that fed it and whether
 * raw points or a vendor summary served it. A row the server filled in or
 * could not answer says so. The whole table copies as tab-separated text,
 * the exact digits and the wire's instants. */

import type { ReactNode } from "react"

import { CopyButton } from "@/components/identity/copy-button"
import { EmptyValue } from "@/components/identity/empty-value"
import { Pill } from "@/components/identity/pill"
import { Reading } from "@/components/observations/marks"
import type {
  AggregateGroupKey,
  AggregateReducer,
  AggregateRow,
} from "@/lib/api/types"
import {
  coverageWords,
  reducerKey,
  reducerLabel,
  rollupText,
  rowValue,
  windowWords,
} from "@/lib/observations"
import { unitSymbol } from "@/lib/units"

const FROM_WORDS: Record<string, string> = {
  raw: "Raw points",
  summaries: "Vendor summary",
  "prefer-raw": "Raw where covered",
}

const GROUP_WORDS: Record<AggregateGroupKey, string> = {
  source: "Source",
  about: "About",
  code: "Category",
  metric: "Metric",
  kind: "Collection",
}

/** A reducer's column head: its words, and the unit its values carry. */
function headOf(
  reducer: AggregateReducer,
  rows: readonly AggregateRow[]
): string {
  const key = reducerKey(reducer)
  const unit = rows
    .map((row) => rowValue(row, key)?.unit)
    .find((u): u is string => Boolean(u))
  const symbol = unit ? unitSymbol(unit) : ""
  return symbol ? `${reducerLabel(reducer)} (${symbol})` : reducerLabel(reducer)
}

export function RollupTable({
  rows,
  reducers,
  groupKey,
  groupCell,
  sourceText,
  label,
}: {
  rows: readonly AggregateRow[]
  reducers: readonly AggregateReducer[]
  groupKey?: AggregateGroupKey
  /** How a group value reads in its cell. */
  groupCell: (value: string | undefined) => ReactNode
  /** A source's name, for the sources column. */
  sourceText: (id: string) => string
  /** The table's accessible name. */
  label: string
}) {
  const flagged = rows.some((r) => r.synthetic || r.unavailable)
  return (
    <div className="mt-3">
      <div className="mb-1.5 flex items-center gap-2 text-[12.5px] text-faint">
        <span>
          {rows.length === 1
            ? "1 row"
            : `${rows.length.toLocaleString("en")} rows`}
        </span>
        <CopyButton
          value={rollupText(rows, reducers, groupKey)}
          label="Copy the table"
        />
      </div>
      <div className="max-h-80 overflow-auto rounded-md border border-border">
        <table aria-label={label} className="w-full text-[13px]">
          <thead className="sticky top-0 bg-background">
            <tr className="[&>th]:h-[34px] [&>th]:border-b [&>th]:border-border [&>th]:px-2.5 [&>th]:text-left [&>th]:text-[12.5px] [&>th]:font-medium [&>th]:whitespace-nowrap [&>th]:text-faint">
              <th scope="col">When</th>
              {groupKey && <th scope="col">{GROUP_WORDS[groupKey]}</th>}
              {reducers.map((r) => (
                <th key={reducerKey(r)} scope="col" className="text-right!">
                  {headOf(r, rows)}
                </th>
              ))}
              <th scope="col" className="text-right!">
                Count
              </th>
              <th scope="col">Covered</th>
              <th scope="col">Sources</th>
              <th scope="col">From</th>
              {flagged && <th scope="col">Note</th>}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => (
              <tr
                key={`${row.start}-${i}`}
                className="[&:last-child>td]:border-b-0 [&>td]:h-[30px] [&>td]:border-b [&>td]:border-border [&>td]:px-2.5 [&>td]:whitespace-nowrap"
              >
                <td title={`${row.start} to ${row.end}`}>
                  {windowWords(Date.parse(row.start), Date.parse(row.end))}
                </td>
                {groupKey && <td>{groupCell(row.group?.[groupKey])}</td>}
                {reducers.map((r) => {
                  const v = rowValue(row, reducerKey(r))
                  return (
                    <td key={reducerKey(r)} className="text-right">
                      <Reading value={v?.value} unit={v?.unit} />
                    </td>
                  )
                })}
                <td className="text-right tabular-nums">
                  {row.count === undefined ? (
                    <EmptyValue />
                  ) : (
                    row.count.toLocaleString("en")
                  )}
                </td>
                <td title={row.coverage}>
                  {row.coverage ? coverageWords(row.coverage) : <EmptyValue />}
                </td>
                <td>
                  {row.sources?.length ? (
                    row.sources.map(sourceText).join(", ")
                  ) : (
                    <EmptyValue />
                  )}
                </td>
                <td>
                  {row.from ? (
                    (FROM_WORDS[row.from] ?? row.from)
                  ) : (
                    <EmptyValue />
                  )}
                </td>
                {flagged && (
                  <td>
                    {row.unavailable ? (
                      <Pill tone="warn">Not available</Pill>
                    ) : row.synthetic ? (
                      <Pill tone="neutral" dot={false}>
                        Filled in
                      </Pill>
                    ) : null}
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
