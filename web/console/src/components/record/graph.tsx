/** The Graph tab: what this record points at, what points back, and a way to
 * walk either direction without leaving the page.
 *
 * The tab used to be **Incoming** and showed the raw `rel` of every inbound
 * row. That reads BACKWARDS: `rel` is the relationship as the OTHER record
 * spells it, so standing on a thread the fan-in said "thread · llmmessage",
 * naming this record instead of what points at it. A group is headed by the
 * declaration's `inverse` now — `messages · llmmessage` — and falls back to
 * `<rel> of <kind>`, which is at least unambiguous, where nobody declared one.
 *
 * TWO DIRECTIONS, and only one of them is a query. Outgoing pointers are on
 * the record already (its edges, and its reference-typed properties), so
 * `thread → agent` costs nothing to show — and could not be answered by the
 * fan-in reader at all, which only ever looks the other way. Incoming groups
 * come from `/incoming`, narrowed per group so expanding one pulls that group
 * alone.
 *
 * A member expands IN PLACE into the same component, so the graph is walkable
 * to any depth. `path` carries the (kind, id) pairs already open above a node:
 * a cycle — two records naming each other, a thread that is its own parent —
 * would otherwise expand forever. */

import { useMemo, useState } from "react"
import { useInfiniteQuery, useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  ArrowDownLeftIcon,
  ArrowUpRightIcon,
  ChevronRightIcon,
  NetworkIcon,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import {
  groupIncoming,
  incomingInfiniteOptions,
  recordQueryOptions,
} from "@/lib/api/records"
import type { IncomingEdge, KindInfo, SubstrateRecord } from "@/lib/api/types"
import { cellValue, recordTitle, relativeTime } from "@/lib/format"
import {
  declaredPointers,
  inverseLabel,
  kindByIdentity,
  splitKind,
  type DeclaredPointer,
} from "@/lib/definition"
import { cn } from "@/lib/utils"

/** How deep a drill-down goes before it asks the reader to open the record
 * itself. Not a correctness bound — the cycle guard is — but a page that can
 * nest without limit stops being readable. */
const MAX_DEPTH = 4
const GROUP_PAGE = 25

/** One end of a pointer, wherever it came from. */
interface NodeRef {
  kind: string
  id: string
  title?: string
}

const keyOf = (ref: NodeRef) => `${ref.kind} ${ref.id}`

/** The route to a record, or undefined when its kind is not installed here —
 * an uninstalled kind renders inert rather than as a dead link. */
function routeOf(kinds: KindInfo[], kind: string) {
  const info = kindByIdentity(kinds, kind)
  if (!info) return undefined
  return { authority: splitKind(kind).authority, plural: info.plural }
}

function RecordLink({
  node,
  kinds,
  className,
}: {
  node: NodeRef
  kinds: KindInfo[]
  className?: string
}) {
  const route = routeOf(kinds, node.kind)
  const label = node.title || node.id
  if (!route) {
    return (
      <span className={cn("truncate data", className)} title={label}>
        {label}
      </span>
    )
  }
  return (
    <Link
      to="/data/$authority/$plural/$id"
      params={{ authority: route.authority, plural: route.plural, id: node.id }}
      className={cn(
        "truncate underline-offset-4 hover:underline",
        !node.title && "data",
        className
      )}
      title={`${node.kind}/${node.id}`}
    >
      {label}
    </Link>
  )
}

/** The disclosure every row shares: a caret, the row's own line, and the
 * expansion beneath it. A row that cannot expand still occupies the caret's
 * width, so the column of names stays a column. */
function Row({
  open,
  onToggle,
  expandable,
  children,
  detail,
}: {
  open: boolean
  onToggle: () => void
  expandable: boolean
  children: React.ReactNode
  detail?: React.ReactNode
}) {
  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-center gap-1.5 py-1">
        {expandable ? (
          <button
            type="button"
            onClick={onToggle}
            className="shrink-0 cursor-pointer text-muted-foreground hover:text-foreground"
            aria-label={open ? "Collapse" : "Expand"}
          >
            <ChevronRightIcon
              className={cn(
                "size-3.5 transition-transform",
                open && "rotate-90"
              )}
            />
          </button>
        ) : (
          <span className="size-3.5 shrink-0" />
        )}
        {children}
      </div>
      {open && detail && (
        <div className="ml-[0.44rem] border-l pl-3">{detail}</div>
      )}
    </div>
  )
}

/** The pointers a record HOLDS, read straight off it — no query, and the only
 * direction the fan-in reader cannot answer. */
function outgoingOf(
  record: SubstrateRecord,
  kind: KindInfo | undefined
): { pointer: DeclaredPointer; targets: NodeRef[] }[] {
  if (!kind) return []
  const out: { pointer: DeclaredPointer; targets: NodeRef[] }[] = []
  for (const pointer of declaredPointers(kind)) {
    const targets: NodeRef[] = []
    if (pointer.via === "edge") {
      for (const target of record.edges?.[pointer.name] ?? []) {
        targets.push({ id: target.id, kind: target.kind, title: target.title })
      }
    } else {
      const value = record.properties[pointer.name]
      for (const one of Array.isArray(value) ? value : [value]) {
        if (typeof one !== "object" || one === null) continue
        const ref = one as Record<string, unknown>
        if (typeof ref.id === "string" && typeof ref.kind === "string") {
          targets.push({ id: ref.id, kind: ref.kind })
        }
      }
    }
    if (targets.length) out.push({ pointer, targets })
  }
  return out
}

function OutgoingGroup({
  pointer,
  targets,
  kinds,
  path,
  depth,
}: {
  pointer: DeclaredPointer
  targets: NodeRef[]
  kinds: KindInfo[]
  path: Set<string>
  depth: number
}) {
  return (
    <div className="min-w-0">
      <div className="flex items-baseline gap-1.5 pt-1.5 text-xs">
        <ArrowUpRightIcon className="size-3 shrink-0 text-muted-foreground" />
        <span className="data">{pointer.name}</span>
        <span className="truncate text-muted-foreground" title={pointer.to}>
          {splitKind(pointer.to).name || pointer.to}
        </span>
        {pointer.description && (
          <span
            className="truncate text-muted-foreground/70"
            title={pointer.description}
          >
            — {pointer.description}
          </span>
        )}
      </div>
      <div className="ml-[0.44rem] border-l pl-3">
        {targets.map((target) => (
          <NodeRow
            key={keyOf(target)}
            node={target}
            kinds={kinds}
            path={path}
            depth={depth}
          />
        ))}
      </div>
    </div>
  )
}

/** One record in the tree: its line, and — expanded — its own graph. */
function NodeRow({
  node,
  kinds,
  path,
  depth,
  meta,
}: {
  node: NodeRef
  kinds: KindInfo[]
  path: Set<string>
  depth: number
  /** A trailing note the row carries: how the pointer reaches it, and when. */
  meta?: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const route = routeOf(kinds, node.kind)
  // A record already open above this one would expand forever, and the deep
  // end of a walk is where the record's own page takes over.
  const cyclic = path.has(keyOf(node))
  const expandable = Boolean(route) && !cyclic && depth < MAX_DEPTH

  return (
    <Row
      open={open}
      onToggle={() => setOpen((v) => !v)}
      expandable={expandable}
      detail={
        open && route ? (
          <GraphNode
            authority={route.authority}
            plural={route.plural}
            id={node.id}
            kind={node.kind}
            kinds={kinds}
            path={new Set([...path, keyOf(node)])}
            depth={depth + 1}
          />
        ) : undefined
      }
    >
      <RecordLink node={node} kinds={kinds} className="text-xs" />
      <span className="shrink-0 text-[0.7rem] text-muted-foreground">
        {splitKind(node.kind).name}
      </span>
      {meta}
      {cyclic && (
        <span className="shrink-0 text-[0.7rem] text-muted-foreground/70">
          already above
        </span>
      )}
    </Row>
  )
}

/** One inbound group: everything of one kind pointing here under one name,
 * paged on its own cursor so opening it costs that group alone. */
function IncomingGroupRow({
  authority,
  plural,
  id,
  rel,
  fromKind,
  seen,
  partial,
  kinds,
  path,
  depth,
}: {
  authority: string
  plural: string
  id: string
  rel: string
  fromKind: string
  seen: number
  partial: boolean
  kinds: KindInfo[]
  path: Set<string>
  depth: number
}) {
  const [open, setOpen] = useState(false)
  const named = useMemo(
    () => inverseLabel(kinds, fromKind, rel),
    [kinds, fromKind, rel]
  )
  const rows = useInfiniteQuery({
    ...incomingInfiniteOptions(authority, plural, id, GROUP_PAGE, {
      rel,
      fromKind,
    }),
    enabled: open,
  })
  const members = (rows.data?.pages ?? []).flatMap((p) => p.incoming ?? [])
  // The group's OWN total, once it is open: the closed count comes from the
  // discovery page, which is capped, so a large group would otherwise announce
  // the cap as its size. Closed and truncated, it says `n+` instead of lying.
  const total = rows.data?.pages[0]?.total

  return (
    <Row
      open={open}
      onToggle={() => setOpen((v) => !v)}
      expandable
      detail={
        <>
          {rows.isPending && (
            <div className="flex flex-col gap-1 py-1">
              <Skeleton className="h-3.5 w-2/3" />
              <Skeleton className="h-3.5 w-1/2" />
            </div>
          )}
          {members.map((row) => (
            <NodeRow
              key={`${row.via}:${row.from.kind}:${row.from.id}`}
              node={{
                id: row.from.id,
                kind: row.from.kind,
                title: row.from.title,
              }}
              kinds={kinds}
              path={path}
              depth={depth}
              meta={<MemberMeta row={row} />}
            />
          ))}
          {rows.hasNextPage && (
            <Button
              variant="ghost"
              size="sm"
              className="h-6 px-1 text-xs font-normal text-muted-foreground"
              onClick={() => void rows.fetchNextPage()}
              disabled={rows.isFetchingNextPage}
            >
              {rows.isFetchingNextPage ? "Loading…" : "Load more"}
            </Button>
          )}
        </>
      }
    >
      <ArrowDownLeftIcon className="size-3 shrink-0 text-muted-foreground" />
      <span className="truncate text-xs">{named.label}</span>
      <span className="shrink-0 data text-[0.7rem] text-muted-foreground">
        {splitKind(fromKind).name}
      </span>
      <span className="shrink-0 text-[0.7rem] text-muted-foreground">
        {total !== undefined
          ? total.toLocaleString()
          : `${seen.toLocaleString()}${partial ? "+" : ""}`}
      </span>
      {named.description && (
        <span
          className="truncate text-[0.7rem] text-muted-foreground/70"
          title={named.description}
        >
          — {named.description}
        </span>
      )}
    </Row>
  )
}

function MemberMeta({ row }: { row: IncomingEdge }) {
  return (
    <span className="ml-auto flex shrink-0 items-center gap-2 text-[0.7rem] text-muted-foreground">
      {/* The mechanism is worth saying: the same relationship can arrive as an
          edge on one row and a reference on the next, mid-migration. */}
      <span className="data">{row.via ?? "edge"}</span>
      {row.createdAt && (
        <span className="data" title={row.createdAt}>
          {relativeTime(row.createdAt)}
        </span>
      )}
    </span>
  )
}

/** One record's whole graph: what it points at, and what points at it. The
 * root renders the record the page already loaded; every nested node fetches
 * its own, lazily, when opened. */
function GraphNode({
  authority,
  plural,
  id,
  kind,
  kinds,
  path,
  depth,
  record: given,
}: {
  authority: string
  plural: string
  id: string
  kind: string
  kinds: KindInfo[]
  path: Set<string>
  depth: number
  record?: SubstrateRecord
}) {
  const fetched = useQuery({
    ...recordQueryOptions(authority, plural, id),
    enabled: !given,
  })
  const record = given ?? fetched.data
  const kindInfo = kindByIdentity(kinds, kind)

  const incoming = useInfiniteQuery(
    incomingInfiniteOptions(authority, plural, id, 200)
  )
  // The server orders by (rel, src_kind, …), so a bucket stays contiguous
  // across pages and grouping is a fold rather than a re-sort — which is what
  // `groupIncoming` already is, tested across a page boundary.
  const groups = useMemo(
    () =>
      groupIncoming(
        (incoming.data?.pages ?? []).flatMap((p) => p.incoming ?? [])
      ),
    [incoming.data]
  )

  const outgoing = useMemo(
    () => (record ? outgoingOf(record, kindInfo) : []),
    [record, kindInfo]
  )

  if (!record && fetched.isPending) {
    return (
      <div className="flex flex-col gap-1 py-1">
        <Skeleton className="h-3.5 w-2/3" />
        <Skeleton className="h-3.5 w-1/2" />
      </div>
    )
  }
  if (!record) {
    // A reference may name a row that is not there — the one thing an edge
    // cannot do — so this is an ordinary state of the graph, not an error.
    return (
      <p className="py-1 text-xs text-muted-foreground">
        This record is not here — a reference may name one that does not exist.
      </p>
    )
  }

  const nothing = outgoing.length === 0 && groups.length === 0
  if (nothing && depth > 0) {
    return (
      <p className="py-1 text-xs text-muted-foreground">
        Nothing points here, and it points nowhere.
      </p>
    )
  }

  return (
    <div className="min-w-0">
      {outgoing.map(({ pointer, targets }) => (
        <OutgoingGroup
          key={pointer.name}
          pointer={pointer}
          targets={targets}
          kinds={kinds}
          path={path}
          depth={depth}
        />
      ))}
      {groups.map((group) => (
        <IncomingGroupRow
          key={`${group.rel} ${group.kind}`}
          authority={authority}
          plural={plural}
          id={id}
          rel={group.rel}
          fromKind={group.kind}
          seen={group.rows.length}
          partial={Boolean(incoming.hasNextPage)}
          kinds={kinds}
          path={path}
          depth={depth}
        />
      ))}
      {incoming.hasNextPage && (
        <Button
          variant="ghost"
          size="sm"
          className="h-6 px-1 text-xs font-normal text-muted-foreground"
          onClick={() => void incoming.fetchNextPage()}
          disabled={incoming.isFetchingNextPage}
        >
          {incoming.isFetchingNextPage ? "Loading…" : "More groups"}
        </Button>
      )}
    </div>
  )
}

export function GraphRail({
  authority,
  plural,
  record,
  kinds,
}: {
  authority: string
  plural: string
  record: SubstrateRecord
  kinds: KindInfo[]
}) {
  const kindInfo = kindByIdentity(kinds, record.kind)
  const outgoing = outgoingOf(record, kindInfo)
  const incoming = useInfiniteQuery(
    incomingInfiniteOptions(authority, plural, record.id, 200)
  )
  const empty =
    outgoing.length === 0 &&
    !incoming.isPending &&
    (incoming.data?.pages[0]?.total ?? 0) === 0

  if (incoming.isError) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <NetworkIcon />
          </EmptyMedia>
          <EmptyTitle>The graph didn't load</EmptyTitle>
          <EmptyDescription>{incoming.error.message}</EmptyDescription>
        </EmptyHeader>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void incoming.refetch()}
        >
          Retry
        </Button>
      </Empty>
    )
  }

  if (empty) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <NetworkIcon />
          </EmptyMedia>
          <EmptyTitle>Nothing is linked</EmptyTitle>
          <EmptyDescription>
            This record points nowhere, and no live record points at it.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="min-w-0 px-4 py-2">
      <p className="pb-1 text-xs text-muted-foreground">
        {recordTitle(record.properties) || record.id} —{" "}
        {cellValue(splitKind(record.kind).name)}
      </p>
      <GraphNode
        authority={authority}
        plural={plural}
        id={record.id}
        kind={record.kind}
        kinds={kinds}
        path={new Set([`${record.kind} ${record.id}`])}
        depth={0}
        record={record}
      />
    </div>
  )
}
