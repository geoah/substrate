import { History } from "lucide-react"
import { memo, useCallback, useMemo, useState } from "react"
import { unitLabel } from "../../lib/units"
import { resultParts } from "../../labs/result"
import type { LabIndex } from "../../labs/types"
import {
  cardBands,
  metricInfo,
  rangesLine,
  resultWhere,
  specimenOf,
  twinText,
  type ChartPoint,
  type MetricCardData,
  type ResultWhere,
} from "../../labs/view"
import { CHART_GUTTER, tickText } from "../../lib/chart"
import type { LabsDisplay } from "../../lib/display"
import { formatMonth, formatValue } from "../../lib/format"
import type { TimeDomain } from "../../lib/timeDomain"
import { routeHash } from "../../route"
import { resultFlag } from "../../rules/labs"
import { Badge } from "../Badge"
import { IconButton } from "../IconButton"
import {
  Keeps,
  MetricCard,
  Truncates,
  type MetricCardHeadline,
} from "../MetricCard"
import { useShell } from "../shell"
import { TrendChart, type TrendPoint } from "../TrendChart"
import { TrendChip } from "../TrendChip"
import { InfoPopover } from "./InfoPopover"
import { MetricInfo } from "./MetricInfo"

/** The headline's first line: "Selected: 27 Sept 2025, Lab A, fasting".
 * Only the source name gives way; the date and "fasting" always show. */
export function Where({
  prefix,
  where,
}: {
  prefix: string
  where: ResultWhere
}) {
  return (
    <>
      <Keeps>{`${prefix}${where.date}${where.source ? ", " : ""}`}</Keeps>
      {where.source && <Truncates>{where.source}</Truncates>}
      {where.fasting && <Keeps>, fasting</Keeps>}
    </>
  )
}

/** A subject's history, carrying the list's range and the card's channel. */
export function historyHref(card: MetricCardData, range: string): string {
  const q = new URLSearchParams()
  if (card.series.channel) q.set("channel", card.series.channel)
  if (range !== "All") q.set("range", range)
  return routeHash({
    screen: "lab",
    entry: card.view.code,
    query: q.toString(),
  })
}

export interface LabCardProps {
  index: LabIndex
  card: MetricCardData
  domain: TimeDomain
  /** The list's range, carried to the history screen. */
  range: string
  display: LabsDisplay
  /** Show the Important badge (scope all). */
  showImportant: boolean
  /** Show the panel's name (Group by panel off). */
  showPanel: boolean
  /** The screen's Details switch: the Details block shows on every card. */
  isOpen: boolean
}

/** One subject on Labs: the status in the header, the newest result as the
 * headline, the chart filling the card, and the sources, direction and
 * cadence in the Details block. The headline is the chart's readout: a
 * hovered, stepped or pinned result replaces it, and nothing floats over
 * the plot. */
export const LabCard = memo(function LabCard({
  index,
  card,
  domain,
  range,
  display,
  showImportant,
  showPanel,
  isOpen,
}: LabCardProps) {
  const { wide } = useShell()
  const { view, status, latest } = card
  const [active, setActive] = useState<ChartPoint | null>(null)
  const onActive = useCallback(
    (p: TrendPoint | null) =>
      setActive((p as unknown as ChartPoint | null) ?? null),
    []
  )
  const precision = view.precision

  const points = useMemo(
    () =>
      display.sourceShapes
        ? card.points
        : card.points.map((p) => ({ ...p, marker: "circle" as const })),
    [card.points, display.sourceShapes]
  )
  const bands = useMemo(
    () => cardBands(index, card, domain, display),
    [index, card, domain, display]
  )

  const r = active?.result
  const withTwin = (u: string, twin?: string) => (twin ? `${u} ${twin}` : u)
  let headline: MetricCardHeadline
  if (r) {
    const shown = resultParts(r, precision, view.unit)
    const flag = resultFlag(r, view.entry)
    const judged = r.ranges.length > 0 || !!view.entry?.target?.length
    headline = {
      value: shown.value,
      unit: withTwin(shown.unit, twinText(index, specimenOf(card.series, r))),
      context: <Where prefix="Selected: " where={resultWhere(index, r)} />,
      detail: rangesLine(view, r),
      end: flag ? (
        <Badge label={flag.word} tone={flag.tone} />
      ) : judged ? (
        <Badge label="In range" tone="success" />
      ) : undefined,
      isSelected: true,
    }
  } else {
    headline = {
      value: card.value,
      unit: withTwin(card.unit, card.twin),
      context: <Where prefix="" where={resultWhere(index, latest)} />,
      detail: rangesLine(view, latest),
      end: (
        <TrendChip
          fit={card.trend}
          tone={card.trendTone}
          text={card.trendText}
        />
      ),
    }
  }

  const info = metricInfo(index, view, latest, status.badge?.word)

  // Built once per data and settings, so a hover step (which draws this
  // card's headline again) does not draw the chart again.
  const chart = useMemo(() => {
    if (!card.points.length) return undefined
    const trend = display.trendline ? card.trend : undefined
    return (
      <TrendChart
        label={`${view.name} over time`}
        data={points}
        bands={bands}
        unit={unitLabel(view.unit)}
        tone="neutral"
        formatValue={(v) => formatValue(v, precision)}
        formatTick={tickText}
        formatTime={formatMonth}
        gutter={CHART_GUTTER}
        empty={card.emptyText}
        timeRange={domain}
        height={wide ? 160 : 128}
        emptyBefore={
          card.emptySpan
            ? { label: (first) => `No results before ${formatMonth(first)}` }
            : undefined
        }
        trendline={
          trend
            ? {
                from: { at: trend.from.at, value: trend.from.value },
                to: { at: trend.to.at, value: trend.to.value },
                // The chart ends the label with its own full stop.
                label: card.trendText.tooltip.replace(/\.$/, ""),
              }
            : undefined
        }
        readout="none"
        onActiveChange={onActive}
        syncKey="labs"
      />
    )
  }, [card, view, points, bands, precision, domain, wide, display, onActive])

  return (
    <MetricCard
      id={view.code}
      title={view.name}
      status={status.key}
      edge={status.edge}
      badges={
        <>
          {card.regionNote && <Badge label={card.regionNote} isMono />}
          {status.badge && (
            <Badge label={status.badge.word} tone={status.badge.tone} />
          )}
          {status.overdueBadge && <Badge label="Overdue" tone="info" />}
          {showImportant && view.important && (
            <Badge label="Important" isMono />
          )}
          {showPanel && <Badge label={view.panel} isMono />}
        </>
      }
      actions={
        <>
          <InfoPopover label={`About ${view.name}`}>
            <MetricInfo info={info} />
          </InfoPopover>
          <IconButton
            label={`${view.name} history`}
            href={historyHref(card, range)}
          >
            <History className="size-[18px]" aria-hidden />
          </IconButton>
        </>
      }
      headline={headline}
      visual={
        <>
          {chart ?? (
            <p className="text-[13px] leading-[19px] text-secondary">
              {card.series.results.length
                ? `These results are words, not numbers. ${card.sourcesText}.`
                : "No result converts to the unit this card shows."}
            </p>
          )}
          {card.apart.length > 0 && (
            <p className="mt-1 text-[12px] leading-4 text-secondary">
              {card.apart.length === 1
                ? `In another unit: ${card.apart[0]}`
                : `In other units: ${card.apart[0]}, and ${card.apart.length - 1} more on its history`}
            </p>
          )}
        </>
      }
      details={{
        isOpen,
        children: (
          // Three fixed lines, so every card's block has one height.
          <div className="flex flex-col text-[13px] leading-[19px] text-secondary">
            <p className="min-h-[38px]">{card.detailsLines[0] || " "}</p>
            <p>{card.detailsLines[1] || " "}</p>
            <p>{card.detailsLines[2] || " "}</p>
          </div>
        ),
      }}
    />
  )
})
