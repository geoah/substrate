import { useMemo, useState } from "react"
import { useInfiniteQuery, useQuery } from "@tanstack/react-query"
import {
  ArrowDownLeftIcon,
  ArrowUpRightIcon,
  ChevronRightIcon,
  type LucideIcon,
} from "lucide-react"

import { Link } from "@tanstack/react-router"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  groupReferencing,
  recordPath,
  recordQueryOptions,
  referencingInfiniteOptions,
  referencingRows,
  type ReferencingRow,
} from "@/lib/api/records"
import {
  readReference,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import { recordTitle } from "@/lib/format"
import {
  declaredReferences,
  kindByIdentity,
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
  return { authority: info.authority, pkg: info.package, name: info.name }
}

function NodeLink({ node, kinds }: { node: NodeRef; kinds: KindInfo[] }) {
  const route = routeOf(kinds, node.kind)
  return route ? (
    <Link
      to="/data/$authority/$pkg/$name/$id"
      params={{ ...route, id: node.id }}
      className="text-sm font-medium break-words text-primary underline-offset-4 hover:underline"
      title={`${node.kind}/${node.id}`}
    >
      {node.title || node.id}
    </Link>
  ) : (
    <span className="text-sm break-words">{node.title || node.id}</span>
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
  /** Already rendered: a plain number, or `N+` while a cursor says more. */
  count?: number | string
  children: React.ReactNode
}) {
  return (
    <section className="min-w-0 rounded-xl border bg-card p-4">
      <div
        className="flex items-center gap-2 text-base font-semibold"
        title={hint}
      >
        <Icon className="size-3.5 text-muted-foreground" />
        <h2>{label}</h2>
        {count !== undefined && (
          <span className="font-normal text-muted-foreground">
            {typeof count === "number" ? count.toLocaleString() : count}
          </span>
        )}
      </div>
      <p className="mt-1 mb-4 text-sm text-muted-foreground">{hint}</p>
      <div className="min-w-0">{children}</div>
    </section>
  )
}

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
    <div className="flex min-w-0 flex-1 flex-col gap-1 py-1">
      {kind && <span className="data break-all whitespace-normal">{kind}</span>}
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
        <span title={description}>
          Reference property: <code>{name}</code>
        </span>
        {count && <span>{count} records</span>}
      </div>
    </div>
  )
}

/** The disclosure every row shares: a caret, the row's own line, and the
 * expansion beneath it. A row that cannot expand still occupies the caret's
 * width, so the column of names stays a column. */
function Row({
  open,
  onToggle,
  expandable,
  label,
  children,
  detail,
}: {
  open: boolean
  onToggle: () => void
  expandable: boolean
  label: string
  children: React.ReactNode
  detail?: React.ReactNode
}) {
  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-center gap-2 py-2">
        {expandable ? (
          <button
            type="button"
            onClick={onToggle}
            className="shrink-0 cursor-pointer text-muted-foreground hover:text-foreground"
            aria-expanded={open}
            aria-label={`${open ? "Collapse" : "Expand"} ${label}`}
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
    ? targets[0].kind
    : undefined
  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-baseline gap-1.5 pt-1.5">
        {/* The caret's width, so group labels align with expandable rows. */}
        <span className="w-3.5 shrink-0" />
        <GroupLabel
          name={pointer.name}
          kind={shared ?? pointer.to}
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
      label={`${node.title || node.id} (${node.kind})`}
      detail={
        open && route ? (
          <GraphNode
            authority={route.authority}
            pkg={route.pkg}
            name={route.name}
            id={node.id}
            kind={node.kind}
            kinds={kinds}
            path={new Set([...path, keyOf(node)])}
            depth={depth + 1}
          />
        ) : undefined
      }
    >
      <div className="min-w-0 flex-1 space-y-1">
        <NodeLink node={node} kinds={kinds} />
        {showKind && (
          <p className="data break-all text-muted-foreground">
            Kind: {node.kind}
          </p>
        )}
        {meta}
      </div>
      {cyclic && (
        <span className="shrink-0 text-[0.7rem] text-muted-foreground/70">
          already above
        </span>
      )}
    </Row>
  )
}

/** Render a page tally: what was seen, `+` while a cursor says there is more. */
function seenCount(seen: number, partial: boolean): string {
  return `${seen.toLocaleString()}${partial ? "+" : ""}`
}

/** One inbound group: everything of one kind pointing here under one name,
 * paged on its own cursor so opening it costs that group alone. */
function ReferencingGroupRow({
  target,
  property,
  fromKind,
  seen,
  partial,
  kinds,
  path,
  depth,
}: {
  /** The target's record path, `<kind>/<id>`. */
  target: string
  property: string
  fromKind: string
  seen: number
  partial: boolean
  kinds: KindInfo[]
  path: Set<string>
  depth: number
}) {
  const [open, setOpen] = useState(false)
  const rows = useInfiniteQuery({
    ...referencingInfiniteOptions(target, GROUP_PAGE, {
      property,
      kind: fromKind,
    }),
    enabled: open,
  })
  // The narrowed read still answers every site of the property; a site under
  // another property cannot arrive, but the fold is by name regardless.
  const members = (rows.data?.pages ?? [])
    .flatMap(referencingRows)
    .filter((row) => row.property === property)
  // The group's OWN count, once it is open: the closed count comes from the
  // discovery page, which is capped, so a large group would otherwise announce
  // the cap as its size. Either way a cursor says `n+` instead of lying.
  const opened = rows.data
    ? seenCount(members.length, Boolean(rows.hasNextPage))
    : undefined

  return (
    <Row
      open={open}
      onToggle={() => setOpen((v) => !v)}
      expandable
      label={`${fromKind} via ${property}`}
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
              key={`${row.record.kind}:${row.record.id}:${row.path ?? ""}`}
              node={{
                id: row.record.id,
                kind: row.record.kind,
                title: recordTitle(row.record.properties) || undefined,
              }}
              kinds={kinds}
              path={path}
              depth={depth}
              meta={<MemberMeta row={row} />}
            />
          ))}
          {rows.isError && (
            <p role="alert" className="py-2 text-sm text-destructive">
              References could not be loaded.{" "}
              <button className="underline" onClick={() => void rows.refetch()}>
                Retry
              </button>
            </p>
          )}
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
        name={property}
        kind={fromKind}
        count={opened ?? seenCount(seen, partial)}
      />
    </Row>
  )
}

/** A NESTED reference site says where inside the property it sits; a kind's
 * own property has nothing more to say, so the row carries no trailing note. */
function MemberMeta({ row }: { row: ReferencingRow }) {
  if (!row.path) return null
  return (
    <span className="ml-auto flex shrink-0 items-center gap-2 text-[0.7rem] text-muted-foreground">
      <span className="data" title={`nested at ${row.path}`}>
        {row.path}
      </span>
    </span>
  )
}

/** One record's whole graph: what it points at, and what points at it. The
 * root renders the record the page already loaded; every nested node fetches
 * its own, lazily, when opened. */
function GraphNode({
  authority,
  pkg,
  name,
  id,
  kind,
  kinds,
  path,
  depth,
  record: given,
}: {
  authority: string
  pkg: string
  name: string
  id: string
  kind: string
  kinds: KindInfo[]
  path: Set<string>
  depth: number
  record?: SubstrateRecord
}) {
  const fetched = useQuery({
    ...recordQueryOptions(authority, pkg, name, id),
    enabled: !given,
  })
  const record = given ?? fetched.data
  const kindInfo = kindByIdentity(kinds, kind)

  const target = recordPath(kind, id)
  const referencing = useInfiniteQuery(referencingInfiniteOptions(target, 200))
  // A page is in the list's order, not grouped, so `groupReferencing` folds
  // by key — which is what makes a group whole across a page boundary.
  const groups = useMemo(
    () =>
      groupReferencing(
        (referencing.data?.pages ?? []).flatMap(referencingRows)
      ),
    [referencing.data]
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
  if (!record && fetched.isError) {
    return (
      <p role="alert" className="py-2 text-sm text-destructive">
        This record could not be loaded.{" "}
        <button className="underline" onClick={() => void fetched.refetch()}>
          Retry
        </button>
      </p>
    )
  }
  if (!record) {
    // A reference may name a row that is not there (only `mustExist` bars it
    // at write, and a purge can still take the target), so this is an ordinary
    // state of the graph, not an error.
    return (
      <p className="py-1 text-xs text-muted-foreground">
        This record is not here. A reference can name one that does not exist.
      </p>
    )
  }

  const nothing = outgoing.length === 0 && groups.length === 0
  if (nothing && depth > 0 && referencing.isSuccess) {
    return (
      <p className="py-1 text-xs text-muted-foreground">
        Nothing points here, and it points nowhere.
      </p>
    )
  }

  // Every pointer at this record is an incoming reference here, a mirror's
  // mapping-owned subject slot included: this is the GENERAL reverse read.
  // Which of them are sources, grouped by the mapping that made them, is the
  // Provenance tab's Sources section (sources.tsx), not a second grouping.
  function groupRows(selected: typeof groups) {
    return selected.map((group) => (
      <ReferencingGroupRow
        key={`${group.property} ${group.kind}`}
        target={target}
        property={group.property}
        fromKind={group.kind}
        seen={group.rows.length}
        partial={Boolean(referencing.hasNextPage)}
        kinds={kinds}
        path={path}
        depth={depth}
      />
    ))
  }

  return (
    <div className="grid min-w-0 gap-5">
      <Section
        icon={ArrowDownLeftIcon}
        label="Incoming references"
        hint="Records that point to this record."
      >
        {groupRows(groups)}
        {!groups.length && !referencing.isError && (
          <p className="text-sm text-muted-foreground">
            {referencing.isPending
              ? "Loading references…"
              : "No incoming references."}
          </p>
        )}
      </Section>
      <Section
        icon={ArrowUpRightIcon}
        label="Outgoing references"
        hint="Records referenced by this record's properties."
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
        {!outgoing.length && (
          <p className="text-sm text-muted-foreground">
            No outgoing references.
          </p>
        )}
      </Section>
      {referencing.isError && (
        <p role="alert" className="text-sm text-destructive">
          References could not be loaded.{" "}
          <button
            className="underline"
            onClick={() => void referencing.refetch()}
          >
            Retry
          </button>
        </p>
      )}
      {referencing.hasNextPage && (
        <Button
          variant="outline"
          onClick={() => void referencing.fetchNextPage()}
          disabled={referencing.isFetchingNextPage}
        >
          {referencing.isFetchingNextPage ? "Loading…" : "Load more references"}
        </Button>
      )}
    </div>
  )
}

export function GraphRail({
  authority,
  pkg,
  name,
  record,
  kinds,
}: {
  authority: string
  pkg: string
  name: string
  record: SubstrateRecord
  kinds: KindInfo[]
}) {
  return (
    <div className="max-w-5xl min-w-0 p-6">
      <GraphNode
        authority={authority}
        pkg={pkg}
        name={name}
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
