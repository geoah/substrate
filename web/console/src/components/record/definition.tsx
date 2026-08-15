/** A kind's DECLARATION, read (owner ask, 2026-08-12: "I go to the
 * llmproviders kind, I can see the table of records but I don't have a tab to
 * see its definition — I want to see the kind YAML").
 *
 * Three readings of one thing, top to bottom: the declaration YAML through the
 * SAME renderer the record manifest uses (shiki, lazy, css-variables), then
 * the declared properties and edges on THE table system — name, type,
 * description, required, and a state machine's states where there is one. No
 * fetch of its own: the kind and the registry both ride the kinds query the
 * browse page already holds. */

import { useMemo } from "react"
import type { DataTableColumn } from "@/components/data-table/data-table"
import { Link } from "@tanstack/react-router"
import { FileQuestionIcon } from "lucide-react"

import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { YamlView } from "@/components/record/yaml-view"
import { Badge } from "@/components/ui/badge"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import type { KindInfo } from "@/lib/api/types"
import { kindLinkTargets, kindManifestYAML } from "@/lib/manifest"
import {
  declaredEdges,
  declaredProperties,
  edgeTypeLabel,
  propertyTypeLabel,
  resolveEdgeTarget,
  type DeclaredEdge,
  type DeclaredProperty,
} from "@/lib/definition"
import { keyDocsOf } from "@/lib/yaml-annotations"

function Muted({ children }: { children: React.ReactNode }) {
  return <span className="text-muted-foreground">{children}</span>
}

/** A section seam, in the rails' voice: a quiet caption over its block. */
function Section({
  title,
  count,
  children,
}: {
  title: string
  count?: number
  children: React.ReactNode
}) {
  return (
    <section className="flex flex-col">
      <h2 className="px-6 pt-4 pb-1 text-xs font-medium text-muted-foreground">
        {title}
        {count !== undefined && <span className="ml-1.5 data">{count}</span>}
      </h2>
      {children}
    </section>
  )
}

/** The datatype cell: the declared spelling, and — where the declaration says
 * more than a name — the machine's states or the enum's admitted values on
 * hover, rather than a second line crowding the row. */
function TypeCell({ prop }: { prop: DeclaredProperty }) {
  const label = propertyTypeLabel(prop)
  const detail = prop.states?.length
    ? prop.states
        .map((s) => (s === prop.initial ? `${s} (initial)` : s))
        .join(" · ")
    : prop.values?.length
      ? prop.values.map((v) => v.label || v.value).join(" · ")
      : undefined
  if (!detail) {
    return (
      <span className="block truncate data text-muted-foreground" title={label}>
        {label}
      </span>
    )
  }
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className="block cursor-help truncate data text-muted-foreground underline decoration-dotted decoration-from-font underline-offset-4" />
        }
      >
        {label}
      </TooltipTrigger>
      <TooltipContent side="right">{detail}</TooltipContent>
    </Tooltip>
  )
}

function propertyColumns(): DataTableColumn<DeclaredProperty>[] {
  return [
    {
      id: "property",
      accessorFn: (p) => p.name,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="property" />
      ),
      cell: ({ row }) => (
        <span className="block truncate data" title={row.original.name}>
          {row.original.name}
        </span>
      ),
      meta: { label: "property", size: { min: 140, max: 260, weight: 1 } },
    },
    {
      id: "type",
      accessorFn: (p) => propertyTypeLabel(p),
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="type" />
      ),
      cell: ({ row }) => <TypeCell prop={row.original} />,
      meta: { label: "type", size: { min: 120, max: 220, weight: 0.75 } },
    },
    {
      id: "required",
      accessorFn: (p) => p.required === true,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="required" />
      ),
      cell: ({ row }) =>
        row.original.required ? (
          <Badge variant="secondary" className="px-1.5 font-normal">
            required
          </Badge>
        ) : (
          <Muted>—</Muted>
        ),
      meta: { label: "required", width: 96 },
    },
    {
      id: "description",
      accessorFn: (p) => p.description,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="description" />
      ),
      cell: ({ row }) => {
        const text = row.original.description
        if (!text) return <Muted>—</Muted>
        return (
          <span className="block truncate" title={text}>
            {text}
          </span>
        )
      },
      // the absorber: leftover width lands on the prose, uncapped.
      meta: { label: "description", size: { min: 220, weight: 2 } },
    },
  ]
}

/** The edge's declared target, linked to that collection when the registry can
 * resolve it (a bare singular resolves inside the declaring authority first). */
function TargetCell({
  edge,
  kind,
  kinds,
}: {
  edge: DeclaredEdge
  kind: KindInfo
  kinds: KindInfo[]
}) {
  const target = resolveEdgeTarget(kinds, kind, edge.to)
  const label = edgeTypeLabel(edge)
  if (!target) {
    return (
      <span className="block truncate data text-muted-foreground" title={label}>
        {label}
      </span>
    )
  }
  return (
    <Link
      to="/data/$authority/$plural"
      params={{ authority: target.authority, plural: target.plural }}
      className="block truncate data underline-offset-4 hover:underline"
      title={target.identity}
    >
      {label}
    </Link>
  )
}

function edgeColumns(
  kind: KindInfo,
  kinds: KindInfo[]
): DataTableColumn<DeclaredEdge>[] {
  return [
    {
      id: "edge",
      accessorFn: (e) => e.rel,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="edge" />
      ),
      cell: ({ row }) => (
        <span className="block truncate data" title={row.original.rel}>
          {row.original.rel}
        </span>
      ),
      meta: { label: "edge", size: { min: 140, max: 260, weight: 1 } },
    },
    {
      id: "to",
      accessorFn: (e) => e.to,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="to" />
      ),
      cell: ({ row }) => (
        <TargetCell edge={row.original} kind={kind} kinds={kinds} />
      ),
      meta: { label: "to", size: { min: 120, max: 220, weight: 0.75 } },
    },
    {
      id: "required",
      accessorFn: (e) => e.required === true,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="required" />
      ),
      cell: ({ row }) =>
        row.original.required ? (
          <Badge variant="secondary" className="px-1.5 font-normal">
            required
          </Badge>
        ) : (
          <Muted>—</Muted>
        ),
      meta: { label: "required", width: 96 },
    },
    {
      id: "description",
      accessorFn: (e) => e.description,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="description" />
      ),
      cell: ({ row }) => {
        const text = row.original.description
        if (!text) return <Muted>—</Muted>
        return (
          <span className="block truncate" title={text}>
            {text}
          </span>
        )
      },
      meta: { label: "description", size: { min: 220, weight: 2 } },
    },
  ]
}

function PropertyTable({ kind }: { kind: KindInfo }) {
  const rows = useMemo(() => declaredProperties(kind), [kind])
  const columns = useMemo(() => propertyColumns(), [])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => row.name,
    prefsKey: "kind-definition-properties",
  })
  if (!rows.length) return null
  return (
    <Section title="Properties" count={rows.length}>
      <DataTable table={table} density="compact" />
    </Section>
  )
}

function EdgeTable({ kind, kinds }: { kind: KindInfo; kinds: KindInfo[] }) {
  const rows = useMemo(() => declaredEdges(kind), [kind])
  const columns = useMemo(() => edgeColumns(kind, kinds), [kind, kinds])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => row.rel,
    prefsKey: "kind-definition-edges",
  })
  if (!rows.length) return null
  return (
    <Section title="Edges" count={rows.length}>
      <DataTable table={table} density="compact" />
    </Section>
  )
}

/** The kind's own facts, above its declaration: the reference the API and the
 * CLI both address it by, its version, and where it came from. */
function Facts({ kind }: { kind: KindInfo }) {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 px-6 pt-4 text-xs text-muted-foreground">
      <span className="data text-foreground/80">{kind.identity}</span>
      {kind.version ? <span>version {kind.version}</span> : null}
      <span>{kind.source}</span>
    </div>
  )
}

export function KindDefinition({
  kind,
  kinds,
}: {
  kind: KindInfo
  /** The registry, so an edge target resolves to a real collection. */
  kinds: KindInfo[]
}) {
  const yaml = useMemo(() => kindManifestYAML(kind), [kind])
  // A declaration's own property keys hover with their one-liners — the same
  // vocabulary the record manifest reads, pointed at the declaring kind.
  const docs = useMemo(() => keyDocsOf(kind), [kind])
  const targets = useMemo(() => kindLinkTargets(kinds), [kinds])

  if (!kind.definition || !Object.keys(kind.definition).length) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FileQuestionIcon />
          </EmptyMedia>
          <EmptyTitle>No stored declaration</EmptyTitle>
          <EmptyDescription>
            The registry knows <span className="data">{kind.identity}</span> but
            carries no declaration for it.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="flex flex-col pb-6">
      <Facts kind={kind} />
      <Section title="Declaration">
        <div className="px-2">
          <YamlView source={yaml} docs={docs} targets={targets} />
        </div>
      </Section>
      <PropertyTable kind={kind} />
      <EdgeTable kind={kind} kinds={kinds} />
    </div>
  )
}
