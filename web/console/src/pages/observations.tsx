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
 * two units (`components/observations/timeline-panel.tsx`). */

import { useCallback, useEffect, useMemo, useState } from "react"
import { Link } from "@tanstack/react-router"
import type { SortingState, Updater } from "@tanstack/react-table"
import { ChartLineIcon } from "lucide-react"
import {
  parseAsBoolean,
  parseAsString,
  parseAsStringLiteral,
  useQueryState,
} from "nuqs"

import { DataGrid, type GridGroups } from "@/components/data-table/data-grid"
import { DataGridSort } from "@/components/data-table/data-grid-sort"
import { useDataTable } from "@/components/data-table/data-table"
import { DataTableFilters } from "@/components/data-table/data-table-filters"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { GroupByMenu } from "@/components/data-table/group-by"
import { ViewTabs } from "@/components/data-table/view-tabs"
import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import {
  DiscoveryError,
  FacetListErrors,
  MetricListError,
  RegistryError,
} from "@/components/observations/discovery-error"
import {
  AboutMark,
  GroupRecord,
  ShapeWord,
} from "@/components/observations/marks"
import {
  MeasurementSources,
  NoObservations,
} from "@/components/observations/no-observations"
import {
  selectColumn,
  streamColumns,
} from "@/components/observations/stream-columns"
import { TimelinePanel } from "@/components/observations/timeline-panel"
import { SearchBox } from "@/components/search-box"
import { Button } from "@/components/ui/button"
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
  distinct,
  refPathOf,
  SHAPE_WORDS,
  shapeOf,
  type ReadingShape,
} from "@/lib/observations"
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
  selectionState,
  shortRef,
  sortStreams,
  streamChoices,
  streamFields,
  streamGroupKey,
  streamMatches,
  streamViewState,
  tokenNames,
  type FieldChoice,
  type StreamField,
  type StreamRow,
} from "@/lib/stream-table"
import { ARRAY } from "@/lib/timeline-address"
import { cn } from "@/lib/utils"

/** The page's side gutter, shared by every band so they line up. */
const GUTTER = "px-4 md:px-8"
const GUTTER_MX = "mx-4 md:mx-8"

/** Where the last view of the table is kept in this browser
 * (`lib/filters.ts` loadBrowsePrefs). */
const PREFS: [string, string] = ["observations", "streams"]

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
  // Chosen metrics narrow the reads on the server by metric alone, and the
  // table's filter narrows their streams by source and thing. With no metric
  // chosen, the chosen sources and things narrow the one read on the server.
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
  const pick = useCallback(
    (row: StreamRow, checked: boolean, shift: boolean) => {
      const at = shown.findIndex((r) => r.key === row.key)
      const from =
        shift && anchor ? shown.findIndex((r) => r.key === anchor) : at
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
    },
    [shown, anchor, selection, pins, setSelection]
  )
  const headState = selectionState(selection, shown)
  const pickAll = useCallback(() => {
    const next = selectRange(
      selection,
      shown,
      0,
      shown.length - 1,
      headState !== "all",
      pins
    )
    void setSelection(next.length ? next : null)
  }, [selection, shown, headState, pins, setSelection])

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
  // The box column reads the selection, so it is rebuilt when that or the
  // rows shown change, and only then: a render that moves neither (a column
  // being resized) keeps the table's column model.
  const gridColumns = useMemo(
    () => [
      selectColumn({
        state: headState,
        selected,
        onAll: pickAll,
        onPick: pick,
      }),
      ...columns,
    ],
    [headState, selected, pickAll, pick, columns]
  )
  const table = useDataTable({
    columns: gridColumns,
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
          // The rows shown, which a filter or the search narrows: a count of
          // what the server read would change with some filters and not
          // others.
          shown.length > 0 && (
            <span>
              {shown.length === 1 ? "1 stream" : `${shown.length} streams`}
              {filtered && (shown.length === 1 ? " matches" : " match")}
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
          {obs.registryError ? (
            <RegistryError
              error={obs.registryError}
              onRetry={obs.retryRegistry}
            />
          ) : (
            <NoObservations />
          )}
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
          // Rows of an earlier read under other filters may match none of
          // these: the grid waits for the read rather than say none match.
          loading={
            obs.discovery.isPending ||
            (obs.discovery.isHeld && shown.length === 0)
          }
          selected={selected}
          groups={gridGroups}
          label="Streams"
          empty={
            filtered ? (
              <p className="px-2.5 py-8 text-muted-foreground">
                No stream matches these filters.
              </p>
            ) : (
              <div className="px-2.5 py-8">
                <p className="text-muted-foreground">
                  Nothing has been measured yet. Streams show up here once a
                  provider or a sample writes its first value.
                </p>
                <MeasurementSources className="mt-3" />
              </div>
            )
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
