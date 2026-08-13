/** Registry (`/registry`): every bundle this substrate knows — the IMPORTED
 * bundles with their runtime state (from the computed status endpoint) AND the
 * closures shipped in the catalog that have not been imported yet. One row per
 * bundle on THE table system; a not-imported row carries an Import button,
 * an imported row opens the bundle's detail with the lifecycle verbs, the
 * config form and the accounts/connect flow. Integration bundles (backend
 * `integration` facet) carry an Integration badge and can be narrowed with the
 * All / Vocabulary / Integrations filter, orthogonal to the imported state.
 *
 * EVERY ROW DISCLOSES ITS CLOSURE (owner ask): a fresh repository holds
 * `core.substrate.reamde.dev` and nothing else, so the reader meets this page before they
 * have any vocabulary at all and must be able to see what an import will DO
 * before pressing it. The chevron opens the closure in place — the kinds it
 * adds (linked once they are imported), its functions, agents, triggers and
 * mappings, its version and owned authority, and the authorities it REQUIRES,
 * each marked present or missing.
 *
 * REQUIREMENTS ARE A GATE, not a surprise: `schema.resolveBundle` refuses an
 * install whose `requires:` authorities are absent, so the console refuses it
 * first — Import is disabled with a tooltip naming what to import first. If the
 * server still refuses (a race), its own problems ride the toast verbatim.
 *
 * VOCABULARY (owner ruling): the reader IMPORTS an bundle from the registry.
 * enable/disable/uninstall are a DIFFERENT lifecycle and keep their own words.
 * The wire is untouched — importing still POSTs `…/catalog/{id}/install`. */

import { useMemo } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import {
  BotIcon,
  BoxesIcon,
  CheckIcon,
  DownloadIcon,
  FunctionSquareIcon,
  SearchXIcon,
  TriangleAlertIcon,
  ZapIcon,
} from "lucide-react"
import { parseAsStringLiteral, useQueryState } from "nuqs"

import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { RowDetail } from "@/components/data-table/row-detail"
import { BundleStateBadge } from "@/components/bundle-state-badge"
import { Badge } from "@/components/ui/badge"
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
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import {
  bundleState,
  bundleStatusesQueryOptions,
  refetchBundleStateSoon,
  seedBundleStatus,
} from "@/lib/api/bundles"
import { catalogQueryOptions, importBundle } from "@/lib/api/catalog"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo } from "@/lib/api/types"
import {
  bundleResourceRows,
  filterBundles,
  importFailureText,
  installedKindRows,
  mergeBundles,
  missingRequirements,
  presentAuthorities,
  requirementsOf,
  requiresHint,
  type BundleFacet,
  type BundleRow,
  type Requirement,
  type ResourceKind,
} from "@/lib/bundles"

/** A row's counts: the live status when imported, else the catalog closure's
 * declared resource counts (nothing is live yet, so accounts/rows read 0). The
 * live status counts are optional on the v1 wire — guard each with `?? 0`. */
function counts(row: BundleRow): {
  accounts: number
  functions: number
  kinds: number
  liveRecords: number
} {
  if (row.status) {
    const s = row.status
    return {
      accounts: s.accounts ?? 0,
      functions: s.functions ?? 0,
      kinds: s.kinds ?? 0,
      liveRecords: s.liveRecords ?? 0,
    }
  }
  const r = row.catalog?.resources
  return {
    accounts: 0,
    functions: r?.functions?.length ?? 0,
    kinds: r?.kinds?.length ?? 0,
    liveRecords: 0,
  }
}

function numColumn(
  id: string,
  title: string,
  value: (r: BundleRow) => number
): ColumnDef<BundleRow, unknown> {
  return {
    id,
    accessorFn: value,
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={title} align="right" />
    ),
    cell: ({ row }) => (
      <span className="block text-right data text-muted-foreground">
        {value(row.original).toLocaleString()}
      </span>
    ),
    meta: {
      label: title,
      width: 90,
      headerClassName: "text-right",
      cellClassName: "text-right",
    },
  }
}

/** Import, gated by the closure's own `requires:`. A missing requirement is a
 * refusal the server WILL make (schema.resolveBundle), so the button is
 * disabled and its tooltip names what to import first — the trigger is a span,
 * since a disabled button dispatches no pointer events. A refusal that still
 * arrives (the requirement was torn down between the read and the click)
 * surfaces the server's own problems verbatim. */
function ImportButton({
  row,
  missing,
}: {
  row: BundleRow
  missing: Requirement[]
}) {
  const queryClient = useQueryClient()
  const importing = useMutation({
    mutationFn: () => importBundle(row.id),
    onSuccess: (status) => {
      toast.add({ type: "success", title: `${row.name} imported.` })
      // The import answers with the fresh status — seed it so this row flips
      // to imported immediately, without waiting on the next status probe.
      seedBundleStatus(queryClient, status)
      // An import lands schema + wiring the whole console reads — refresh all,
      // and re-read the bundle surfaces again shortly since the probe-backed
      // reads can lag it.
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: `Could not import ${row.name}`,
        description: importFailureText(error),
      })
    },
  })

  const blocked = missing.length > 0
  const button = (
    <Button
      variant="outline"
      size="sm"
      disabled={blocked || importing.isPending}
      onClick={(e) => {
        e.stopPropagation()
        importing.mutate()
      }}
    >
      {importing.isPending ? (
        <Spinner className="size-3.5" />
      ) : (
        <DownloadIcon />
      )}
      {importing.isPending ? "Importing…" : "Import"}
    </Button>
  )
  if (!blocked) return button
  const hint = requiresHint(missing)
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex cursor-help" />}>
        {button}
        <span className="sr-only">{hint}</span>
      </TooltipTrigger>
      <TooltipContent>{hint}</TooltipContent>
    </Tooltip>
  )
}

/** The two catalog facets said plainly. They are disjoint by construction (a
 * vocabulary bundle owns a bare authority and ships kinds alone; an
 * integration owns a categorized one and connects a provider), so a row wears
 * at most one. */
function FacetBadge({ row }: { row: BundleRow }) {
  if (row.vocabulary) {
    return (
      <Badge variant="outline" className="shrink-0 font-normal">
        Vocabulary
      </Badge>
    )
  }
  if (row.integration) {
    return (
      <Badge variant="outline" className="shrink-0 font-normal">
        Integration
      </Badge>
    )
  }
  return null
}

function buildColumns(
  requirements: (row: BundleRow) => Requirement[]
): ColumnDef<BundleRow, unknown>[] {
  return [
    {
      id: "bundle",
      accessorFn: (r) => r.name,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="bundle" />
      ),
      cell: ({ row }) => (
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate font-medium">{row.original.name}</span>
            <FacetBadge row={row.original} />
          </div>
          <div
            className="truncate data text-xs text-muted-foreground"
            title={row.original.catalog?.description || row.original.authority}
          >
            {row.original.catalog?.description || row.original.authority}
          </div>
        </div>
      ),
      meta: { label: "bundle", size: { min: 220, max: 460, weight: 1.5 } },
    },
    {
      id: "state",
      accessorFn: (r) => (r.status ? bundleState(r.status) : "not imported"),
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="state" />
      ),
      // An imported bundle shows its own runtime lifecycle (enabled /
      // disabled / needs configuration); one that has never been imported has
      // no lifecycle to show, only the invitation — and, when its closure
      // declares against vocabulary this repository lacks, what blocks it.
      cell: ({ row }) => {
        const missing = row.original.installed
          ? []
          : missingRequirements(requirements(row.original))
        return (
          <div className="min-w-0">
            {row.original.status ? (
              <BundleStateBadge state={bundleState(row.original.status)} />
            ) : (
              <Badge variant="outline" className="gap-1.5 font-normal">
                <span className="size-1.5 rounded-full bg-muted-foreground/40" />
                <span className="data">not imported</span>
              </Badge>
            )}
            {missing.length > 0 && (
              <div
                className="truncate pt-0.5 data text-xs text-warning"
                title={requiresHint(missing)}
              >
                needs {missing.map((r) => r.authority).join(", ")}
              </div>
            )}
          </div>
        )
      },
      meta: { label: "state", width: 170 },
    },
    numColumn("accounts", "accounts", (r) => counts(r).accounts),
    numColumn("functions", "functions", (r) => counts(r).functions),
    numColumn("kinds", "kinds", (r) => counts(r).kinds),
    numColumn("liveRecords", "live rows", (r) => counts(r).liveRecords),
    {
      id: "action",
      enableSorting: false,
      enableHiding: false,
      header: () => <span className="sr-only">action</span>,
      cell: ({ row }) =>
        row.original.installed ? null : row.original.catalog ? (
          <div className="flex justify-end">
            <ImportButton
              row={row.original}
              missing={missingRequirements(requirements(row.original))}
            />
          </div>
        ) : null,
      meta: {
        label: "action",
        width: 120,
        headerClassName: "text-right",
        cellClassName: "text-right",
      },
    },
  ]
}

// ── the disclosure: what an import will actually do ─────────────────────────

/** One labelled line of the disclosure grid. */
function Line({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <>
      <dt className="pt-0.5 text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 flex-wrap items-center gap-1">{children}</dd>
    </>
  )
}

const RESOURCE_ICON: Record<ResourceKind, typeof BotIcon> = {
  function: FunctionSquareIcon,
  agent: BotIcon,
  trigger: ZapIcon,
  mapping: BoxesIcon,
}

const RESOURCE_LABEL: Record<ResourceKind, string> = {
  function: "functions",
  agent: "agents",
  trigger: "triggers",
  mapping: "mappings",
}

const RESOURCE_ORDER: ResourceKind[] = ["function", "agent", "trigger", "mapping"]

/** The row's closure, opened in place: what it adds, what it needs, and what it
 * IS (vocabulary or integration) — everything the reader needs to decide before
 * the import, in the table's own voice. */
function BundleDisclosure({
  row,
  requirements,
  kinds,
}: {
  row: BundleRow
  requirements: Requirement[]
  kinds: KindInfo[]
}) {
  const catalog = row.catalog
  const configType = catalog?.configType ?? row.status?.configType
  const kindRows = useMemo(
    () =>
      installedKindRows(
        { authority: row.authority, configType },
        kinds,
        catalog
      ),
    [row.authority, configType, kinds, catalog]
  )
  const resources = useMemo(() => bundleResourceRows(catalog), [catalog])
  const missing = missingRequirements(requirements)

  return (
    <RowDetail>
      {catalog?.description && <p>{catalog.description}</p>}
      <dl className="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-1.5">
        <Line label="authority">
          <span className="data text-muted-foreground">{row.authority}</span>
          {catalog?.version && (
            <span className="data text-muted-foreground">
              · {catalog.version}
            </span>
          )}
        </Line>
        <Line label="bundle">
          <span>
            {row.vocabulary
              ? "Vocabulary — record kinds and nothing else: no configuration, no functions, no provider account."
              : row.integration
                ? "Integration — connects an external provider through its own configuration and accounts."
                : "Bundle — ships callables and configuration in its own authority."}
          </span>
        </Line>
        {requirements.length > 0 && (
          <Line label="requires">
            {requirements.map((req) => (
              <span
                key={req.authority}
                className={cn(
                  "inline-flex items-center gap-1 rounded border px-1.5 py-0.5 data",
                  req.present
                    ? "bg-background text-muted-foreground"
                    : "border-warning/40 text-warning"
                )}
                title={
                  req.present
                    ? `${req.authority} is imported`
                    : `${req.authority} is not imported — the import is refused until it is`
                }
              >
                {req.present ? (
                  <CheckIcon className="size-3 shrink-0" />
                ) : (
                  <TriangleAlertIcon className="size-3 shrink-0" />
                )}
                {req.authority}
                <span className="sr-only">
                  {req.present ? " imported" : " missing"}
                </span>
              </span>
            ))}
          </Line>
        )}
        <Line label="kinds">
          {kindRows.length ? (
            kindRows.map((k) => {
              // The host's own roles ride the hover, not a second chip: the
              // singleton `config` record type and the `account` type the
              // connect flow writes tokens onto are the two the reader must be
              // able to tell from ordinary vocabulary.
              const named = k.role
                ? `${k.identity} — the ${k.role} record type`
                : k.identity
              // What the kind is, on the same hover: a reader deciding on an
              // import should not have to install it to find out.
              const title = k.description
                ? `${named}\n\n${k.description}`
                : named
              return k.authority && k.plural ? (
                <Link
                  key={k.identity}
                  to="/data/$authority/$plural"
                  params={{ authority: k.authority, plural: k.plural }}
                  className="rounded border bg-background px-1.5 py-0.5 data underline-offset-4 hover:underline"
                  title={title}
                  onClick={(e) => e.stopPropagation()}
                >
                  {k.name}
                </Link>
              ) : (
                // Not imported yet (or unreconciled): the kind exists on paper
                // only, so it names itself and links nowhere.
                <span
                  key={k.identity}
                  className="rounded border bg-background px-1.5 py-0.5 data text-muted-foreground"
                  title={title}
                >
                  {k.name}
                </span>
              )
            })
          ) : (
            <span className="text-muted-foreground">none</span>
          )}
        </Line>
        {RESOURCE_ORDER.map((kind) => {
          const members = resources.filter((r) => r.kind === kind)
          if (!members.length) return null
          const Icon = RESOURCE_ICON[kind]
          return (
            <Line key={kind} label={RESOURCE_LABEL[kind]}>
              {members.map((m) => (
                <span
                  key={m.identity}
                  className="inline-flex items-center gap-1 rounded border bg-background px-1.5 py-0.5 data text-muted-foreground"
                  title={m.identity}
                >
                  <Icon className="size-3 shrink-0" />
                  {m.name}
                </span>
              ))}
            </Line>
          )
        })}
      </dl>
      {missing.length > 0 && (
        <p className="text-warning">{requiresHint(missing)}</p>
      )}
      {!catalog && (
        <p className="text-muted-foreground">
          This bundle was applied directly — the shipped catalog has no closure
          for it, so only what the registry reconciled is listed.
        </p>
      )}
      {row.installed && (
        <span className="text-muted-foreground">
          <Link
            to="/registry/$id"
            params={{ id: row.id }}
            className="underline-offset-4 hover:underline"
            onClick={(e) => e.stopPropagation()}
          >
            Open bundle
          </Link>
        </span>
      )}
    </RowDetail>
  )
}

const FACETS = ["all", "vocabulary", "integrations", "examples"] as const

/** The catalog facet toggle: All / Vocabulary / Integrations, orthogonal to
 * whether a row is imported. Full-size h-8 outline controls (GUIDE rule 3 —
 * filters are controls, not chips); the choice lives in the URL (nuqs). */
function FacetFilter({
  value,
  onChange,
}: {
  value: BundleFacet
  onChange: (facet: BundleFacet) => void
}) {
  const options: { value: BundleFacet; label: string }[] = [
    { value: "all", label: "All" },
    { value: "vocabulary", label: "Vocabulary" },
    { value: "integrations", label: "Integrations" },
    { value: "examples", label: "Examples" },
  ]
  return (
    <div className="flex h-8 items-center rounded-md border p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          className={cn(
            "h-7 rounded-sm px-2.5 text-xs font-medium transition-colors",
            value === o.value
              ? "bg-muted text-foreground"
              : "text-muted-foreground hover:text-foreground"
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

export function RegistryPage() {
  const navigate = useNavigate()
  const statuses = useQuery(bundleStatusesQueryOptions)
  const catalog = useQuery(catalogQueryOptions)
  // The kind registry answers two questions here: which authorities this
  // repository already holds (a requirement is met when its authority is
  // live), and where a closure's kinds actually browse once imported.
  const registry = useQuery(kindsQueryOptions)
  const kinds = useMemo(() => registry.data ?? [], [registry.data])

  const [facet, setFacet] = useQueryState(
    "facet",
    parseAsStringLiteral(FACETS).withDefault("all")
  )

  const allRows = useMemo(
    () => mergeBundles(statuses.data ?? [], catalog.data ?? []),
    [statuses.data, catalog.data]
  )
  // Presence is computed over EVERY row, never the filtered view: hiding the
  // vocabulary behind a facet must not make an integration look unimportable.
  const present = useMemo(
    () => presentAuthorities(allRows, kinds),
    [allRows, kinds]
  )
  const requirements = useMemo(() => {
    const byId = new Map<string, Requirement[]>()
    for (const row of allRows) byId.set(row.id, requirementsOf(row, present))
    return (row: BundleRow) => byId.get(row.id) ?? []
  }, [allRows, present])

  const rows = useMemo(() => filterBundles(allRows, facet), [allRows, facet])
  const importedCount = rows.filter((r) => r.installed).length
  const notImportedCount = rows.length - importedCount
  const columns = useMemo(() => buildColumns(requirements), [requirements])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => row.id,
    prefsKey: "registry",
  })

  // The kind registry is a read the whole console shares (the sidebar holds it
  // warm); waiting for it here keeps a requirement from reading as missing for
  // one frame and disabling an Import that is perfectly legal.
  if (statuses.isPending || catalog.isPending || registry.isPending)
    return <RegistrySkeleton />

  if (statuses.isError || catalog.isError) {
    const error = statuses.error ?? catalog.error
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>The bundles didn't load</EmptyTitle>
            <EmptyDescription>{error?.message}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                void statuses.refetch()
                void catalog.refetch()
              }}
            >
              Retry
            </Button>
          </EmptyContent>
        </Empty>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-end justify-between gap-3 px-6 pt-5 pb-2">
        <div>
          <h1 className="text-lg font-semibold">Registry</h1>
          <p className="text-xs text-muted-foreground">
            {importedCount.toLocaleString()} imported,{" "}
            {notImportedCount.toLocaleString()} not imported
            {facet === "integrations"
              ? " (integrations)"
              : facet === "vocabulary"
                ? " (vocabulary)"
                : ""}
            , from <span className="data">core.substrate.reamde.dev/catalog</span>
          </p>
          <p className="pt-0.5 text-xs text-muted-foreground">
            A new repository ships <span className="data">core.substrate.reamde.dev</span>{" "}
            alone — the vocabulary it records into and every integration are
            imported from here. Expand a row to see what it adds.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <FacetFilter value={facet} onChange={(f) => void setFacet(f)} />
          <DataTableViewOptions table={table} />
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <DataTable
          table={table}
          onRowClick={(row) => {
            // Only imported bundles have a detail page (it reads runtime
            // status); a catalog closure is imported from its row button, and
            // read from the chevron's disclosure.
            if (row.status)
              void navigate({ to: "/registry/$id", params: { id: row.id } })
          }}
          renderExpanded={(row) => (
            <BundleDisclosure
              row={row}
              requirements={requirements(row)}
              kinds={kinds}
            />
          )}
          empty={
            <Empty className="py-16">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <BoxesIcon />
                </EmptyMedia>
                <EmptyTitle>
                  {facet === "integrations"
                    ? "No integrations"
                    : facet === "vocabulary"
                      ? "No vocabulary bundles"
                      : "No bundles"}
                </EmptyTitle>
                <EmptyDescription>
                  {facet === "integrations"
                    ? "No bundle in the catalog connects an external provider."
                    : facet === "vocabulary"
                      ? "No bundle in the catalog ships vocabulary alone."
                      : "Nothing is imported and the catalog is empty."}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          }
        />
      </div>
    </div>
  )
}

function RegistrySkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-1">
        <Skeleton className="h-6 w-32" />
        <Skeleton className="mt-1.5 h-3.5 w-56" />
      </div>
      <div className="min-h-0 flex-1 overflow-hidden px-6 pt-4">
        {Array.from({ length: 5 }, (_, i) => (
          <div key={i} className="flex h-12 items-center gap-6 border-b last:border-0">
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-4 w-24" />
            <Skeleton className="ml-auto h-4 w-16" />
          </div>
        ))}
      </div>
    </div>
  )
}
