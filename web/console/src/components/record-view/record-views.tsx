/** The record views of a record page (decision record 0166): the
 * `substrate.reamde.dev/console/recordview` records that name the
 * record's kind, each drawn as a section of blocks above the generic
 * sheet, in id order. A page whose kind has no view draws none of this.
 * The blocks read what the view names and nothing else; a section the
 * console cannot draw says why where it would be. */

import { useCallback, useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ChartLineIcon } from "lucide-react"

import { BreakdownBlockView } from "./breakdown-block"
import { ChartBlockView, SpansBlockView } from "./observed-blocks"
import {
  Failed,
  Loading,
  Notice,
  Part,
  Quiet,
  type BlockContext,
} from "./parts"
import { spanWords } from "./span-words"
import { StatsBlockView } from "./stats-block"
import { TableBlockView } from "./table-block"
import { TrendBlockView } from "./trend-block"
import { RecordRef } from "@/components/identity/record-ref"
import { SectionHead } from "@/components/identity/section-head"
import { LazyMarkdownEditor } from "@/components/markdown/lazy-markdown-editor"
import { DeclaredValue } from "@/components/property-sheet/property-value"
import { sheetRows } from "@/components/property-sheet/sheet-rows"
import { Button } from "@/components/ui/button"
import { recordsQueryOptions } from "@/lib/api/records"
import {
  readReference,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { displayName, lowerFirst } from "@/lib/kind-names"
import { readerTimezone, type TimeRange } from "@/lib/observations-time"
import { splitRecordPath } from "@/lib/record-path"
import {
  boundOf,
  readsWindow,
  viewNeedsWindow,
  viewTimelineSearch,
  type Block,
  type HeadingBlock,
  type PropertiesBlock,
  type RecordView,
  type RelatedBlock,
  type TextBlock,
} from "@/lib/record-view"

export function RecordViews({
  views,
  record,
  kind,
  kinds,
}: {
  views: readonly RecordView[]
  record: SubstrateRecord
  kind?: KindInfo
  kinds: readonly KindInfo[]
}) {
  return views.map((view) => (
    <RecordViewSection
      // The record page stays mounted across records of one route, and a
      // zoom into one record's window means nothing on another's.
      key={`${record.kind}/${record.id}/${view.id}`}
      view={view}
      record={record}
      kind={kind}
      kinds={kinds}
    />
  ))
}

function RecordViewSection({
  view,
  record,
  kind,
  kinds,
}: {
  view: RecordView
  record: SubstrateRecord
  kind?: KindInfo
  kinds: readonly KindInfo[]
}) {
  const noun = lowerFirst(displayName(kind ?? record.kind))
  // An open window ends at the time of reading; read once per mount, so a
  // render does not move it and every block reads the same one.
  const [now] = useState(() => Date.now())
  const bound = useMemo(
    () => boundOf(view, record, kinds, now),
    [view, record, kinds, now]
  )
  const timezone = useMemo(() => readerTimezone(), [])
  // A zoom reads part of the window, and every line and spans block
  // follows it on one time axis; the totals stay whole.
  const [zoom, setZoom] = useState<TimeRange>()
  const ctx: BlockContext = {
    record,
    kind,
    kinds,
    noun,
    bound,
    window: zoom ?? bound?.window,
    zoomed: Boolean(zoom),
    onZoom: setZoom,
    onReset: () => setZoom(undefined),
    timezone,
  }
  const search = bound ? viewTimelineSearch(bound, view, record) : undefined
  const title = view.name ?? `${displayName(kind ?? record.kind)} view`
  // A view binding a window the record does not hold (no end yet, say)
  // leaves out the blocks that read it, and is left out itself where
  // nothing else remains, as a page with no view would be. A window the
  // view cannot bind as written is said once, where the first of those
  // blocks would be.
  const windowless = viewNeedsWindow(view) && !bound?.window
  const broken = view.windowProblem
  const drawn = view.sections.flatMap((s, i) =>
    "block" in s && readsWindow(s.block) && (broken || windowless)
      ? []
      : [{ section: s, i }]
  )
  const firstWindowed = view.sections.findIndex(
    (s) => "block" in s && readsWindow(s.block)
  )
  const sections =
    broken && firstWindowed >= 0
      ? [
          ...drawn.filter((d) => d.i < firstWindowed),
          { section: { problem: broken }, i: firstWindowed },
          ...drawn.filter((d) => d.i > firstWindowed),
        ]
      : drawn
  // A view whose every block hides when empty (a table that holds rows
  // on a few records of its kind, a vendor's stats and trends on the
  // records another source wrote) stays hidden until one of them draws
  // something, so the other records' pages are unchanged.
  const hiders = sections.flatMap(({ section, i }) =>
    "block" in section &&
    (section.block.type === "table" ||
      section.block.type === "stats" ||
      section.block.type === "trend") &&
    section.block.empty === "hide"
      ? [i]
      : []
  )
  const allHide = sections.length > 0 && hiders.length === sections.length
  const [filled, setFilled] = useState<ReadonlySet<number>>(() => new Set())
  const onEmpty = useCallback((i: number, hidden: boolean) => {
    setFilled((held) => {
      if (held.has(i) === !hidden) return held
      const next = new Set(held)
      if (hidden) next.delete(i)
      else next.add(i)
      return next
    })
  }, [])
  if (!sections.length) return null
  return (
    <section
      aria-label={title}
      data-record-view={view.id}
      hidden={allHide && filled.size === 0}
    >
      <SectionHead
        title={title}
        hint={
          (bound?.lastWindow
            ? spanWords(bound, noun).whole
            : spanWords(bound, noun).soFar) || undefined
        }
        actions={
          search && (
            <Button
              variant="ghost"
              size="sm"
              render={
                <Link to="/observations/timeline" search={search as never} />
              }
              nativeButton={false}
            >
              <ChartLineIcon />
              Open in the timeline
            </Button>
          )
        }
      />
      {view.intro && (
        <div className="text-[13px] text-muted-foreground">
          <LazyMarkdownEditor
            value={view.intro}
            label={`${title}: introduction`}
            editable={false}
          />
        </div>
      )}
      {sections.map(({ section, i }) =>
        "problem" in section ? (
          <Notice key={i}>{section.problem}</Notice>
        ) : (
          <BlockView
            key={i}
            block={section.block}
            ctx={ctx}
            onEmpty={(hidden) => onEmpty(i, hidden)}
          />
        )
      )}
    </section>
  )
}

function BlockView({
  block,
  ctx,
  onEmpty,
}: {
  block: Block
  ctx: BlockContext
  /** Said by a table, stats or trend block that hides when empty: whether
   * it drew nothing. */
  onEmpty: (hidden: boolean) => void
}) {
  switch (block.type) {
    case "heading":
      return <Heading block={block} />
    case "text":
      return <Text block={block} />
    case "properties":
      return <Properties block={block} ctx={ctx} />
    case "related":
      return <Related block={block} ctx={ctx} />
    case "chart":
      return <ChartBlockView block={block} ctx={ctx} />
    case "spans":
      return <SpansBlockView block={block} ctx={ctx} />
    case "stats":
      return <StatsBlockView block={block} ctx={ctx} onEmpty={onEmpty} />
    case "trend":
      return <TrendBlockView block={block} ctx={ctx} onEmpty={onEmpty} />
    case "table":
      return <TableBlockView block={block} ctx={ctx} onEmpty={onEmpty} />
    case "breakdown":
      return <BreakdownBlockView block={block} ctx={ctx} />
    case "lanes":
      return <Notice>This console does not draw lanes yet.</Notice>
  }
}

function Heading({ block }: { block: HeadingBlock }) {
  return (
    <div className="mt-5 flex flex-col gap-0.5">
      <h3 className="text-[14px] font-semibold">{block.text}</h3>
      {block.note && <Quiet>{block.note}</Quiet>}
    </div>
  )
}

function Text({ block }: { block: TextBlock }) {
  return (
    <div className="mt-3 text-[13.5px]">
      <LazyMarkdownEditor value={block.text} label="Text" editable={false} />
    </div>
  )
}

/** Chosen properties of the record, in the view's order, each under the
 * view's label or its own; a property the record does not hold is left
 * out. */
function Properties({
  block,
  ctx,
}: {
  block: PropertiesBlock
  ctx: BlockContext
}) {
  const rows = useMemo(
    () => sheetRows(ctx.record, ctx.kind, true).all,
    [ctx.record, ctx.kind]
  )
  const shown = block.list.flatMap((item) => {
    const row = rows.find((r) => r.name === item.property)
    return row?.filled ? [{ item, row }] : []
  })
  return (
    <Part title={block.label}>
      {shown.length ? (
        <dl
          aria-label={block.label ?? "Properties"}
          className="grid max-w-[520px] grid-cols-[minmax(96px,170px)_minmax(0,1fr)] gap-x-3 gap-y-1 text-[13.5px]"
        >
          {shown.map(({ item, row }) => (
            <div key={item.property} className="contents">
              <dt className="text-muted-foreground">
                {item.label ?? row.spec.label}
              </dt>
              <dd className="min-w-0">
                <DeclaredValue spec={row.spec} value={row.value} />
              </dd>
            </div>
          ))}
        </dl>
      ) : (
        <Quiet>None of these is set on this {ctx.noun}.</Quiet>
      )}
    </Part>
  )
}

/** Records linked to this one: those an outgoing reference property names,
 * or those of a kind whose reference property points here. */
function Related({ block, ctx }: { block: RelatedBlock; ctx: BlockContext }) {
  if (block.property) {
    const raw = ctx.record.properties?.[block.property]
    const paths = (Array.isArray(raw) ? raw : raw === undefined ? [] : [raw])
      .map((v) => readReference(v)?.path)
      .filter((p): p is string => Boolean(p))
    return (
      <Part title={block.label}>
        <RelatedList paths={paths} show={block.show} limit={block.limit} />
      </Part>
    )
  }
  return <Referencing block={block} ctx={ctx} />
}

function Referencing({
  block,
  ctx,
}: {
  block: RelatedBlock
  ctx: BlockContext
}) {
  const known = Boolean(
    block.kind && kindByIdentity([...ctx.kinds], block.kind)
  )
  const page = useQuery({
    ...recordsQueryOptions({
      kinds: [block.kind ?? ""],
      first: block.limit,
      count: block.show === "count",
      filter: {
        properties: {
          [block.via ?? ""]: { eq: `${ctx.record.kind}/${ctx.record.id}` },
        },
      },
    }),
    enabled: known,
  })
  if (!known) {
    return (
      <Notice>
        This related block names a kind this repository does not declare:{" "}
        {block.kind}.
      </Notice>
    )
  }
  return (
    <Part title={block.label}>
      {page.isPending ? (
        <Loading />
      ) : page.error ? (
        <Failed error={page.error} />
      ) : (
        <RelatedList
          paths={page.data.records.map((r) => `${r.kind}/${r.id}`)}
          count={page.data.count}
          show={block.show}
          limit={block.limit}
        />
      )}
    </Part>
  )
}

function RelatedList({
  paths,
  count,
  show,
  limit,
}: {
  paths: readonly string[]
  /** The whole set's size, where it was read; else the paths'. */
  count?: number
  show: "list" | "count"
  limit: number
}) {
  const total = count ?? paths.length
  if (show === "count") {
    return (
      <p className="text-[13.5px] tabular-nums" data-slot="related-count">
        {total}
      </p>
    )
  }
  if (!paths.length) return <Quiet>None.</Quiet>
  return (
    <ul className="flex flex-col gap-1 text-[13.5px]">
      {paths.slice(0, limit).map((path) => {
        const parts = splitRecordPath(path)
        return (
          <li key={path}>
            {parts ? (
              <RecordRef kind={parts.kind} id={parts.id} />
            ) : (
              <span className="font-mono">{path}</span>
            )}
          </li>
        )
      })}
      {total > limit && <Quiet>and {total - limit} more</Quiet>}
    </ul>
  )
}
