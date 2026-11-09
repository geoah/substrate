import { useMemo, useState } from "react"
import { directionPhrase, entryByCode } from "../catalogue"
import { useBackend } from "../data"
import { useMode } from "../data/mode"
import { labsKeyItems } from "../labs/keys"
import { resultParts, resultText } from "../labs/result"
import {
  chartBands,
  chartPoints,
  formResults,
  labMarkers,
  selectedContext,
  seriesForms,
  specimenOf,
  trendText,
  twinText,
  type ChartPoint,
} from "../labs/view"
import { CHART_GUTTER, clipBands, tickText } from "../lib/chart"
import { useDisplayControl } from "../lib/display"
import {
  channelLabel,
  formatDate,
  formatMonth,
  formatValue,
  rangeText,
} from "../lib/format"
import { latestLine, resultCount, seriesLegend } from "../lib/sourceMarkers"
import {
  emptySpan,
  inDomain,
  isRangeId,
  pageDomain,
  RANGES,
  type RangeId,
} from "../lib/timeDomain"
import { unitLabel } from "../lib/units"
import { go, routeHash } from "../route"
import { normalBands, resultFlag, trendFit, trendTone } from "../rules/labs"
import { ChartKey } from "../ui/ChartKey"
import { EmptyState } from "../ui/EmptyState"
import { Screen } from "../ui/Screen"
import { useShell } from "../ui/shell"
import { Skeleton, SkeletonCard } from "../ui/Skeleton"
import { SegmentedControl } from "../ui/ToggleGroup"
import { TrendChart, type TrendPoint } from "../ui/TrendChart"
import { FilterSelect } from "../ui/labs/FilterSelect"
import { ReadProblem } from "../ui/labs/ReadProblem"
import { ResultTable } from "../ui/labs/ResultTable"
import { StatTiles, type StatTile } from "../ui/labs/StatTiles"

/** The trend's direction as one glyph, for the tile's value. */
const ARROW = { up: "↗", down: "↘", flat: "→" } as const

/** Back to Labs: the list as it was left where the history came from it. */
function back() {
  if (window.history.length > 1) window.history.back()
  else go({ screen: "labs", query: "" })
}

/** One subject over time: the newest (or the hovered) result, the trend and
 * the count in tiles, the chart on the screen's own range, and every result
 * in a table. `?range=`, `?channel=` and `?unit=` ride in the hash. */
export function LabScreen(props: { entry: string; query: string }) {
  const { today } = useMode()
  const { wide } = useShell()
  const labs = useBackend().labs.useLabs()
  const control = useDisplayControl()
  const display = control.display.labs
  const params = useMemo(() => new URLSearchParams(props.query), [props.query])
  const rangeParam = params.get("range")
  const range: RangeId = isRangeId(rangeParam) ? rangeParam : "All"
  const [active, setActive] = useState<ChartPoint | null>(null)

  const model = labs.data
  const index = model?.index
  const view = model?.views.find((v) => v.code === props.entry)
  const markers = useMemo(() => labMarkers(index), [index])

  const set = (k: string, v: string | undefined) => {
    const next = new URLSearchParams(props.query)
    if (v === undefined) next.delete(k)
    else next.set(k, v)
    go({ screen: "lab", entry: props.entry, query: next.toString() }, true)
  }
  const backTo = { label: "Labs", onClick: back }

  if (!model || !index) {
    return (
      <Screen
        title={entryByCode(props.entry)?.name ?? "Lab result"}
        back={backTo}
      >
        {labs.error ? (
          <ReadProblem error={labs.error} onRetry={labs.refresh} />
        ) : (
          <div role="status" aria-label="Loading the results">
            <Skeleton height={88} className="mb-6" radius={10} />
            <SkeletonCard chartHeight={wide ? 280 : 200} />
          </div>
        )}
      </Screen>
    )
  }
  if (!view) {
    const name =
      entryByCode(props.entry)?.name ??
      index.metrics.get(props.entry)?.name ??
      "this test"
    return (
      <Screen title={name} back={backTo}>
        <EmptyState
          title={`No results for ${name} yet`}
          description="They show here once a lab report holding them is imported or synced."
        />
      </Screen>
    )
  }

  const channel = params.get("channel") ?? undefined
  const series = view.series.find((s) => s.channel === channel) ?? view.lead
  const forms = seriesForms(view.subject, series)
  const unitParam = params.get("unit")
  const form = forms.find((f) => !f.display && f.unit === unitParam) ?? forms[0]
  const own = form.display
  const entry = view.entry
  const results = own ? series.results : formResults(index, series, form)
  const prec = own ? view.precision : (form.metrics[0]?.precision ?? undefined)
  const unit = own ? view.unit : form.unit
  const domain = pageDomain(range, today, results)
  const shown = results.filter((r) => inDomain(r.at, domain))
  const allPoints = chartPoints(results, markers, entry, own)
  const points = display.sourceShapes
    ? allPoints
    : allPoints.map((p) => ({ ...p, marker: "circle" as const }))
  // Fit only what the chart draws: the axis ends at noon today.
  const inside = points.filter((p) => inDomain(p.at, domain))
  const fit = trendFit(inside.map((p) => ({ at: p.at, value: p.value })))
  const tone = trendTone(fit, entry)
  const text = trendText(fit, entry)
  const newest = results[results.length - 1]
  const newestShown = newest ? resultParts(newest, prec, unit) : undefined
  const newestFlag = newest ? resultFlag(newest, entry, own) : undefined
  const latest = latestLine(index.sources, results)
  const phrase = directionPhrase(entry)
  const sel = active?.result
  const selShown = sel ? resultParts(sel, prec, unit) : undefined
  const selFlag = sel ? resultFlag(sel, entry, own) : undefined
  const bands = clipBands(
    chartBands(index, entry, newest, {
      withTarget: own,
      targetZones: display.targetZones,
      labRange: display.labRange,
    }),
    [
      ...inside.map((p) => p.value),
      ...(fit && display.trendline ? [fit.from.value, fit.to.value] : []),
    ]
  )
  const gap = points.length ? emptySpan(domain.start, points[0].at) : undefined
  // The screen's key lists the layers; the chart's own legend names the
  // sources with their counts.
  const keyItems = labsKeyItems(display, markers, {
    sources: new Set(),
    zones: new Set(
      bands
        .map((b) => b.tone)
        .filter((t): t is "success" | "warning" | "danger" => t !== "neutral")
    ),
    labRange: bands.some((b) => b.tone === "neutral"),
    trend: !!fit,
    hatch: !!gap,
    outOfRange: inside.some((p) => p.tone === "danger"),
    offTarget: inside.some((p) => p.tone === "warning"),
  })
  const selJudged =
    !!sel && (normalBands(sel).length > 0 || (own && !!entry?.target?.length))
  const selWord = selFlag?.word ?? (selJudged ? "In range" : undefined)
  const word =
    fit?.direction === "up"
      ? "Rising"
      : fit?.direction === "down"
        ? "Falling"
        : "Flat"
  const flagTone = (t: "danger" | "warning" | "neutral" | undefined) =>
    t === "neutral" ? undefined : t

  const tiles: StatTile[] = [
    sel && selShown
      ? {
          label: "Selected",
          value: selShown.value,
          unit: selShown.unit,
          // The flag's word leads, so status is never colour alone.
          sub: `${selWord ? `${selWord}, ` : ""}${selectedContext(index, { unit, precision: prec }, sel).replace(/^Selected: /, "")}`,
          tone: flagTone(selFlag?.tone) ?? (selJudged ? "success" : undefined),
          isSelected: true,
        }
      : {
          label: "Latest",
          value: newestShown?.value ?? "None",
          unit: newestShown?.unit,
          sub: latest
            ? `${newestFlag ? `${newestFlag.word}, ` : ""}${latest.date}${latest.source ? `, ${latest.source}` : ""}`
            : undefined,
          tone: flagTone(newestFlag?.tone),
          isSelected: false,
        },
    {
      label: "Trend",
      // A no-break space, so the arrow never stands on a line of its own.
      value: fit ? `${ARROW[fit.direction]}\u00a0${text.label}` : "None",
      tone: tone === "neutral" ? undefined : tone,
      sub: fit
        ? `${word} over ${resultCount(fit.n)}.${phrase ? ` ${phrase}.` : ""}`
        : "Needs three results in the range",
    },
    {
      label: "Results",
      value: String(shown.length),
      sub: `of ${results.length} in all`,
    },
  ]

  const title = series.channel
    ? `${view.name}, ${channelLabel(series.channel).toLowerCase()}`
    : view.name
  const reportHref = (id: string | undefined) =>
    id ? routeHash({ screen: "report", id }) : undefined

  return (
    <Screen
      title={title}
      subtitle={view.subject.description}
      back={backTo}
      width={control.display.wide ? "full" : "cards"}
      toolbar={
        <>
          <SegmentedControl
            label="Range"
            options={RANGES.map((r) => ({ value: r.id, label: r.id }))}
            value={range}
            onChange={(v) => set("range", v === "All" ? undefined : v)}
          />
          {view.series.length > 1 && (
            <FilterSelect
              label="Region"
              value={series.channel ?? ""}
              onChange={(v) => set("channel", v || undefined)}
              options={view.series.map((s) => ({
                value: s.channel ?? "",
                label: s.channel ? channelLabel(s.channel) : "Whole body",
              }))}
            />
          )}
          {forms.length > 1 && (
            <FilterSelect
              label="Unit"
              value={own ? "" : (form.unit ?? "")}
              onChange={(v) => set("unit", v || undefined)}
              options={forms.map((f) => ({
                value: f.display ? "" : (f.unit ?? ""),
                label: unitLabel(f.unit) || "No unit",
              }))}
            />
          )}
        </>
      }
    >
      <StatTiles tiles={tiles} />
      {display.key && points.length > 0 && (
        <ChartKey className="mb-4" label="Chart key" items={keyItems} />
      )}
      {points.length > 0 && (
        <section className="mb-7" aria-label="Over time">
          <h2 className="mb-1.5 px-4 text-[13px] tracking-wide text-secondary uppercase">
            Over time
          </h2>
          <div className="rounded-[10px] bg-card p-4">
            <TrendChart
              label={`${title} over time`}
              data={points}
              bands={bands}
              unit={unitLabel(unit)}
              tone="neutral"
              formatValue={(v) => formatValue(v, prec)}
              formatTick={tickText}
              formatTime={formatMonth}
              gutter={CHART_GUTTER}
              empty={`No results in this range; latest ${formatMonth(points[points.length - 1].at)}`}
              timeRange={domain}
              height={wide ? 280 : 200}
              emptyBefore={
                gap
                  ? {
                      label: (first) =>
                        `No results before ${formatMonth(first)}`,
                    }
                  : undefined
              }
              trendline={
                fit && display.trendline
                  ? {
                      from: { at: fit.from.at, value: fit.from.value },
                      to: { at: fit.to.at, value: fit.to.value },
                      label: text.tooltip,
                    }
                  : undefined
              }
              legend={seriesLegend(results, markers, (r) =>
                inDomain(r.at, domain)
              ).map((l) => ({
                key: l.key,
                label: l.label,
                glyph: {
                  kind: "marker" as const,
                  shape: display.sourceShapes ? l.marker : ("circle" as const),
                },
              }))}
              readout="none"
              onActiveChange={(p: TrendPoint | null) =>
                setActive((p as unknown as ChartPoint | null) ?? null)
              }
              syncKey={`lab-${props.entry}`}
            />
            {phrase && (
              <p className="mt-2 text-[13px] text-secondary">{phrase}.</p>
            )}
          </div>
        </section>
      )}
      <section className="mb-7" aria-label="Results">
        <h2 className="mb-1.5 px-4 text-[13px] tracking-wide text-secondary uppercase">
          Results
        </h2>
        <ResultTable
          label={`${title} results`}
          leadHeader="Date"
          withSource
          rows={[...shown].reverse().map((r) => {
            const f = resultFlag(r, entry, own)
            const nb = normalBands(r)[0]
            const source = r.source
              ? (index.sources.get(r.source)?.name ?? r.source)
              : undefined
            return {
              key: r.id,
              lead: formatDate(r.at),
              leadNote: source,
              value: resultText(r, prec, unit),
              twin: own ? twinText(index, specimenOf(series, r)) : undefined,
              range: nb ? rangeText(nb.low, nb.high, prec) : undefined,
              flag: f,
              source,
              fasting: r.fasting,
              report: reportHref(r.report),
            }
          })}
          empty={
            <EmptyState title="No results in this range" variant="compact" />
          }
        />
      </section>
      {own && series.apart.length > 0 && (
        <section className="mb-7" aria-label="In other units">
          <h2 className="mb-1.5 px-4 text-[13px] tracking-wide text-secondary uppercase">
            In other units
          </h2>
          <p className="mb-2 px-4 text-[13px] text-secondary">
            These convert to {unitLabel(unit) || "this unit"} through no unit or
            analyte record this repository holds, so they are shown as they
            came.
          </p>
          <ResultTable
            label={`${title} results in other units`}
            leadHeader="Date"
            withSource
            rows={[...series.apart].reverse().map(({ shown: r }) => {
              const nb = normalBands(r)[0]
              const m = index.metrics.get(r.metric)
              const source = r.source
                ? (index.sources.get(r.source)?.name ?? r.source)
                : undefined
              return {
                key: r.id,
                lead: formatDate(r.at),
                leadNote: source,
                value: resultText(r, m?.precision, m?.unit),
                range: nb
                  ? rangeText(nb.low, nb.high, m?.precision)
                  : undefined,
                flag: resultFlag(r, entry, false),
                source,
                fasting: r.fasting,
                report: reportHref(r.report),
              }
            })}
            empty={null}
          />
        </section>
      )}
    </Screen>
  )
}
