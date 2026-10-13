/** Observations (`/observations`): one row per stream, a metric written by
 * one source about one thing (on one of the source's channels), with its
 * newest value, from the latest read (`aggregate` grouped by metric,
 * source, about and channel, the one reducer `last`, no bucket and no
 * range). Shown only where a kind binds core's `observed` trait.
 *
 * The rows sit in the records grid every collection uses, with its
 * machinery: a sort in the address, the filter bar over the stream columns
 * (metric, source, about, reading, channel, each a set of values to pick), a
 * search, column options, grouping, and saved views kept on the console
 * preference record under their own collection key (`lib/stream-table.ts`).
 * The metric is the row's link: it opens the timeline narrowed to that
 * stream.
 *
 * A box on each row selects it (shift for a run, the header box for every
 * row shown), the selection kept in the address. The Timeline toggle opens
 * a panel above the grid drawing the selected streams in one chart of up to
 * two units (`components/observations/overlay-chart.tsx`). */

import { useEffect, useMemo, useState, type MouseEvent } from "react"
import { Link } from "@tanstack/react-router"
import type { SortingState, Updater } from "@tanstack/react-table"
import { ChartLineIcon, XIcon } from "lucide-react"
import {
  parseAsArrayOf,
  parseAsBoolean,
  parseAsString,
  parseAsStringLiteral,
  useQueryState,
} from "nuqs"

import { timeText } from "@/components/data-table/columns"
import { DataGrid, type GridGroups } from "@/components/data-table/data-grid"
import { GridColumnHeader } from "@/components/data-table/data-grid-header"
import { DataGridSort } from "@/components/data-table/data-grid-sort"
import {
  useDataTable,
  type DataTableColumn,
} from "@/components/data-table/data-table"
import { DataTableFilters } from "@/components/data-table/data-table-filters"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { GroupByMenu } from "@/components/data-table/group-by"
import { ViewTabs } from "@/components/data-table/view-tabs"
import { EmptyValue } from "@/components/identity/empty-value"
import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import {
  DiscoveryError,
  FacetListErrors,
  MetricListError,
} from "@/components/observations/discovery-error"
import {
  AboutMark,
  CodeMark,
  GroupRecord,
  Reading,
  ShapeWord,
} from "@/components/observations/marks"
import { NoObservations } from "@/components/observations/no-observations"
import { OverlayChart } from "@/components/observations/overlay-chart"
import { Labelled } from "@/components/observations/picker"
import { RangePicker } from "@/components/observations/range-picker"
import { SearchBox } from "@/components/search-box"
import { Button } from "@/components/ui/button"
import { Segmented } from "@/components/ui/segmented"
import { Skeleton } from "@/components/ui/skeleton"
import {
  useConsolePreferences,
  useDensity,
  useLayoutWidths,
  useTechnicalDetails,
} from "@/hooks/use-console-preferences"
import {
  useFacetValues,
  useObservations,
  type Observations,
} from "@/hooks/use-observations"
import { encodeFilter, loadBrowsePrefs, saveBrowsePrefs } from "@/lib/filters"
import {
  DEFAULT_PRECISION,
  distinct,
  refPathOf,
  SHAPE_WORDS,
  shapeOf,
  streamSearch,
  type ReadingShape,
} from "@/lib/observations"
import {
  BUCKET_CHOICES,
  BUCKET_SIZE_WORDS,
  bucketSizeOf,
  coverageWords,
  presetRange,
  RANGE_PRESETS,
  readerTimezone,
  type BucketChoice,
  type RangePreset,
  type TimeRange,
} from "@/lib/observations-time"
import { GROUP_CHOICES, type GroupChoice } from "@/lib/observations-reads"
import { readChoices, writeChoice } from "@/lib/timeline-address"
import { planPlot } from "@/lib/overlay"
import {
  isAllView,
  matchingView,
  newViewId,
  viewFromState,
  viewsOf,
  type SavedView,
} from "@/lib/saved-views"
import {
  DEFAULT_STREAM_SORT,
  FIELD_LABELS,
  OWNER,
  STREAM_GROUPS,
  STREAM_VIEWS,
  choiceFilter,
  groupStreams,
  isStreamGroup,
  parseStreamSort,
  selectRange,
  selectedStreams,
  selectionByMetric,
  selectionState,
  shortRef,
  sortStreams,
  streamChoices,
  streamFields,
  streamGroupKey,
  streamMatches,
  streamViewState,
  tokenMetric,
  tokenNames,
  type FieldChoice,
  type StreamField,
  type StreamRow,
} from "@/lib/stream-table"
import { cn } from "@/lib/utils"

const ARRAY = parseAsArrayOf(parseAsString).withDefault([])
const RANGE_VALUES = [...RANGE_PRESETS.map((p) => p.value), "custom"] as const
const BUCKET_VALUES = BUCKET_CHOICES.map((b) => b.value)
const CHART_GROUPS = GROUP_CHOICES.map((g) => g.value)
/** The panel opens on a month: long enough to compare, daily buckets. */
const PANEL_RANGE: RangePreset = "30d"

/** The page's side gutter, shared by every band so they line up. */
const GUTTER = "px-4 md:px-8"
const GUTTER_MX = "mx-4 md:mx-8"

/** Where the last view of the table is kept in this browser
 * (`lib/filters.ts` loadBrowsePrefs). */
const PREFS: [string, string] = ["observations", "streams"]

function customRange(
  start: string | null,
  end: string | null
): TimeRange | undefined {
  const a = start ? Date.parse(start) : NaN
  const b = end ? Date.parse(end) : NaN
  return Number.isNaN(a) || Number.isNaN(b) || b <= a
    ? undefined
    : { start: a, end: b }
}

export function ObservationsPage() {
  const [technical] = useTechnicalDetails()
  const [density] = useDensity()
  const { tableWidth } = useLayoutWidths()
  const { preferences, busy, change } = useConsolePreferences()

  // The table's shape, in the address so a view can be shared.
  const [filterTokens, setFilterTokens] = useQueryState("filter", ARRAY)
  const [search, setSearch] = useQueryState(
    "search",
    parseAsString.withDefault("")
  )
  const [sort, setSort] = useQueryState(
    "sort",
    parseAsString.withDefault(DEFAULT_STREAM_SORT)
  )
  const [group, setGroup] = useQueryState(
    "group",
    parseAsStringLiteral(STREAM_GROUPS)
  )
  // The selection and the panel: in the address, never in a view.
  const [selection, setSelection] = useQueryState("select", ARRAY)
  const [panel, setPanel] = useQueryState(
    "timeline",
    parseAsBoolean.withDefault(false)
  )

  // A BARE address restores the last view of the table, one dimension at a
  // time; an address that names one always wins. Writes happen in the
  // change handlers, never here.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const stored = loadBrowsePrefs(...PREFS)
    if (!stored) return
    if (!params.has("filter") && stored.filter?.length) {
      void setFilterTokens(stored.filter, { history: "replace" })
    }
    if (!params.has("sort") && stored.sort) {
      void setSort(stored.sort, { history: "replace" })
    }
    if (!params.has("group") && isStreamGroup(stored.group)) {
      void setGroup(stored.group, { history: "replace" })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once
  }, [])

  function persist(next: {
    filter?: string[]
    sort?: string
    group?: StreamField | null
  }) {
    const nextSort = next.sort ?? sort
    saveBrowsePrefs(...PREFS, {
      filter: next.filter ?? filterTokens,
      sort: nextSort === DEFAULT_STREAM_SORT ? undefined : nextSort,
      group: (next.group === undefined ? group : next.group) ?? undefined,
    })
  }

  const choices = useMemo(() => streamChoices(filterTokens), [filterTokens])
  // The chosen metrics, sources and things narrow the reads on the server.
  // "You" names no record, so a filter holding it narrows things here only.
  const narrow = useMemo(
    () => ({
      metrics: choices.metric,
      sources: choices.source,
      abouts: choices.about.includes(OWNER) ? [] : choices.about,
    }),
    [choices]
  )
  const obs = useObservations(narrow)
  // Each field's values under the other choices, so a chosen source's menu
  // still offers the sources a reader can add.
  const values = useFacetValues(obs, narrow)

  const rows = useMemo<StreamRow[]>(
    () =>
      obs.streams.map((s) => ({
        ...s,
        metricName: obs.metricName(s.metric),
        sourceTitle: obs.titleOf(refPathOf(s.source, obs.pins.source)),
        aboutTitle: obs.titleOf(refPathOf(s.about, obs.pins.about)),
        // The stream's own form and code, which the index read, before the
        // metric record's default.
        shape: shapeOf(obs.metricOf(s.metric), s),
      })),
    [obs]
  )

  // A record a field offers is spelled by id where its pin allows, the
  // spelling the address keeps, so a picked value and a linked one agree.
  const spell = useMemo(() => {
    const pins: Partial<Record<StreamField, string | undefined>> = {
      metric: obs.pins.metric,
      source: obs.pins.source,
      about: obs.pins.about,
    }
    return (field: StreamField, value: string) =>
      field in pins ? shortRef(value, pins[field]) : value
  }, [obs.pins])
  const fields = useMemo(() => {
    // Every metric with a point, so choosing one never hides the others.
    const metrics = new Map(
      obs.metrics.map((m) => [spell("metric", m), obs.metricName(m)])
    )
    for (const r of rows) metrics.set(spell("metric", r.metric), r.metricName)
    const named = (
      field: StreamField,
      list: string[],
      pin?: string
    ): FieldChoice[] =>
      list.map((value) => ({
        value: spell(field, value),
        label: values.nameOf(value, pin),
      }))
    return streamFields({
      metric: [...metrics]
        .map(([value, label]) => ({ value, label }))
        .sort((a, b) => a.label.localeCompare(b.label)),
      source: named("source", values.sources, obs.pins.source),
      about: values.abouts.length
        ? [
            { value: OWNER, label: "You" },
            ...named("about", values.abouts, obs.pins.about),
          ]
        : [],
      shape: distinct(rows.map((r) => r.shape)).map((value) => ({
        value,
        label: SHAPE_WORDS[value as ReadingShape],
      })),
      channel: distinct(rows.map((r) => r.channel)).map((value) => ({
        value,
        label: value,
      })),
    })
  }, [obs, rows, values, spell])

  // The words a group head and a group order read: a metric's name, a
  // record's title, a form's word.
  function groupWords(field: StreamField, key: string): string {
    if (!key) return field === "about" ? "You" : `No ${field}`
    if (field === "metric") return obs.metricName(key)
    if (field === "source") return obs.nameOf(key, obs.pins.source)
    if (field === "about") return obs.nameOf(key, obs.pins.about)
    if (field === "shape") return SHAPE_WORDS[key as ReadingShape] ?? key
    return key
  }

  const shown = useMemo(() => {
    const matched = rows.filter((r) => streamMatches(r, choices, search))
    const sorted = sortStreams(matched, sort)
    return group
      ? groupStreams(sorted, group, (key) => groupWords(group, key))
      : sorted
    // groupWords reads obs, which `rows` already follows.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, choices, search, sort, group])

  // ── selection ────────────────────────────────────────────────────────────
  const pins = obs.pins
  const selected = useMemo(
    () =>
      new Set(
        shown
          .filter((r) => selection.some((t) => tokenNames(t, r)))
          .map((r) => r.key)
      ),
    [shown, selection]
  )
  const [anchor, setAnchor] = useState<string | null>(null)
  function pick(row: StreamRow, checked: boolean, shift: boolean) {
    const at = shown.findIndex((r) => r.key === row.key)
    const from = shift && anchor ? shown.findIndex((r) => r.key === anchor) : at
    const next = selectRange(
      selection,
      shown,
      from < 0 ? at : from,
      at,
      checked,
      pins
    )
    setAnchor(row.key)
    void setSelection(next.length ? next : null)
  }
  const headState = selectionState(selection, shown)
  function pickAll() {
    const next = selectRange(
      selection,
      shown,
      0,
      shown.length - 1,
      headState !== "all",
      pins
    )
    void setSelection(next.length ? next : null)
  }

  // ── columns, sort, views ─────────────────────────────────────────────────
  const columns = useMemo(() => streamColumns(obs, technical), [obs, technical])
  const autoHidden = useMemo(
    () => [
      ...(rows.some((r) => r.count !== undefined) ? [] : ["count"]),
      ...(rows.some((r) => r.coverage) ? [] : ["coverage"]),
      ...(rows.some((r) => r.about) ? [] : ["about"]),
      ...(rows.some((r) => r.channel) ? [] : ["channel"]),
    ],
    [rows]
  )
  const sorting = useMemo<SortingState>(() => {
    const { id, desc } = parseStreamSort(sort)
    return [{ id, desc }]
  }, [sort])
  function onSortingChange(updater: Updater<SortingState>) {
    const next = typeof updater === "function" ? updater(sorting) : updater
    const first = next[0]
    const nextSort = first
      ? `${first.id}:${first.desc ? "desc" : "asc"}`
      : DEFAULT_STREAM_SORT
    void setSort(nextSort === DEFAULT_STREAM_SORT ? null : nextSort)
    persist({ sort: nextSort })
  }
  // The box column reads the selection, so it is rebuilt as that moves.
  const boxes = selectColumn({
    state: headState,
    selected,
    onAll: pickAll,
    onPick: pick,
  })
  const table = useDataTable({
    columns: [boxes, ...columns],
    data: shown,
    sorting,
    onSortingChange,
    getRowId: (r) => r.key,
    prefsKey: "observations:streams",
    autoHidden,
  })

  const views = viewsOf(preferences.views ?? [], STREAM_VIEWS)
  const viewState = streamViewState({
    filter: filterTokens,
    sort,
    group,
    columns: table.state.columnOrder,
    hidden: table.options.meta?.readerHidden ?? [],
  })
  const [picked, setPicked] = useState<string>()
  const matchedView = matchingView(
    views,
    viewState,
    DEFAULT_STREAM_SORT,
    picked
  )
  const activeView = matchedView
    ? matchedView.id
    : isAllView(viewState, DEFAULT_STREAM_SORT)
      ? "all"
      : null
  const editedView =
    activeView === null && picked && views.some((v) => v.id === picked)
      ? picked
      : null

  function saveView(name: string) {
    const view = viewFromState(
      {
        id: newViewId(preferences.views ?? []),
        collection: STREAM_VIEWS,
        name,
      },
      viewState,
      DEFAULT_STREAM_SORT
    )
    change({ type: "view", view })
    setPicked(view.id)
  }

  /** Show a saved view, or with none the table as it opens. The search is
   * the question of the moment and is cleared; the selection and the panel
   * are kept. */
  function pickView(view: SavedView | null) {
    setPicked(view?.id)
    const filter = view?.filter ?? []
    const nextSort = view?.sort ?? DEFAULT_STREAM_SORT
    const nextGroup = isStreamGroup(view?.group) ? view.group : null
    void setFilterTokens(filter.length ? filter : null)
    void setSort(nextSort === DEFAULT_STREAM_SORT ? null : nextSort)
    void setGroup(nextGroup)
    void setSearch(null)
    if (view?.columns || view?.hidden) {
      table.options.meta?.applyColumns?.({
        order: view.columns,
        hidden: view.hidden,
      })
    }
    persist({ filter, sort: nextSort, group: nextGroup })
  }

  function changeGroup(next: string | null) {
    const value = isStreamGroup(next) ? next : null
    void setGroup(value)
    persist({ group: value })
  }

  // Folded groups, forgotten when the grouping changes.
  const [folded, setFolded] = useState<{
    group?: StreamField
    keys: Set<string>
  }>({ keys: new Set() })
  const gridGroups: GridGroups<StreamRow> | undefined = group
    ? {
        keyOf: (row) => streamGroupKey(row, group),
        head: (key, at) => (
          <>
            <GroupValue obs={obs} field={group} value={key} />
            <span className="text-[12.5px] font-normal text-faint tabular-nums">
              {at.rows === 1 ? "1 stream" : `${at.rows} streams`}
            </span>
          </>
        ),
        label: (key) => `${FIELD_LABELS[group]}: ${groupWords(group, key)}`,
        collapsed: folded.group === group ? folded.keys : NO_KEYS,
        onToggle: (key) =>
          setFolded((prev) => {
            const keys = new Set(prev.group === group ? prev.keys : [])
            if (keys.has(key)) keys.delete(key)
            else keys.add(key)
            return { group, keys }
          }),
      }
    : undefined
  const filtered = filterTokens.length > 0 || Boolean(search)
  const count = selection.length

  const header = (
    <div className={cn("shrink-0 pt-6", GUTTER)}>
      <PageHeader
        title="Observations"
        meta={
          obs.streams.length > 0 && (
            <span>
              {obs.streams.length === 1
                ? "1 stream"
                : `${obs.streams.length} streams`}
            </span>
          )
        }
        description="What your devices, apps and providers measured: one row for each metric from each source, with its newest value. Select streams to draw them together, or open one to see it over time."
        actions={
          obs.observed.length > 0 && (
            <Button
              variant="outline"
              size="sm"
              render={<Link to="/observations/timeline" />}
              nativeButton={false}
            >
              <ChartLineIcon />
              Timeline page
            </Button>
          )
        }
      />
    </div>
  )

  if (obs.pending) {
    return (
      <TablePage>
        <Skeleton className="h-7 w-48" />
        <Skeleton className="mt-3 h-4 w-96" />
      </TablePage>
    )
  }

  if (obs.observed.length === 0) {
    return (
      <TablePage className="px-0 py-0 md:px-0">
        {header}
        <div className={GUTTER}>
          <NoObservations />
        </div>
      </TablePage>
    )
  }

  return (
    <TablePage
      className={cn(
        "flex flex-col px-0 py-0 md:px-0",
        // With the panel open the page scrolls as one; closed, the grid
        // fills it and scrolls on its own.
        panel ? "pb-6" : "min-h-0 flex-1"
      )}
    >
      {header}
      <ViewTabs
        className={cn("mt-3", GUTTER_MX)}
        views={views}
        active={activeView}
        edited={editedView}
        busy={busy}
        onPick={pickView}
        onSave={saveView}
        onRename={(view, name) =>
          change({ type: "view", view: { ...view, name } })
        }
        onReplace={(view) =>
          change({
            type: "view",
            view: viewFromState(view, viewState, DEFAULT_STREAM_SORT),
          })
        }
        onDelete={(view) => change({ type: "forget-view", id: view.id })}
      />
      <div
        className={cn(
          "flex shrink-0 flex-wrap items-center gap-1.5 border-b border-border py-2.5",
          GUTTER_MX
        )}
      >
        <button
          type="button"
          aria-pressed={panel}
          title={
            panel
              ? "Hide the timeline of the selected streams"
              : "Draw the selected streams over time, in one chart"
          }
          className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-border-strong bg-background px-2.5 text-[12.5px] text-muted-foreground outline-none hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring/50 aria-pressed:border-transparent aria-pressed:bg-primary-soft aria-pressed:text-primary-text"
          onClick={() => void setPanel(panel ? null : true)}
        >
          <ChartLineIcon aria-hidden className="size-3.5" />
          Timeline
          {count > 0 && (
            <span className="rounded-full bg-hover px-1.5 text-[11.5px] leading-[18px] tabular-nums">
              {count} selected
            </span>
          )}
        </button>
        <GroupByMenu
          options={fields.filter((f) => isStreamGroup(f.name))}
          value={group ?? undefined}
          labelOf={(name) => FIELD_LABELS[name as StreamField] ?? name}
          onChange={changeGroup}
        />
        <DataTableFilters
          className="gap-1.5 px-0 py-0"
          fields={fields}
          filters={(Object.keys(choices) as StreamField[]).flatMap((f) =>
            choices[f].length
              ? [
                  choiceFilter(
                    f,
                    choices[f].map((v) => spell(f, v))
                  ),
                ]
              : []
          )}
          labelOf={(name) => FIELD_LABELS[name as StreamField] ?? name}
          tags={false}
          onChange={(next) => {
            const tokens = next.map(encodeFilter)
            void setFilterTokens(tokens.length ? tokens : null)
            persist({ filter: tokens })
          }}
        />
        <span className="flex-1" />
        <SearchBox
          className="h-[30px] w-48 [&_input]:font-sans"
          label="Search streams"
          placeholder="Search streams"
          value={search}
          onChange={(next) => void setSearch(next || null)}
        />
        <DataGridSort table={table} />
        <DataTableViewOptions table={table} toolbar />
      </div>
      {!obs.discovery.isError && (
        <FacetListErrors className={cn("mt-2.5", GUTTER_MX)} values={values} />
      )}
      {obs.metricListError && (
        <MetricListError
          className={cn("mt-2.5", GUTTER_MX)}
          error={obs.metricListError}
          onRetry={() => obs.discovery.refetch()}
        />
      )}
      {panel && (
        <TimelinePanel
          selection={selection}
          onClear={() => {
            void setSelection(null)
            setAnchor(null)
          }}
          className={cn("border-b border-border py-3", GUTTER_MX)}
        />
      )}
      {obs.discovery.isError ? (
        <div className={GUTTER}>
          <DiscoveryError discovery={obs.discovery} />
        </div>
      ) : (
        <DataGrid
          table={table}
          density={density}
          fill={tableWidth === "full"}
          loading={obs.discovery.isPending}
          selected={selected}
          groups={gridGroups}
          label="Streams"
          empty={
            <p className="px-2.5 py-8 text-muted-foreground">
              {filtered
                ? "No stream matches these filters."
                : "Nothing has been measured yet. Streams show up here once a provider or a sample writes its first value."}
            </p>
          }
          className={cn(
            "border-b border-border",
            panel ? "max-h-[70vh]" : "flex-1",
            obs.discovery.isFetching &&
              !obs.discovery.isPending &&
              "opacity-60 transition-opacity",
            GUTTER_MX
          )}
        />
      )}
      {technical && (
        <p className={cn("mt-3 text-[12.5px] text-faint", GUTTER)}>
          Read from{" "}
          <span className="font-mono">
            {obs.observedIds.join(", ") || "no kind"}
          </span>
          , every kind binding{" "}
          <span className="font-mono">substrate.reamde.dev/core/observed</span>.
        </p>
      )}
    </TablePage>
  )
}

const NO_KEYS: ReadonlySet<string> = new Set()

/** A group head's value, drawn the way the grid's cells draw it. */
function GroupValue({
  obs,
  field,
  value,
}: {
  obs: Observations
  field: StreamField
  value: string
}) {
  if (field === "about") {
    return <AboutMark value={value || undefined} pin={obs.pins.about} />
  }
  if (!value) {
    return (
      <span className="font-normal text-muted-foreground">
        No {FIELD_LABELS[field].toLowerCase()}
      </span>
    )
  }
  if (field === "metric") return <span>{obs.metricName(value)}</span>
  if (field === "source") {
    return <GroupRecord value={value} pin={obs.pins.source} />
  }
  if (field === "shape") return <ShapeWord shape={value as ReadingShape} />
  return <span>{value}</span>
}

/** A row's or the header's selection box. The header's is mixed while
 * some rows are selected and others not. */
function SelectBox({
  checked,
  mixed = false,
  label,
  onClick,
}: {
  checked: boolean
  mixed?: boolean
  label: string
  onClick: (event: MouseEvent<HTMLInputElement>) => void
}) {
  return (
    <input
      type="checkbox"
      aria-label={label}
      className="size-3.5 cursor-pointer align-middle accent-primary"
      checked={checked}
      ref={(el) => {
        if (el) el.indeterminate = mixed
      }}
      onChange={() => {}}
      onClick={onClick}
    />
  )
}

function selectColumn({
  state,
  selected,
  onAll,
  onPick,
}: {
  state: "all" | "some" | "none"
  selected: ReadonlySet<string>
  onAll: () => void
  onPick: (row: StreamRow, checked: boolean, shift: boolean) => void
}): DataTableColumn<StreamRow> {
  return {
    id: "select",
    enableSorting: false,
    enableHiding: false,
    header: () => (
      <SelectBox
        checked={state === "all"}
        mixed={state === "some"}
        label={
          state === "all"
            ? "Clear every stream shown"
            : "Select every stream shown"
        }
        onClick={onAll}
      />
    ),
    cell: ({ row }) => (
      <SelectBox
        checked={selected.has(row.original.key)}
        label={`Select ${row.original.metricName}${row.original.sourceTitle ? ` from ${row.original.sourceTitle}` : ""}${row.original.channel ? ` (${row.original.channel})` : ""}`}
        onClick={(e) =>
          onPick(row.original, e.currentTarget.checked, e.shiftKey)
        }
      />
    ),
    meta: { label: "Select", width: 36, fixed: true },
  }
}

function streamColumns(
  obs: Observations,
  technical: boolean
): DataTableColumn<StreamRow>[] {
  return [
    {
      id: "metric",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Metric" />
      ),
      accessorFn: (r) => r.metricName,
      cell: ({ row }) => (
        <Link
          to="/observations/timeline"
          search={streamSearch(row.original) as never}
          className="block truncate font-medium underline-offset-[3px] hover:underline hover:decoration-border-strong"
          title={technical ? row.original.metric : undefined}
        >
          {row.original.metricName}
        </Link>
      ),
      meta: { label: "Metric", width: 200 },
    },
    {
      id: "source",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Source" />
      ),
      accessorFn: (r) => r.source,
      cell: ({ row }) =>
        row.original.source ? (
          <GroupRecord
            value={row.original.source}
            pin={obs.pins.source}
            title={row.original.sourceTitle}
          />
        ) : (
          <EmptyValue />
        ),
      meta: { label: "Source", width: 210 },
    },
    {
      id: "about",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="About" />
      ),
      accessorFn: (r) => r.about,
      cell: ({ row }) => (
        <AboutMark
          value={row.original.about}
          pin={obs.pins.about}
          title={row.original.aboutTitle}
        />
      ),
      meta: { label: "About", width: 130 },
    },
    {
      id: "channel",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Channel" />
      ),
      accessorFn: (r) => r.channel,
      cell: ({ row }) => row.original.channel ?? <EmptyValue />,
      meta: { label: "Channel", width: 100 },
    },
    {
      id: "shape",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Reading" />
      ),
      accessorFn: (r) => r.shape,
      cell: ({ row }) => <ShapeWord shape={row.original.shape} />,
      meta: { label: "Reading", width: 124 },
    },
    {
      id: "latest",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Latest" />
      ),
      accessorFn: (r) => r.value,
      cell: ({ row }) =>
        row.original.code ? (
          <CodeMark code={row.original.code} />
        ) : (
          <Reading
            value={row.original.value}
            unit={row.original.unit}
            // Shown to the metric's places, the stored digits on hover;
            // with Technical details on, the stored digits as they are.
            precision={
              technical
                ? undefined
                : (obs.metricOf(row.original.metric)?.displayPrecision ??
                  DEFAULT_PRECISION)
            }
          />
        ),
      meta: { label: "Latest", width: 124 },
    },
    {
      id: "when",
      header: ({ column }) => <GridColumnHeader column={column} label="When" />,
      accessorFn: (r) => r.at,
      cell: ({ row }) => (
        // The wire's instant on hover, as every time cell has it.
        <span
          className="block truncate data text-muted-foreground"
          title={row.original.at}
        >
          {timeText(row.original.at, "datetime")}
        </span>
      ),
      meta: { label: "When", width: 136 },
    },
    {
      id: "count",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Points" align="right" />
      ),
      accessorFn: (r) => r.count,
      cell: ({ row }) =>
        row.original.count === undefined ? (
          <EmptyValue />
        ) : (
          <span className="tabular-nums">
            {row.original.count.toLocaleString("en")}
          </span>
        ),
      meta: {
        label: "Points",
        width: 90,
        headerClassName: "text-right",
        cellClassName: "text-right",
      },
    },
    {
      id: "coverage",
      header: ({ column }) => (
        <GridColumnHeader column={column} label="Covered" />
      ),
      accessorFn: (r) => r.coverage,
      cell: ({ row }) =>
        row.original.coverage ? (
          <span title={row.original.coverage}>
            {coverageWords(row.original.coverage)}
          </span>
        ) : (
          <EmptyValue />
        ),
      meta: { label: "Covered", width: 110 },
    },
  ]
}

/** The panel the Timeline toggle opens: the selected streams over a range
 * in one chart of up to two units, with the range, the bucket and the
 * grouping, and each metric's reducer and unit, in the address. */
function TimelinePanel({
  selection,
  onClear,
  className,
}: {
  selection: string[]
  onClear: () => void
  className?: string
}) {
  const [rangeParam, setRangeParam] = useQueryState(
    "range",
    parseAsStringLiteral(RANGE_VALUES).withDefault(PANEL_RANGE)
  )
  const [startParam, setStart] = useQueryState("start", parseAsString)
  const [endParam, setEnd] = useQueryState("end", parseAsString)
  const [bucket, setBucket] = useQueryState(
    "bucket",
    parseAsStringLiteral(BUCKET_VALUES).withDefault("auto")
  )
  const [by, setBy] = useQueryState(
    "by",
    parseAsStringLiteral(CHART_GROUPS).withDefault("metric")
  )
  const [reducerTokens, setReducerTokens] = useQueryState("reducer", ARRAY)
  const [unitTokens, setUnitTokens] = useQueryState("unit", ARRAY)
  // Presets count back from when the panel opened, so a re-render never
  // moves the window (and never re-asks).
  const [now] = useState(() => Date.now())
  const timezone = useMemo(() => readerTimezone(), [])

  const custom =
    rangeParam === "custom" ? customRange(startParam, endParam) : undefined
  const preset: RangePreset | "custom" = custom
    ? "custom"
    : rangeParam === "custom"
      ? PANEL_RANGE
      : rangeParam
  const range = custom ?? presetRange(preset as RangePreset, now)
  const size = bucketSizeOf(bucket, range)

  // The selection's own streams, read apart from the table's filters, so
  // narrowing the table never drops a selected stream from the chart.
  const metrics = useMemo(
    () => [...new Set(selection.map(tokenMetric).filter(Boolean))],
    [selection]
  )
  const own = useObservations({ metrics })
  const picked = selectedStreams(selection, own.streams)
  const reducers = readChoices(reducerTokens)
  const units = readChoices(unitTokens)
  const plots = selectionByMetric(picked).map((entry) =>
    // By id where the pin allows, so a metric's reducer and unit read
    // short in the address.
    planPlot(own, shortRef(entry.metric, own.pins.metric), {
      range,
      size,
      timezone,
      group: by,
      sources: entry.sources,
      abouts: entry.abouts,
      channels: entry.channels,
      streams: entry.streams,
      reducer: reducers.get(shortRef(entry.metric, own.pins.metric)),
      unit: units.get(shortRef(entry.metric, own.pins.metric)),
    })
  )
  const missing = own.ready ? selection.length - picked.length : 0

  function zoom(next: TimeRange) {
    void setRangeParam("custom")
    void setStart(new Date(next.start).toISOString())
    void setEnd(new Date(next.end).toISOString())
    void setBucket(null)
  }

  return (
    <section
      aria-label="Timeline of the selected streams"
      data-slot="stream-timeline"
      className={cn("shrink-0", className)}
    >
      <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
        <h2 className="text-[13px] font-semibold">
          {picked.length === 0
            ? "Timeline"
            : picked.length === 1
              ? "1 stream over time"
              : `${picked.length} streams over time`}
        </h2>
        <RangePicker
          preset={preset}
          range={range}
          onPreset={(p) => {
            void setRangeParam(p === PANEL_RANGE ? null : p)
            void setStart(null)
            void setEnd(null)
          }}
          onCustom={(next) => {
            void setRangeParam("custom")
            void setStart(new Date(next.start).toISOString())
            void setEnd(new Date(next.end).toISOString())
          }}
        />
        <Labelled label="Bucket">
          <Segmented
            label="Bucket"
            value={bucket}
            options={BUCKET_CHOICES.map((b) => ({
              value: b.value,
              label:
                b.value === "auto" && bucket === "auto"
                  ? `Automatic: ${BUCKET_SIZE_WORDS[size]}`
                  : b.label,
            }))}
            onChange={(next: BucketChoice) =>
              void setBucket(next === "auto" ? null : next)
            }
          />
        </Labelled>
        <Labelled label="Group by">
          <Segmented
            label="Group by"
            value={by}
            options={GROUP_CHOICES}
            onChange={(next: GroupChoice) =>
              void setBy(next === "metric" ? null : next)
            }
          />
        </Labelled>
        <span className="flex-1" />
        {selection.length > 0 && (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1 text-[12.5px] font-normal text-muted-foreground"
            onClick={onClear}
          >
            <XIcon className="size-3.5" />
            Clear the selection
          </Button>
        )}
      </div>
      {missing > 0 && (
        <p className="mt-2 text-[12.5px] text-faint">
          {missing === 1
            ? "1 selected stream has no points any more, so the chart leaves it out."
            : `${missing} selected streams have no points any more, so the chart leaves them out.`}
        </p>
      )}
      {!own.ready && selection.length > 0 ? (
        <Skeleton className="mt-3 h-[250px] w-full" />
      ) : picked.length === 0 ? (
        <p className="mt-3 text-muted-foreground">
          Select streams in the table to draw them here, in one chart. Streams
          of one unit share an axis; a second unit reads on the right.
        </p>
      ) : (
        <OverlayChart
          className="mt-3"
          obs={own}
          plots={plots}
          range={range}
          size={size}
          onReducer={(metric, value) => {
            const next = writeChoice(reducerTokens, metric, value)
            void setReducerTokens(next.length ? next : null)
          }}
          onUnit={(metric, value) => {
            const next = writeChoice(unitTokens, metric, value)
            void setUnitTokens(next.length ? next : null)
          }}
          onZoom={zoom}
        />
      )}
    </section>
  )
}
