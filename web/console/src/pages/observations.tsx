/** Observations (`/observations`): one row per stream, a metric written by
 * one source about one thing, with its newest value, from the latest read
 * (`aggregate` grouped by metric, source and about, the one reducer `last`,
 * no bucket and no range). Filters for metric, source, about and form and a
 * search narrow the rows in the page; a row opens the timeline narrowed to
 * that stream. Shown only where a kind binds core's `observed` trait. */

import { useMemo } from "react"
import { Link, useNavigate } from "@tanstack/react-router"
import { ChartLineIcon } from "lucide-react"
import { parseAsArrayOf, parseAsString, useQueryState } from "nuqs"

import { timeColumn } from "@/components/data-table/columns"
import {
  DataTable,
  useDataTable,
  type DataTableColumn,
} from "@/components/data-table/data-table"
import { EmptyValue } from "@/components/identity/empty-value"
import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import {
  FacetFilters,
  type Facet,
} from "@/components/observations/facet-filters"
import {
  AboutMark,
  CodeMark,
  GroupRecord,
  Reading,
  ShapeWord,
} from "@/components/observations/marks"
import {
  DiscoveryError,
  MetricListError,
} from "@/components/observations/discovery-error"
import { NoObservations } from "@/components/observations/no-observations"
import { SearchBox } from "@/components/search-box"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  useFacetValues,
  useObservations,
  type Observations,
} from "@/hooks/use-observations"
import {
  SHAPE_WORDS,
  coverageWords,
  refPathOf,
  sameRef,
  shapeOf,
  streamSearch,
  type ReadingShape,
  type Stream,
} from "@/lib/observations"
import { cn } from "@/lib/utils"

/** The facet value for a stream about the owner, which names no record. An
 * id never starts with `~`, so this can never be one. */
const OWNER = "~you"

/** One table row: the stream and the words its marks read as. */
interface StreamRow extends Stream {
  metricName: string
  sourceTitle?: string
  aboutTitle?: string
  shape: ReadingShape
}

const ARRAY = parseAsArrayOf(parseAsString).withDefault([])

function rowsOf(obs: Observations): StreamRow[] {
  return obs.streams.map((s) => ({
    ...s,
    metricName: obs.metricName(s.metric),
    sourceTitle: obs.titleOf(refPathOf(s.source, obs.pins.source)),
    aboutTitle: obs.titleOf(refPathOf(s.about, obs.pins.about)),
    // The stream's own form and code, which the index read, before the
    // metric record's default.
    shape: shapeOf(obs.metricOf(s.metric), s),
  }))
}

/** Whether a row matches every word of the search, in any of its words. */
function matches(row: StreamRow, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (!words.length) return true
  const text = [
    row.metricName,
    row.metric,
    row.sourceTitle,
    row.source,
    row.aboutTitle,
    row.about,
    SHAPE_WORDS[row.shape],
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase()
  return words.every((w) => text.includes(w))
}

function distinct(values: (string | undefined)[]): string[] {
  return [...new Set(values.filter((v): v is string => Boolean(v)))]
}

export function ObservationsPage() {
  const [technical] = useTechnicalDetails()
  const navigate = useNavigate()
  const [metrics, setMetrics] = useQueryState("metric", ARRAY)
  const [sources, setSources] = useQueryState("source", ARRAY)
  const [abouts, setAbouts] = useQueryState("about", ARRAY)
  const [forms, setForms] = useQueryState("form", ARRAY)
  const [search, setSearch] = useQueryState("q", parseAsString.withDefault(""))
  // The chosen metrics, sources and things narrow the reads on the server.
  // "You" names no record, so a filter holding it narrows things here only.
  const narrow = {
    metrics,
    sources,
    abouts: abouts.includes(OWNER) ? [] : abouts,
  }
  const obs = useObservations(narrow)
  // Each facet's values under the other choices, so a chosen source's menu
  // still offers the sources a reader can add.
  const values = useFacetValues(obs, narrow)

  const rows = useMemo(() => rowsOf(obs), [obs])
  const facets = useMemo<Facet[]>(() => {
    // Every metric with a point, so choosing one never hides the others.
    const byMetric = new Map(
      obs.metrics.map((m) => [m, obs.metricName(m)] as const)
    )
    for (const r of rows) byMetric.set(r.metric, r.metricName)
    return [
      {
        name: "metric",
        label: "Metric",
        options: [...byMetric]
          .map(([value, label]) => ({ value, label }))
          .sort((a, b) => a.label.localeCompare(b.label)),
      },
      {
        name: "source",
        label: "Source",
        options: values.sources.map((value) => ({
          value,
          label: values.nameOf(value, obs.pins.source),
        })),
      },
      {
        name: "about",
        label: "About",
        options: values.abouts.length
          ? [
              { value: OWNER, label: "You" },
              ...values.abouts.map((value) => ({
                value,
                label: values.nameOf(value, obs.pins.about),
              })),
            ]
          : [],
      },
      {
        name: "form",
        label: "Form",
        options: distinct(rows.map((r) => r.shape)).map((value) => ({
          value,
          label: SHAPE_WORDS[value as ReadingShape],
        })),
      },
    ]
  }, [rows, obs, values])

  const shown = useMemo(
    () =>
      rows.filter(
        (r) =>
          // An address may name a record by its path or by its id.
          (!metrics.length || metrics.some((m) => sameRef(r.metric, m))) &&
          (!sources.length || sources.some((s) => sameRef(r.source, s))) &&
          (!abouts.length ||
            abouts.some((a) =>
              r.about ? sameRef(r.about, a) : a === OWNER
            )) &&
          (!forms.length || forms.includes(r.shape)) &&
          matches(r, search)
      ),
    [rows, metrics, sources, abouts, forms, search]
  )

  const columns = useMemo(() => streamColumns(obs), [obs])
  const autoHidden = useMemo(
    () => [
      ...(rows.some((r) => r.count !== undefined) ? [] : ["count"]),
      ...(rows.some((r) => r.coverage) ? [] : ["coverage"]),
      ...(rows.some((r) => r.about) ? [] : ["about"]),
    ],
    [rows]
  )
  const table = useDataTable({
    columns,
    data: shown,
    getRowId: (r) => r.key,
    autoHidden,
  })

  const setters: Record<string, (v: string[] | null) => unknown> = {
    metric: setMetrics,
    source: setSources,
    about: setAbouts,
    form: setForms,
  }
  const filtered =
    metrics.length + sources.length + abouts.length + forms.length > 0 ||
    Boolean(search)

  const header = (
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
      description="What your devices, apps and providers measured: one row for each metric from each source, with its newest value. Open one to see it over time."
      actions={
        obs.observed.length > 0 && (
          <Button
            variant="outline"
            size="sm"
            render={<Link to="/observations/timeline" />}
            nativeButton={false}
          >
            <ChartLineIcon />
            Timeline
          </Button>
        )
      }
    />
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
      <TablePage>
        {header}
        <NoObservations />
      </TablePage>
    )
  }

  return (
    <TablePage>
      {header}
      <div className="mt-4 flex flex-wrap items-center gap-1.5 border-b border-border pb-2.5">
        <FacetFilters
          facets={facets}
          selected={{
            metric: metrics,
            source: sources,
            about: abouts,
            form: forms,
          }}
          onChange={(name, values) =>
            void setters[name](values.length ? values : null)
          }
        />
        <span className="flex-1" />
        <SearchBox
          className="h-[30px] w-60 [&_input]:font-sans"
          label="Search streams"
          placeholder="Search streams"
          value={search}
          onChange={(next) => void setSearch(next || null)}
        />
      </div>
      {obs.metricListError && (
        <MetricListError
          className="mt-2.5"
          error={obs.metricListError}
          onRetry={() => obs.discovery.refetch()}
        />
      )}
      {obs.discovery.isError ? (
        <DiscoveryError discovery={obs.discovery} />
      ) : (
        <div
          className={cn(
            "-mx-4 md:-mx-6",
            obs.discovery.isFetching &&
              !obs.discovery.isPending &&
              "opacity-60 transition-opacity"
          )}
        >
          <DataTable
            table={table}
            loading={obs.discovery.isPending}
            onRowClick={(row) =>
              void navigate({
                to: "/observations/timeline",
                search: streamSearch(row) as never,
              })
            }
            empty={
              <p className="px-6 py-8 text-muted-foreground">
                {filtered
                  ? "No stream matches these filters."
                  : "Nothing has been measured yet. Streams show up here once a provider or a sample writes its first value."}
              </p>
            }
          />
        </div>
      )}
      {technical && (
        <p className="mt-3 text-[12.5px] text-faint">
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

function streamColumns(obs: Observations): DataTableColumn<StreamRow>[] {
  return [
    {
      id: "metric",
      header: "Metric",
      accessorFn: (r) => r.metricName,
      cell: ({ row }) => (
        <Link
          to="/observations/timeline"
          search={streamSearch(row.original) as never}
          onClick={(e) => e.stopPropagation()}
          className="block truncate font-medium underline-offset-[3px] hover:underline hover:decoration-border-strong"
        >
          {row.original.metricName}
        </Link>
      ),
      meta: { label: "Metric", size: { min: 160, weight: 2 } },
    },
    {
      id: "source",
      header: "Source",
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
      meta: { label: "Source", size: { min: 160, weight: 2 } },
    },
    {
      id: "about",
      header: "About",
      accessorFn: (r) => r.about,
      cell: ({ row }) => (
        <AboutMark
          value={row.original.about}
          pin={obs.pins.about}
          title={row.original.aboutTitle}
        />
      ),
      meta: { label: "About", size: { min: 120 } },
    },
    {
      id: "form",
      header: "Form",
      accessorFn: (r) => r.shape,
      cell: ({ row }) => <ShapeWord shape={row.original.shape} />,
      meta: { label: "Form", width: 150 },
    },
    {
      id: "latest",
      header: "Latest",
      accessorFn: (r) => r.value,
      cell: ({ row }) =>
        row.original.code ? (
          <CodeMark code={row.original.code} />
        ) : (
          <Reading value={row.original.value} unit={row.original.unit} />
        ),
      meta: { label: "Latest", width: 140 },
    },
    timeColumn<StreamRow>({
      id: "when",
      title: "When",
      iso: (r) => r.at,
      voice: "datetime",
      width: 150,
    }),
    {
      id: "count",
      header: "Points",
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
      header: "Covered",
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
