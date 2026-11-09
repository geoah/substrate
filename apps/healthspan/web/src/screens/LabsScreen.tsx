import { BarButton } from "@substrate/react"
import { useMemo, useState } from "react"
import { useBackend } from "../data"
import { useMode } from "../data/mode"
import { labsKeyItems } from "../labs/keys"
import {
  applyFilters,
  DEFAULT_FILTERS,
  filterCounts,
  filtersToParams,
  parseFilters,
  type LabsFilters,
} from "../labs/labsFilters"
import {
  byPanel,
  cardBands,
  flatOrder,
  labMarkers,
  labsSummary,
  metricCard,
  panelOrder,
  type LabView,
  type MetricCardData,
} from "../labs/view"
import { useDisplayControl } from "../lib/display"
import { sourceOptions } from "../lib/sourceMarkers"
import { inDomain, pageDomain } from "../lib/timeDomain"
import { go, useRoute } from "../route"
import { ChartKey } from "../ui/ChartKey"
import { Disclosure } from "../ui/Disclosure"
import { EmptyState } from "../ui/EmptyState"
import { CardGrid } from "../ui/MetricCard"
import { Screen } from "../ui/Screen"
import { useShell } from "../ui/shell"
import { SkeletonCard } from "../ui/Skeleton"
import { LabCard } from "../ui/labs/LabCard"
import { LabsToolbar } from "../ui/labs/LabsToolbar"
import { PanelSection } from "../ui/labs/PanelSection"
import { ReadProblem } from "../ui/labs/ReadProblem"

/** The card statuses that put a panel first. */
const ATTENTION = new Set(["range", "target", "overdue"])

/** The panels folded, kept in the history entry, so Back from a history
 * screen finds them folded still. */
function savedCollapsed(): Set<string> {
  const saved = (window.history.state as Record<string, unknown> | null)
    ?.labsCollapsed
  return new Set(
    Array.isArray(saved) ? saved.filter((x) => typeof x === "string") : []
  )
}

function saveCollapsed(set: Set<string>): void {
  window.history.replaceState(
    {
      ...((window.history.state as Record<string, unknown> | null) ?? {}),
      labsCollapsed: [...set],
    },
    ""
  )
}

/** Labs: one card per subject in panels, every chart on one time axis, the
 * filters in the hash, one chart key under them. */
export function LabsScreen() {
  const route = useRoute()
  const { today } = useMode()
  const { wide } = useShell()
  const labs = useBackend().labs.useLabs()
  const control = useDisplayControl()
  const display = control.display.labs
  const query = route.screen === "labs" ? route.query : ""
  const filters = useMemo(
    () => parseFilters(new URLSearchParams(query)),
    [query]
  )
  const set = (next: Partial<LabsFilters>) =>
    go(
      {
        screen: "labs",
        query: filtersToParams({ ...filters, ...next }).toString(),
      },
      true
    )

  const model = labs.data
  const index = model?.index
  const views = useMemo(() => model?.views ?? [], [model])
  const markers = useMemo(() => labMarkers(index), [index])
  const domain = useMemo(
    () => pageDomain(filters.range, today, index?.results ?? []),
    [filters.range, today, index]
  )
  // Every card's data, built once per range: a keystroke or a toggle only
  // picks which of them show.
  const cards = useMemo(
    () =>
      index
        ? new Map(
            views.map((v) => [v.code, metricCard(index, v, domain, markers)])
          )
        : new Map<string, MetricCardData>(),
    [index, views, domain, markers]
  )
  const visible = applyFilters(views, filters, domain)
  const counts = filterCounts(views, filters, domain)
  const panels = panelOrder(views.map((v) => v.panel))

  const [collapsed, setCollapsed] = useState(savedCollapsed)
  const collapse = (panel: string, isCollapsed: boolean) => {
    const next = new Set(collapsed)
    if (isCollapsed) next.add(panel)
    else next.delete(panel)
    setCollapsed(next)
    saveCollapsed(next)
  }

  // The key lists only what the visible charts draw.
  const keyItems = useMemo(() => {
    if (!index) return []
    const charted = visible
      .map((v) => cards.get(v.code))
      .filter((c): c is MetricCardData => !!c && c.points.length > 0)
    const inside = charted.flatMap((c) =>
      c.points.filter((p) => inDomain(p.at, domain))
    )
    const bands = charted.flatMap((c) => cardBands(index, c, domain, display))
    const zones = new Set(
      bands
        .map((b) => b.tone)
        .filter((t): t is "success" | "warning" | "danger" => t !== "neutral")
    )
    return labsKeyItems(display, markers, {
      sources: new Set(inside.map((p) => p.series)),
      zones,
      labRange: bands.some((b) => b.tone === "neutral"),
      trend: charted.some((c) => !!c.trend),
      hatch: charted.some((c) => !!c.emptySpan),
      outOfRange: inside.some((p) => p.tone === "danger"),
      offTarget: inside.some((p) => p.tone === "warning"),
    })
  }, [index, visible, cards, domain, display, markers])

  // Panels with something to look at come first, then the rest; both keep
  // the catalogue's order, and the order inside a panel stays the
  // catalogue's.
  const sections = byPanel(visible)
  const ordered = [
    ...sections.filter((p) => p.views.some((v) => ATTENTION.has(v.status.key))),
    ...sections.filter(
      (p) => !p.views.some((v) => ATTENTION.has(v.status.key))
    ),
  ]

  const grid = (items: LabView[]) =>
    index && (
      <CardGrid wide={control.display.wide}>
        {items.map((v) => {
          const card = cards.get(v.code)
          return (
            card && (
              <LabCard
                key={v.code}
                index={index}
                card={card}
                domain={domain}
                range={filters.range}
                display={display}
                showImportant={filters.scope === "all"}
                showPanel={!display.groupByPanel}
                isOpen={display.details}
              />
            )
          )
        })}
      </CardGrid>
    )

  const hasResults = !!index && index.results.length > 0

  return (
    <Screen
      title="Labs"
      subtitle={index ? labsSummary(index, markers) : undefined}
      width={control.display.wide ? "full" : "cards"}
      actions={
        <BarButton onClick={() => go({ screen: "reports" })}>Reports</BarButton>
      }
      toolbar={
        hasResults && (
          <LabsToolbar
            filters={filters}
            set={set}
            counts={counts}
            panels={panels}
            sources={sourceOptions(markers)}
            control={control}
          />
        )
      }
    >
      {!model ? (
        labs.error ? (
          <ReadProblem error={labs.error} onRetry={labs.refresh} />
        ) : (
          <div role="status" aria-label="Loading the labs">
            <CardGrid wide={control.display.wide}>
              {Array.from({ length: 6 }, (_, i) => (
                <SkeletonCard key={i} chartHeight={wide ? 160 : 128} />
              ))}
            </CardGrid>
          </div>
        )
      ) : !hasResults ? (
        <EmptyState
          title="No lab results yet"
          description="Import a lab report or connect a provider that sends them, on Sources."
          action={{
            label: "Open Sources",
            onClick: () => go({ screen: "sources" }),
          }}
        />
      ) : (
        <>
          {display.key &&
            keyItems.length > 0 &&
            (wide ? (
              <ChartKey className="mb-6" label="Chart key" items={keyItems} />
            ) : (
              // Six lines on a phone: folded under its name.
              <Disclosure className="mb-4" title="Chart key">
                <ChartKey label="Chart key" size="sm" items={keyItems} />
              </Disclosure>
            ))}
          {visible.length === 0 && (
            <EmptyState
              title="No metrics match"
              description="Change or clear the filters."
              action={{
                label: "Clear filters",
                onClick: () => set(DEFAULT_FILTERS),
              }}
            />
          )}
          {visible.length > 0 && !display.groupByPanel && (
            <div className="mb-6">{grid(flatOrder(visible))}</div>
          )}
          {display.groupByPanel &&
            ordered.map(({ panel, views: items }) => (
              <PanelSection
                key={panel}
                title={panel}
                count={items.length}
                isCollapsed={collapsed.has(panel)}
                onCollapsedChange={(c) => collapse(panel, c)}
              >
                {grid(items)}
              </PanelSection>
            ))}
        </>
      )}
    </Screen>
  )
}
