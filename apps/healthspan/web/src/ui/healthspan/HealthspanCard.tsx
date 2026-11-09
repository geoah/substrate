import { memo, useCallback, useState } from "react"
import type {
  CardChip,
  HealthspanCardData,
} from "../../healthspan/healthspanCards"
import { selectedHeadline } from "../../healthspan/healthspanCards"
import { CHART_GUTTER, tickText } from "../../lib/chart"
import { formatMonth } from "../../lib/format"
import { Badge } from "../Badge"
import { InfoBlock, InfoSources, InfoTitle } from "../Info"
import {
  HeadlineStack,
  MetricCard,
  type MetricCardHeadline,
} from "../MetricCard"
import { TEXT } from "../tone"
import { TrendChart, type TrendPoint } from "../TrendChart"
import { TrendChip } from "../TrendChip"
import { ZoneBar } from "../ZoneBar"

/** A Healthspan direction as the shared trend chip. */
export function HealthspanChip({ chip }: { chip: CardChip }) {
  return (
    <TrendChip
      fit={{ direction: chip.direction }}
      tone={chip.tone}
      text={{
        label: chip.label,
        tooltip: chip.tooltip,
        aria: `${chip.label}: ${chip.tooltip}`,
      }}
    />
  )
}

export interface HealthspanCardProps {
  card: HealthspanCardData
  axis: { start: string; end: string }
  /** The screen's Details switch. */
  isOpen: boolean
  /** Set for three seconds after a focus row or a link points at it. */
  isHighlighted?: boolean
}

/** One Healthspan card on the shared MetricCard: the label with its status
 * and group badges, the newest value as the headline (the age score
 * effect at its end for a contributor, the trend chip for a signal), a
 * zone bar with labelled segments or one muted line saying what it needs,
 * the suggestion, and under Details the 182-day chart on the screen's
 * axis with the bar's zones as bands. A point hovered, stepped or pinned
 * on that chart swaps the headline; nothing floats over the plot. */
export const HealthspanCard = memo(function HealthspanCard({
  card,
  axis,
  isOpen,
  isHighlighted,
}: HealthspanCardProps) {
  const [active, setActive] = useState<TrendPoint | null>(null)
  const onActive = useCallback((p: TrendPoint | null) => setActive(p), [])
  const h = card.headline
  const chip = card.headlineChip ?? card.facts?.chip
  const end =
    h.impact === undefined ? (
      chip ? (
        <HealthspanChip chip={chip} />
      ) : undefined
    ) : (
      <HeadlineStack
        label="Age score effect"
        value={
          <span className={TEXT[h.impact?.tone ?? "neutral"]}>
            {h.impact ? h.impact.text : "No data"}
          </span>
        }
      />
    )
  const headline: MetricCardHeadline =
    active && isOpen
      ? { ...selectedHeadline(card.chart, active), end, isSelected: true }
      : { value: h.value, unit: h.unit, context: h.context, end }

  const visual = card.bar ? (
    <ZoneBar
      label={card.bar.label}
      min={card.bar.min}
      max={card.bar.max}
      segments={card.bar.segments}
      markers={card.bar.markers}
      ticks={card.bar.ticks}
      minLabel={card.bar.minLabel}
      maxLabel={card.bar.maxLabel}
    />
  ) : card.facts ? (
    <p className="flex flex-wrap gap-x-4 gap-y-1 text-[13px]">
      {card.facts.items.map((i) => (
        <span key={i.label}>
          <span className="text-secondary">{i.label}</span>{" "}
          <span className="tabular-nums">{i.value}</span>
        </span>
      ))}
    </p>
  ) : (
    <p className="text-[13px] text-secondary">{card.line ?? ""}</p>
  )

  const c = card.chart
  return (
    <MetricCard
      id={card.id}
      title={card.label}
      status={[...card.statuses].join(" ")}
      edge={card.edge}
      badges={card.badges.map((b) => (
        <Badge key={b.label} label={b.label} tone={b.tone} isMono={b.isMono} />
      ))}
      info={{
        label: `About ${card.label}`,
        content: (
          <>
            <InfoTitle>{card.label}</InfoTitle>
            {card.info.map((i) => (
              <InfoBlock key={i.label} label={i.label}>
                {i.value}
              </InfoBlock>
            ))}
            <InfoSources
              sources={card.sources.map((s) => ({ name: s.name, url: s.url }))}
            />
          </>
        ),
      }}
      history={{ label: `${card.label} history`, href: card.historyHref }}
      headline={headline}
      note={card.note.text}
      isHighlighted={isHighlighted}
      visual={visual}
      details={{
        isOpen,
        // Drawn only while open: a closed fold holds no chart.
        children: isOpen ? (
          <div className="flex flex-col gap-1">
            <TrendChart
              label={c.label}
              data={c.data}
              baseline={c.baseline}
              bands={c.bands}
              unit={c.unit}
              formatValue={c.formatValue}
              formatTick={c.formatTick ?? tickText}
              valueRange={c.valueRange}
              formatTime={formatMonth}
              timeRange={axis}
              gutter={CHART_GUTTER}
              height={128}
              readout="none"
              onActiveChange={onActive}
              syncKey="healthspan"
              empty="No values in the last 182 days"
            />
            {/* One line under every chart, so a row's cards keep their
                bars, notes and charts level: the facts or the caption,
                then the chip. */}
            <div className="flex min-h-6 flex-wrap items-center justify-between gap-x-3 gap-y-1 text-[13px]">
              {c.facts ? (
                <span className="flex flex-wrap gap-x-4">
                  {c.facts.map((f) => (
                    <span key={f.label}>
                      <span className="text-secondary">{f.label}</span>{" "}
                      <span className="tabular-nums">{f.value}</span>
                    </span>
                  ))}
                </span>
              ) : (
                <span className="text-secondary">{c.caption ?? ""}</span>
              )}
              {c.chip && <HealthspanChip chip={c.chip} />}
            </div>
          </div>
        ) : null,
      }}
    />
  )
})
