/** The numbers under a chart, one click away: one line per row of the
 * answer (a bucket, or one group of a bucket) with its window, one column per
 * reducer with its unit, the points, the covered time, the sources that fed
 * it and whether raw points or a vendor summary served it. A row the server
 * filled in or could not answer says so. Grouped by source, a source's
 * channel has its own column. The whole table copies as
 * tab-separated text, the exact digits and the wire's instants, folded or
 * not. */

import { useState, type ReactNode } from "react"
import { ChevronRightIcon } from "lucide-react"

import { CopyButton } from "@/components/identity/copy-button"
import { EmptyValue } from "@/components/identity/empty-value"
import { Pill } from "@/components/identity/pill"
import { CodeMark, Reading } from "@/components/observations/marks"
import { Button } from "@/components/ui/button"
import type {
  AggregateGroupKey,
  AggregateReducer,
  AggregateRow,
} from "@/lib/api/types"
import {
  coverageWords,
  namedSources,
  reducerKey,
  reducerLabel,
  reducerName,
  rollupText,
  rowValue,
  windowWords,
} from "@/lib/observations"
import { unitSymbol } from "@/lib/units"
import { cn } from "@/lib/utils"

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
  channel: "Channel",
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
  caption,
  precision,
}: {
  rows: readonly AggregateRow[]
  reducers: readonly AggregateReducer[]
  groupKey?: AggregateGroupKey
  /** How a group value reads in its cell. */
  groupCell: (value: string | undefined) => ReactNode
  /** Which metric the numbers are, where a chart draws several. */
  caption?: ReactNode
  /** A source's name, for the sources column. */
  sourceText: (id: string) => string
  /** The table's accessible name. */
  label: string
  /** The decimal places a value is shown to; the copy keeps every digit. */
  precision?: number
}) {
  // Folded by default, so a timeline of several metrics reads chart after
  // chart; one click shows the numbers.
  const [open, setOpen] = useState(false)
  const flagged = rows.some((r) => r.synthetic || r.unavailable)
  const channels = rows.some((r) => r.group?.channel)
  const count =
    rows.length === 1 ? "1 row" : `${rows.length.toLocaleString("en")} rows`
  return (
    <div className="mt-2">
      <div className="flex items-center gap-1 text-[12.5px] text-faint">
        {caption && (
          <span className="mr-1 font-medium text-muted-foreground">
            {caption}
          </span>
        )}
        <Button
          variant="ghost"
          size="sm"
          aria-expanded={open}
          className="h-7 gap-1 px-1.5 text-[12.5px] font-normal text-muted-foreground"
          onClick={() => setOpen((o) => !o)}
        >
          <ChevronRightIcon
            className={cn("size-3.5 transition-transform", open && "rotate-90")}
          />
          {open ? "Hide the numbers" : "Show the numbers"}
        </Button>
        <span>{count}</span>
        <CopyButton
          value={rollupText(rows, reducers, groupKey)}
          label="Copy the table"
        />
      </div>
      {open && (
        <div className="mt-1 max-h-80 overflow-auto rounded-md border border-border">
          <table aria-label={label} className="w-full text-[13px]">
            <thead className="sticky top-0 bg-background">
              <tr className="[&>th]:h-[34px] [&>th]:border-b [&>th]:border-border [&>th]:px-2.5 [&>th]:text-left [&>th]:text-[12.5px] [&>th]:font-medium [&>th]:whitespace-nowrap [&>th]:text-faint">
                <th scope="col">When</th>
                {groupKey && <th scope="col">{GROUP_WORDS[groupKey]}</th>}
                {channels && <th scope="col">Channel</th>}
                {reducers.map((r) => (
                  <th key={reducerKey(r)} scope="col" className="text-right!">
                    {headOf(r, rows)}
                  </th>
                ))}
                <th scope="col" className="text-right!">
                  Points
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
                  {channels && <td>{row.group?.channel ?? <EmptyValue />}</td>}
                  {reducers.map((r) => {
                    const v = rowValue(row, reducerKey(r))
                    return (
                      <td key={reducerKey(r)} className="text-right">
                        {v?.value == null && v?.code ? (
                          <CodeMark code={v.code} />
                        ) : (
                          <Reading
                            value={v?.value}
                            unit={v?.unit}
                            precision={
                              reducerName(r) === "count" ? undefined : precision
                            }
                          />
                        )}
                      </td>
                    )
                  })}
                  <td className="text-right tabular-nums">
                    {row.count.toLocaleString("en")}
                  </td>
                  <td title={row.coverage}>
                    {row.coverage ? (
                      coverageWords(row.coverage)
                    ) : (
                      <EmptyValue />
                    )}
                  </td>
                  <td>
                    {namedSources(row).length ? (
                      namedSources(row).map(sourceText).join(", ")
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
      )}
    </div>
  )
}
