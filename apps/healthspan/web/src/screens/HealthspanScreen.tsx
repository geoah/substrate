import { describe } from "@substrate/client"
import { BarButton } from "@substrate/react"
import { Info } from "lucide-react"
import { useEffect, useMemo, useRef, useState } from "react"
import { useBackend } from "../data"
import { countsSleep } from "../engine/engine"
import {
  healthspanAxis,
  healthspanCards,
  healthspanKey,
  type HealthspanCardData,
} from "../healthspan/healthspanCards"
import {
  applyHealthspanFilters,
  DEFAULT_HEALTHSPAN_FILTERS,
  HEALTHSPAN_GROUP_OPTIONS,
  HEALTHSPAN_STATUS_OPTIONS,
  healthspanFilterCounts,
  healthspanFiltersToParams,
  parseHealthspanFilters,
  type HealthspanFilters,
} from "../healthspan/healthspanFilters"
import { useDisplayControl } from "../lib/display"
import { go, useRoute } from "../route"
import { ChartKey } from "../ui/ChartKey"
import { DetailsSwitch, DisplaySettings } from "../ui/DisplaySettings"
import { EmptyState } from "../ui/EmptyState"
import { MobileFilters, MobileGroup } from "../ui/Filters"
import { FocusList } from "../ui/healthspan/FocusList"
import { HealthspanCard } from "../ui/healthspan/HealthspanCard"
import { HealthspanHero } from "../ui/healthspan/HealthspanHero"
import { TrendView } from "../ui/healthspan/TrendView"
import { CardGrid } from "../ui/MetricCard"
import { Screen } from "../ui/Screen"
import { useShell } from "../ui/shell"
import { SkeletonCard } from "../ui/Skeleton"
import { ToggleGroup } from "../ui/ToggleGroup"
import { Toolbar } from "../ui/Toolbar"

// Healthspan: the hero (age score, pace of aging, years at stake), the
// Status and Group filters in the hash's query with the Details switch and
// the display settings, the chart key, what to focus on, the
// contributors, signals and body readings as card grids, and the trend
// view. Every chart shares one 182-day axis and one crosshair. A link
// naming a card (`?focus=contributor-strength`) turns Details on, brings
// the card into view and rings it for three seconds.

const SECTIONS: { title: string; kind: HealthspanCardData["kind"] }[] = [
  { title: "Age score contributors", kind: "contributor" },
  { title: "Signals", kind: "signal" },
  { title: "Body", kind: "body" },
]

const HIGHLIGHT_MS = 3000

export function HealthspanScreen() {
  const route = useRoute()
  const query = route.screen === "healthspan" ? route.query : ""
  const params = useMemo(() => new URLSearchParams(query), [query])
  const filters = parseHealthspanFilters(params)
  const focus = params.get("focus")
  const control = useDisplayControl()
  const display = control.display.healthspan
  const shell = useShell()
  const read = useBackend().healthspan.useHealthspan()
  const view = read.data

  const axis = useMemo(
    () => (view ? healthspanAxis(view.today) : undefined),
    [view]
  )
  // Every card, built once per data and day; the filters only pick.
  const cards = useMemo(
    () =>
      view
        ? healthspanCards({
            data: view.series,
            report: view.report,
            averages: view.averages,
            profile: view.profile,
            findings: view.findings,
            today: view.today,
          })
        : [],
    [view]
  )
  const visible = applyHealthspanFilters(cards, filters)
  const counts = healthspanFilterCounts(cards, filters)

  const set = (next: Partial<HealthspanFilters>) =>
    go(
      {
        screen: "healthspan",
        query: healthspanFiltersToParams({ ...filters, ...next }).toString(),
      },
      true
    )

  // A link to a card: Details on, the card in view, a ring for three
  // seconds. The timers outlive the effect, so the ring always clears.
  const [highlight, setHighlight] = useState<string | null>(null)
  const timers = useRef<ReturnType<typeof setTimeout>[]>([])
  const hasFocus = !!focus && cards.some((c) => c.id === focus)
  const { setDisplay } = control
  useEffect(() => {
    if (!hasFocus || !focus) return
    setDisplay({ healthspan: { details: true } })
    for (const t of timers.current) clearTimeout(t)
    timers.current = [
      setTimeout(() => {
        document
          .getElementById(focus)
          ?.scrollIntoView({ block: "center", behavior: "smooth" })
        setHighlight(focus)
      }, 60),
      setTimeout(() => setHighlight(null), 60 + HIGHLIGHT_MS),
    ]
  }, [hasFocus, focus, setDisplay])
  useEffect(() => {
    const own = timers
    return () => {
      for (const t of own.current) clearTimeout(t)
    }
  }, [])

  const statusToggle = (
    <ToggleGroup
      label="Status"
      options={HEALTHSPAN_STATUS_OPTIONS.map((o) => ({
        ...o,
        count: counts.status[o.value],
      }))}
      value={filters.status}
      onChange={(status) => set({ status })}
    />
  )
  const groupToggle = (
    <ToggleGroup
      label="Group"
      options={HEALTHSPAN_GROUP_OPTIONS.map((o) => ({
        ...o,
        count: counts.group[o.value],
      }))}
      value={filters.group}
      onChange={(group) => set({ group })}
    />
  )
  const toolbar = view && (
    <Toolbar
      end={
        <>
          <DetailsSwitch page="healthspan" control={control} />
          {shell.wide && (
            <DisplaySettings page="healthspan" control={control} />
          )}
        </>
      }
    >
      {shell.wide ? (
        <>
          {statusToggle}
          {groupToggle}
        </>
      ) : (
        <MobileFilters
          page="healthspan"
          control={control}
          activeCount={filters.status.length + filters.group.length}
        >
          <MobileGroup label="Status">{statusToggle}</MobileGroup>
          <MobileGroup label="Group">{groupToggle}</MobileGroup>
        </MobileFilters>
      )}
    </Toolbar>
  )

  const howLink = (
    <BarButton
      aria-label="How the age score works"
      title="How the age score works"
      onClick={() => go({ screen: "how" })}
    >
      <Info className="size-5" aria-hidden />
    </BarButton>
  )

  if (!view || !axis)
    return (
      <Screen title="Healthspan" width="cards" actions={howLink}>
        {read.error !== undefined ? (
          <EmptyState
            title="Healthspan could not be read"
            description={describe(read.error)}
            action={{ label: "Try again", onClick: read.refresh }}
          />
        ) : (
          <div aria-busy="true" aria-label="Loading Healthspan">
            <CardGrid>
              <SkeletonCard />
              <SkeletonCard />
              <SkeletonCard />
            </CardGrid>
          </div>
        )}
      </Screen>
    )

  const sync = view.findings.filter((f) => f.subject.startsWith("sync"))
  const hasProfile = !!view.profile.birthDate && !!view.profile.sex
  const input = view.series.input
  const hasNights = input.sleeps.some(countsSleep) || input.rhr.length > 0
  const snaps = (view.report?.snapshots ?? []).filter(
    (s) => s.date >= axis.start && s.date <= axis.end
  )

  return (
    <Screen
      title="Healthspan"
      width={control.display.wide ? "full" : "cards"}
      actions={howLink}
      toolbar={toolbar}
    >
      {sync.length > 0 && (
        <div
          role="status"
          className="bg-info/12 mb-4 rounded-[10px] px-4 py-3 text-[15px]"
        >
          <p className="text-info-ink font-semibold">
            Some data has stopped arriving
          </p>
          <ul className="mt-1 text-secondary">
            {sync.map((f) => (
              <li key={f.subject}>
                <span className="text-label">{f.title}</span>: {f.detail}.
              </li>
            ))}
          </ul>
        </div>
      )}
      <HealthspanHero
        report={view.report}
        cards={cards}
        hasProfile={hasProfile}
        hasNights={hasNights}
      />
      {display.key && (
        <ChartKey
          className="mb-6"
          label="Chart key"
          items={healthspanKey(visible, {
            details: display.details && visible.length > 0,
            trendView: display.trendView && snaps.length > 0,
          })}
        />
      )}
      {view.report?.snapshots.length ? (
        <FocusList cards={cards} filters={filters} />
      ) : null}
      {visible.length === 0 && (
        <EmptyState
          title="No cards match"
          description="Change or clear the filters."
          action={{
            label: "Clear filters",
            onClick: () => set(DEFAULT_HEALTHSPAN_FILTERS),
          }}
        />
      )}
      {SECTIONS.map(({ title, kind }) => {
        const items = visible.filter((c) => c.kind === kind)
        if (!items.length) return null
        return (
          <section key={kind} className="mb-7" aria-label={title}>
            <h2 className="mb-1.5 flex items-baseline gap-2 px-4 text-[13px] tracking-wide text-secondary uppercase">
              {title}
              <span className="normal-case tabular-nums">{items.length}</span>
            </h2>
            <CardGrid wide={control.display.wide}>
              {items.map((c) => (
                <HealthspanCard
                  key={c.id}
                  card={c}
                  axis={axis}
                  isOpen={display.details}
                  isHighlighted={highlight === c.id}
                />
              ))}
            </CardGrid>
          </section>
        )
      })}
      {display.trendView && snaps.length > 0 && (
        <TrendView snaps={snaps} axis={axis} />
      )}
      <a
        href="#/healthspan/how"
        className="mb-4 flex min-h-11 items-center gap-2 rounded-[10px] bg-card px-4 text-[17px] text-accent-text"
      >
        <Info className="size-5 shrink-0" aria-hidden />
        How the age score works
      </a>
    </Screen>
  )
}
