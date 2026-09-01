/** The Graph tab: what this record points at, what points back, and a way to
 * walk either direction without leaving the page.
 *
 * The LAYOUT is a hierarchy, not a wall of rows: the record on top, then one
 * **Outgoing** and one **Incoming** section — direction is said once, at the
 * section header, so the rows under it carry no per-row arrows. Inside a
 * section, each group is a small uppercase label (the reference property, or
 * the inverse for fan-in) with the group's kind and count beside it, and every
 * target renders as the RecordPill every other surface uses. A group that
 * shares one kind never repeats it on its rows.
 *
 * The fan-in used to show the raw property name of every inbound row. That
 * reads BACKWARDS: the name is the pointer as the OTHER record spells it, so
 * standing on a thread the fan-in said "thread · llmmessage", naming this
 * record instead of what points at it. A group is headed by the declaration's
 * `inverse` — `messages · llmmessage` — and falls back to
 * `<property> of <kind>`, which is at least unambiguous, where nobody declared
 * one.
 *
 * TWO DIRECTIONS, and only one of them is a query. Outgoing pointers are the
 * record's own reference-typed properties, so `thread → agent` costs nothing
 * to show — and could not be answered by the fan-in reader at all, which only
 * ever looks the other way. Incoming groups come from `/incoming`, narrowed
 * per group so expanding one pulls that group alone.
 *
 * A member expands IN PLACE into the same component, so the graph is walkable
 * to any depth. `path` carries the (kind, id) pairs already open above a node:
 * a cycle — two records naming each other, a thread that is its own parent —
 * would otherwise expand forever. */

import { useMemo, useState } from "react"
import { useInfiniteQuery, useQuery } from "@tanstack/react-query"
import {
  ArrowDownLeftIcon,
  ArrowUpRightIcon,
  ChevronRightIcon,
  NetworkIcon,
  type LucideIcon,
} from "lucide-react"

import { RecordPill } from "@/components/record-pill"
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
import {
  readReference,
  type IncomingReference,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import { recordTitle, relativeTime } from "@/lib/format"
import {
  declaredReferences,
  inverseLabel,
  kindByIdentity,
  splitKind,
  type DeclaredProperty,
} from "@/lib/definition"
import { splitRecordPath } from "@/lib/record-path"
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
  return { authority: splitKind(kind).authority, name: info.name }
}

/** The node as a RecordPill — the one way a record is referenced anywhere.
 * An uninstalled kind hands the pill an unroutable reference on purpose, so
 * it renders its inert form instead of minting a dead link. */
function NodePill({ node, kinds }: { node: NodeRef; kinds: KindInfo[] }) {
  const routable = Boolean(routeOf(kinds, node.kind))
  return (
    <RecordPill
      kind={routable ? node.kind : ""}
      id={node.id}
      title={node.title}
      className="min-w-0"
    />
  )
}

/** One direction of the graph, said ONCE: the header carries the arrow and
 * the count, so no row under it needs its own. */
function Section({
  icon: Icon,
  label,
  hint,
  count,
  children,
}: {
  icon: LucideIcon
  label: string
  /** The direction spelled out, for the reader the arrow does not reach. */
  hint: string
  count?: number
  children: React.ReactNode
}) {
  return (
    <div className="min-w-0 pt-2 first:pt-0">
      <div
        className="flex cursor-default items-center gap-1.5 text-xs font-semibold"
        title={hint}
      >
        <Icon className="size-3.5 text-muted-foreground" />
        {label}
        {count !== undefined && (
          <span className="font-normal text-muted-foreground">
            {count.toLocaleString()}
          </span>
        )}
      </div>
      <div className="min-w-0">{children}</div>
    </div>
  )
}

/** A group's heading: the pointer's name as a small uppercase label, the kind
 * the group shares, and how many rows it holds. The declaration's one-liner
 * rides the label as a native title, not another line of text. */
function GroupLabel({
  name,
  kind,
  count,
  description,
}: {
  name: string
  kind?: string
  count?: string
  description?: string
}) {
  return (
    <>
      <span
        className={cn(
          "truncate text-[0.65rem] font-medium tracking-wider uppercase",
          "text-muted-foreground",
          description && "cursor-help"
        )}
        title={description}
      >
        {name}
      </span>
      {kind && (
        <span className="shrink-0 data text-[0.7rem] text-muted-foreground/70">
          {kind}
        </span>
      )}
      {count && (
        <span className="shrink-0 text-[0.7rem] text-muted-foreground/70">
          {count}
        </span>
      )}
    </>
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

/** The pointers a record HOLDS: its declared reference properties, read
 * straight off it — no query, and the only direction the fan-in reader cannot
 * answer. */
function outgoingOf(
  record: SubstrateRecord,
  kind: KindInfo | undefined
): { pointer: DeclaredProperty; targets: NodeRef[] }[] {
  if (!kind) return []
  const out: { pointer: DeclaredProperty; targets: NodeRef[] }[] = []
  for (const pointer of declaredReferences(kind)) {
    const targets: NodeRef[] = []
    const value = record.properties[pointer.name]
    for (const one of Array.isArray(value) ? value : [value]) {
      // Either value shape: the flat path, or the object a reference with link
      // data stores.
      const held = readReference(one)
      if (!held) continue
      // A stored reference is the referent's whole record path, and the kind
      // grammar is what splits it — the registry is not consulted, so a
      // pointer at a kind nobody installed still draws its row.
      const target = splitRecordPath(held.path)
      if (target) targets.push(target)
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
  pointer: DeclaredProperty
  targets: NodeRef[]
  kinds: KindInfo[]
  path: Set<string>
  depth: number
}) {
  // One kind across the group means the kind is the GROUP's fact; a mixed
  // group (a reference that never declared its target) says it per row.
  const shared = targets.every((t) => t.kind === targets[0].kind)
    ? splitKind(targets[0].kind).name
    : undefined
  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-baseline gap-1.5 pt-1.5">
        {/* The caret's width, so group labels align with expandable rows. */}
        <span className="w-3.5 shrink-0" />
        <GroupLabel
          name={pointer.name}
          kind={shared ?? (splitKind(pointer.to ?? "").name || pointer.to)}
          count={
            targets.length > 1 ? targets.length.toLocaleString() : undefined
          }
          description={pointer.description}
        />
      </div>
      <div className="ml-[0.44rem] border-l pl-3">
        {targets.map((target) => (
          <NodeRow
            key={keyOf(target)}
            node={target}
            kinds={kinds}
            path={path}
            depth={depth}
            showKind={!shared}
          />
        ))}
      </div>
    </div>
  )
}

/** One record in the tree: its pill, and — expanded — its own graph. */
function NodeRow({
  node,
  kinds,
  path,
  depth,
  meta,
  showKind,
}: {
  node: NodeRef
  kinds: KindInfo[]
  path: Set<string>
  depth: number
  /** A trailing note the row carries: how the pointer reaches it, and when. */
  meta?: React.ReactNode
  /** Only a mixed group says the kind per row; a shared one said it above. */
  showKind?: boolean
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
            plural={route.name}
            id={node.id}
            kind={node.kind}
            kinds={kinds}
            path={new Set([...path, keyOf(node)])}
            depth={depth + 1}
          />
        ) : undefined
      }
    >
      <NodePill node={node} kinds={kinds} />
      {showKind && (
        <span className="shrink-0 data text-[0.7rem] text-muted-foreground">
          {splitKind(node.kind).name}
        </span>
      )}
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
  property,
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
  property: string
  fromKind: string
  seen: number
  partial: boolean
  kinds: KindInfo[]
  path: Set<string>
  depth: number
}) {
  const [open, setOpen] = useState(false)
  const named = useMemo(
    () => inverseLabel(kinds, fromKind, property),
    [kinds, fromKind, property]
  )
  const rows = useInfiniteQuery({
    ...incomingInfiniteOptions(authority, plural, id, GROUP_PAGE, {
      property,
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
              key={`${row.from.kind}:${row.from.id}:${row.path ?? ""}`}
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
      <GroupLabel
        name={named.label}
        kind={splitKind(fromKind).name}
        count={
          total !== undefined
            ? total.toLocaleString()
            : `${seen.toLocaleString()}${partial ? "+" : ""}`
        }
        description={named.description}
      />
    </Row>
  )
}

function MemberMeta({ row }: { row: IncomingReference }) {
  return (
    <span className="ml-auto flex shrink-0 items-center gap-2 text-[0.7rem] text-muted-foreground">
      {/* A NESTED reference site says where inside the property it sits;
          a kind's own property has nothing more to say. */}
      {row.path && (
        <span className="data" title={`nested at ${row.path}`}>
          {row.path}
        </span>
      )}
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
  // The refs index walks (src_kind, src, property, …), so a bucket is not
  // contiguous and `groupIncoming` folds by key — which is what makes a group
  // whole across a page boundary.
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
    // A reference may name a row that is not there (only `mustExist` bars it
    // at write, and a purge can still take the target), so this is an ordinary
    // state of the graph, not an error.
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
      {outgoing.length > 0 && (
        <Section
          icon={ArrowUpRightIcon}
          label="Outgoing"
          hint="What this record points at"
          count={outgoing.reduce((n, g) => n + g.targets.length, 0)}
        >
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
        </Section>
      )}
      {groups.length > 0 && (
        <Section
          icon={ArrowDownLeftIcon}
          label="Incoming"
          hint="What points at this record"
          count={incoming.data?.pages[0]?.total}
        >
          {groups.map((group) => (
            <IncomingGroupRow
              key={`${group.property} ${group.kind}`}
              authority={authority}
              plural={plural}
              id={id}
              property={group.property}
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
        </Section>
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
    <div className="min-w-0 px-4 py-3">
      {/* The record the graph hangs off, said the way the tree cannot: big.
          Everything under it points away from or back at this line. */}
      <div className="flex min-w-0 items-baseline gap-2 pb-2">
        <span className="truncate text-sm font-semibold">
          {recordTitle(record.properties) || record.id}
        </span>
        <span className="shrink-0 data text-xs text-muted-foreground">
          {splitKind(record.kind).name}
        </span>
      </div>
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
