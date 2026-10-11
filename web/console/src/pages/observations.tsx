/** Observations (`/observations`): one row per stream, a metric written by
 * one source about one thing, with its newest value, from the latest read
 * (`aggregate` grouped by metric, source and about, the one reducer `last`,
 * no bucket and no range). Filters for metric, source, about and form and a
 * search narrow the rows in the page; a row opens the timeline narrowed to
 * that stream. Shown only where a kind binds core's `observed` trait. */

import { useMemo } from "react"
import { Link, useNavigate } from "@tanstack/react-router"
import { ChartLineIcon, SearchXIcon } from "lucide-react"
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
  GroupRecord,
  Reading,
  ShapeWord,
} from "@/components/observations/marks"
import { NoObservations } from "@/components/observations/no-observations"
import { SearchBox } from "@/components/search-box"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useObservations, type Observations } from "@/hooks/use-observations"
import {
  SHAPE_WORDS,
  coverageWords,
  refPathOf,
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
    shape: shapeOf(obs.metricOf(s.metric)),
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
  const obs = useObservations()
  const [technical] = useTechnicalDetails()
  const navigate = useNavigate()
  const [metrics, setMetrics] = useQueryState("metric", ARRAY)
  const [sources, setSources] = useQueryState("source", ARRAY)
  const [abouts, setAbouts] = useQueryState("about", ARRAY)
  const [forms, setForms] = useQueryState("form", ARRAY)
  const [search, setSearch] = useQueryState("q", parseAsString.withDefault(""))

  const rows = useMemo(() => rowsOf(obs), [obs])
  const facets = useMemo<Facet[]>(() => {
    const byMetric = new Map(rows.map((r) => [r.metric, r.metricName]))
    const bySource = new Map(rows.map((r) => [r.source, r.sourceTitle]))
    const byAbout = new Map(rows.map((r) => [r.about, r.aboutTitle]))
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
        options: distinct(rows.map((r) => r.source)).map((value) => ({
          value,
          label: bySource.get(value) ?? value,
        })),
      },
      {
        name: "about",
        label: "About",
        options: rows.some((r) => r.about)
          ? [
              { value: OWNER, label: "You" },
              ...distinct(rows.map((r) => r.about)).map((value) => ({
                value,
                label: byAbout.get(value) ?? value,
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
  }, [rows])

  const shown = useMemo(
    () =>
      rows.filter(
        (r) =>
          (!metrics.length || metrics.includes(r.metric)) &&
          (!sources.length || (r.source && sources.includes(r.source))) &&
          (!abouts.length || abouts.includes(r.about ?? OWNER)) &&
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
      {obs.latest.isError ? (
        <Empty className="mt-6">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>The streams didn’t load</EmptyTitle>
            <EmptyDescription>{obs.latest.error.message}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void obs.latest.refetch()}
            >
              Try again
            </Button>
          </EmptyContent>
        </Empty>
      ) : (
        <div
          className={cn(
            "-mx-4 md:-mx-8",
            obs.latest.isFetching &&
              !obs.latest.isPending &&
              "opacity-60 transition-opacity"
          )}
        >
          <DataTable
            table={table}
            loading={obs.latest.isPending}
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
      cell: ({ row }) => (
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
      header: "Count",
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
        label: "Count",
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
