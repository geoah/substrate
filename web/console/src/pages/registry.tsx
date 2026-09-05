/** Registry (`/registry`): every bundle this substrate knows: the ones this
 * repository holds, with their runtime state (from the computed status
 * endpoint), and the closures shipped in the catalog it has not taken yet.
 *
 * TWO SECTIONS, the two catalog tiers (decision record 0048). PROVIDERS are
 * packages a publisher owns: they install under the authority that publishes
 * them, and their upgrades are offered here. SAMPLES are vocabulary to copy:
 * importing one rewrites it onto THIS repository's authority, so the row
 * previews the identity it will land under before the button is pressed, and
 * nothing upstream can change it afterwards. A bundle applied outside the
 * shipped catalog has no tier and is listed on its own rather than guessed
 * into one.
 *
 * EVERY ROW DISCLOSES ITS CLOSURE (owner ask): a fresh repository holds
 * `substrate.reamde.dev/core` and nothing else, so the reader meets this page before they
 * have any vocabulary at all and must be able to see what an import will DO
 * before pressing it. The chevron opens the closure in place — the kinds it
 * adds (linked once they are imported), its functions, agents, triggers and
 * mappings, its version and owned authority, and the authorities it REQUIRES,
 * each marked present or missing.
 *
 * REQUIREMENTS ARE A GATE, not a surprise: `schema.resolveBundle` refuses an
 * install whose `requires:` packages are absent, so the console refuses it
 * first, so the button is disabled with a tooltip naming what to take first. A
 * sample's requirements are shown REHOMED, under this repository's authority,
 * because that is what the server will look for. If the server still refuses
 * (a race), its own problems ride the toast verbatim.
 *
 * The two doors are two endpoints: `…/catalog/{id}/install` for a provider,
 * `…/catalog/{id}/import` for a sample. enable/disable/uninstall are a
 * DIFFERENT lifecycle and keep their own words. */

import { useMemo } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import type { DataTableColumn } from "@/components/data-table/data-table"
import {
  BotIcon,
  BoxesIcon,
  BoxIcon,
  CheckIcon,
  CircleArrowUpIcon,
  DownloadIcon,
  FunctionSquareIcon,
  SearchXIcon,
  TriangleAlertIcon,
  ZapIcon,
} from "lucide-react"
import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { RowDetail } from "@/components/data-table/row-detail"
import { BundleStateBadge, SetupBadge } from "@/components/bundle-state-badge"
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import {
  bundleState,
  bundleStatusesQueryOptions,
  refetchBundleStateSoon,
  seedBundleStatus,
  setupCount,
} from "@/lib/api/bundles"
import {
  catalogQueryOptions,
  installBundle,
  takeBundle,
} from "@/lib/api/catalog"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { CORE_PACKAGE } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo } from "@/lib/api/types"
import { splitKind } from "@/lib/definition"
import {
  bundleRecordRows,
  bundleSections,
  importFailureText,
  installedKindRows,
  mergeBundles,
  missingRequirements,
  presentPackages,
  requirementsOf,
  requiresHint,
  upgradeAvailable,
  upgradeBlocked,
  upgradeMotion,
  type BundleRow,
  type Requirement,
} from "@/lib/bundles"

/** A row's counts: the live status when imported, else the catalog closure's
 * declared closure counts (nothing is live yet, so accounts/rows read 0). The
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
  const r = row.catalog?.closure
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
): DataTableColumn<BundleRow> {
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

/** The row's door, named for its tier: a PROVIDER installs under the authority
 * that publishes it, a SAMPLE imports as yours. Gated by the closure's own
 * `requires:`. A missing requirement is a refusal the server WILL make
 * (schema.resolveBundle), so the button is disabled and its tooltip names what
 * to take first; the trigger is a span, since a disabled button dispatches no
 * pointer events. A refusal that still arrives (the requirement was torn down
 * between the read and the click) surfaces the server's own problems
 * verbatim. */
function TakeButton({
  row,
  missing,
}: {
  row: BundleRow
  missing: Requirement[]
}) {
  const queryClient = useQueryClient()
  const sample = row.tier === "sample"
  const verb = sample ? "Import as yours" : "Install"
  const running = sample ? "Importing…" : "Installing…"
  const taking = useMutation({
    mutationFn: () =>
      takeBundle({
        id: row.catalog?.id ?? row.id,
        tier: row.tier ?? "provider",
      }),
    onSuccess: (status) => {
      toast.add({
        type: "success",
        title: sample
          ? `${row.name} imported as ${status.id}.`
          : `${row.name} installed.`,
      })
      // The door answers with the fresh status, so seed it and this row flips to
      // held immediately, without waiting on the next status probe.
      seedBundleStatus(queryClient, status)
      // It lands schema + wiring the whole console reads, so refresh all, and
      // re-read the bundle surfaces again shortly since the probe-backed reads
      // can lag it.
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: `Could not ${sample ? "import" : "install"} ${row.name}`,
        description: importFailureText(error),
      })
    },
  })

  const blocked = missing.length > 0
  const button = (
    <Button
      variant="outline"
      size="sm"
      disabled={blocked || taking.isPending}
      onClick={(e) => {
        e.stopPropagation()
        taking.mutate()
      }}
    >
      {taking.isPending ? <Spinner className="size-3.5" /> : <DownloadIcon />}
      {taking.isPending ? running : verb}
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

/** Upgrade: re-install the shipped closure, which is the provider's own
 * upgrade verb (`…/catalog/{id}/install`). Offered only when the server's
 * preview says the closure moved AND nothing blocks it; a BLOCKED upgrade
 * renders as UpgradeBlockedChip instead, because the server would refuse it,
 * so the console never offers the click (owner decision: no force). A SAMPLE
 * never reaches here: the server attaches no preview to one, because what it
 * landed belongs to the repository (decision record 0048). */
function UpgradeButton({ row }: { row: BundleRow }) {
  const queryClient = useQueryClient()
  const upgrade = row.upgrade
  const upgrading = useMutation({
    mutationFn: () => installBundle(row.catalog?.id ?? row.id),
    onSuccess: (status) => {
      toast.add({
        type: "success",
        title: upgrade?.to
          ? `${row.name} upgraded to ${upgrade.to}.`
          : `${row.name} upgraded.`,
      })
      seedBundleStatus(queryClient, status)
      // The upgrade lands schema the whole console reads, and the catalog's
      // preview must re-read as current: refresh everything.
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: `Could not upgrade ${row.name}`,
        description: importFailureText(error),
      })
    },
  })
  const motion = upgrade ? upgradeMotion(upgrade) : ""
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            disabled={upgrading.isPending}
            onClick={(e) => {
              e.stopPropagation()
              upgrading.mutate()
            }}
          />
        }
      >
        {upgrading.isPending ? (
          <Spinner className="size-3.5" />
        ) : (
          <CircleArrowUpIcon />
        )}
        {upgrading.isPending ? "Upgrading…" : "Upgrade"}
      </TooltipTrigger>
      {motion && <TooltipContent>{motion}</TooltipContent>}
    </Tooltip>
  )
}

/** A blocked upgrade, stated instead of offered: the shipped closure moved,
 * but re-importing it would strand live records, and the server refuses that
 * (refuse-breakage). The chip's tooltip carries the server's own guard lines,
 * which name the kind, the property and the count: the reader's migration
 * instructions. */
function UpgradeBlockedChip({ row }: { row: BundleRow }) {
  const blockers = row.upgrade?.blockers ?? []
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex cursor-help" />}>
        <Badge
          variant="outline"
          className="gap-1 border-warning/40 font-normal text-warning"
        >
          <TriangleAlertIcon className="size-3 shrink-0" />
          <span className="data">upgrade blocked</span>
        </Badge>
        <span className="sr-only">{blockers.join("; ")}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-96">
        <div className="space-y-1">
          {blockers.map((b) => (
            <p key={b}>{b}</p>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  )
}

function buildColumns(
  requirements: (row: BundleRow) => Requirement[]
): DataTableColumn<BundleRow>[] {
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
      accessorFn: (r) => (r.status ? bundleState(r.status) : "not taken"),
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="state" />
      ),
      // An imported bundle shows its own runtime lifecycle (enabled /
      // disabled / uninstalled) with the setup chip BESIDE it when steps
      // stand; one that has never been imported has no lifecycle to show, only
      // the invitation — and, when its closure declares against vocabulary
      // this repository lacks, what blocks it.
      cell: ({ row }) => {
        const missing = row.original.installed
          ? []
          : missingRequirements(requirements(row.original))
        return (
          <div className="min-w-0">
            {row.original.status ? (
              <span className="inline-flex flex-wrap items-center gap-1.5">
                <BundleStateBadge state={bundleState(row.original.status)} />
                <SetupBadge count={setupCount(row.original.status)} />
              </span>
            ) : (
              <Badge variant="outline" className="gap-1.5 font-normal">
                <span className="size-1.5 rounded-full bg-muted-foreground/40" />
                <span className="data">
                  {row.original.tier === "sample"
                    ? "not imported"
                    : "not installed"}
                </span>
              </Badge>
            )}
            {/* A sample is rewritten onto this repository's authority on the
                way in, so the row says the identity it will land under before
                the button is pressed, since the reader is about to own it. */}
            {!row.original.status && row.original.tier === "sample" && (
              <div
                className="truncate pt-0.5 data text-xs text-muted-foreground"
                title={`Importing lands ${row.original.id}, yours to edit`}
              >
                lands as {row.original.id}
              </div>
            )}
            {missing.length > 0 && (
              <div
                className="truncate pt-0.5 data text-xs text-warning"
                title={requiresHint(missing)}
              >
                needs {missing.map((r) => r.package).join(", ")}
              </div>
            )}
            {row.original.upgrade && upgradeAvailable(row.original) && (
              <div className="truncate pt-0.5 data text-xs text-muted-foreground">
                update {upgradeMotion(row.original.upgrade)}
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
        row.original.installed ? (
          upgradeAvailable(row.original) ? (
            <div className="flex justify-end">
              {upgradeBlocked(row.original) ? (
                <UpgradeBlockedChip row={row.original} />
              ) : (
                <UpgradeButton row={row.original} />
              )}
            </div>
          ) : null
        ) : row.original.catalog ? (
          <div className="flex justify-end">
            <TakeButton
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

/** The shipped-record kinds this preview groups by, in reading order: the
 * declarations first, then the data rows an install writes. A record of any
 * other kind falls into the trailing `records` line, so a bundle shipping
 * something new is previewed rather than dropped. */
const RECORD_KINDS = [
  {
    kind: `${CORE_PACKAGE}/function`,
    label: "functions",
    icon: FunctionSquareIcon,
  },
  { kind: `${CORE_PACKAGE}/agent`, label: "agents", icon: BotIcon },
  {
    kind: `${CORE_PACKAGE}/recordmapping`,
    label: "mappings",
    icon: BoxesIcon,
  },
  { kind: `${CORE_PACKAGE}/trigger`, label: "triggers", icon: ZapIcon },
] as const

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
  const inputs = row.status?.inputs
  const kindRows = useMemo(
    () => installedKindRows({ id: row.id, inputs }, kinds, catalog),
    [row.id, inputs, kinds, catalog]
  )
  const records = useMemo(() => bundleRecordRows(catalog), [catalog])
  const missing = missingRequirements(requirements)

  return (
    <RowDetail>
      {catalog?.description && <p>{catalog.description}</p>}
      <dl className="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-1.5">
        <Line label="authority">
          <span className="data text-muted-foreground">{row.authority}</span>
          {catalog?.version ? (
            <span className="data text-muted-foreground">
              · {catalog.version}
            </span>
          ) : null}
        </Line>
        {row.upgrade?.available && (
          <Line label="upgrade">
            <span className="data">{upgradeMotion(row.upgrade)}</span>
            {(row.upgrade.changes ?? []).map((ch) => (
              <span
                key={`${ch.kind}:${ch.id}`}
                className="inline-flex items-center gap-1 rounded border bg-background px-1.5 py-0.5 data text-muted-foreground"
                title={
                  ch.to
                    ? ch.from
                      ? `${ch.id}: ${ch.from} → ${ch.to}`
                      : `${ch.id}: new at ${ch.to}`
                    : `${ch.id}: removed by this upgrade`
                }
              >
                {ch.kind} {splitKind(ch.id).name}
              </span>
            ))}
          </Line>
        )}
        <Line label="tier">
          <span>
            {row.tier === "provider"
              ? "Provider: a published package. It installs under the authority that publishes it, and its publisher ships each change as an upgrade."
              : row.tier === "sample"
                ? `Sample: vocabulary to copy. Importing lands it as ${row.id}, yours to edit, and nothing upstream changes it afterwards.`
                : "Applied directly: this bundle is not in the shipped catalog, so it has no tier and no closure to preview."}
          </span>
        </Line>
        {catalog?.inputs && Object.keys(catalog.inputs).length > 0 && (
          <Line label="inputs">
            {Object.entries(catalog.inputs).map(([name, input]) => (
              <span
                key={name}
                className="inline-flex items-center gap-1 rounded border bg-background px-1.5 py-0.5 data text-muted-foreground"
                title={
                  input.description
                    ? `${input.kind}\n\n${input.description}`
                    : input.kind
                }
              >
                {name}
              </span>
            ))}
          </Line>
        )}
        {requirements.length > 0 && (
          <Line label="requires">
            {requirements.map((req) => (
              <span
                key={req.package}
                className={cn(
                  "inline-flex items-center gap-1 rounded border px-1.5 py-0.5 data",
                  req.present
                    ? "bg-background text-muted-foreground"
                    : "border-warning/40 text-warning"
                )}
                title={
                  req.present
                    ? `${req.package} is imported`
                    : `${req.package} is not imported — the import is refused until it is`
                }
              >
                {req.present ? (
                  <CheckIcon className="size-3 shrink-0" />
                ) : (
                  <TriangleAlertIcon className="size-3 shrink-0" />
                )}
                {req.package}
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
              // The host's own roles ride the hover, not a second chip: a kind
              // a declared input resolves records of, and the `account` kind
              // the connect flow writes tokens onto, are the two the reader
              // must be able to tell from ordinary vocabulary.
              const named =
                k.role === "input"
                  ? `${k.identity} (its records satisfy a declared input)`
                  : k.role === "account"
                    ? `${k.identity} (the account record kind)`
                    : k.identity
              // What the kind is, on the same hover: a reader deciding on an
              // import should not have to install it to find out.
              const title = k.description
                ? `${named}\n\n${k.description}`
                : named
              return k.authority && k.package && k.name ? (
                <Link
                  key={k.identity}
                  to="/data/$authority/$pkg/$name"
                  params={{
                    authority: k.authority,
                    pkg: k.package,
                    name: k.name,
                  }}
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
        {RECORD_KINDS.map(({ kind, label, icon: Icon }) => {
          const members = records.filter((r) => r.kind === kind)
          if (!members.length) return null
          return (
            <Line key={kind} label={label}>
              {members.map((m) => (
                <span
                  key={m.id}
                  className="inline-flex items-center gap-1 rounded border bg-background px-1.5 py-0.5 data text-muted-foreground"
                  title={m.id}
                >
                  <Icon className="size-3 shrink-0" />
                  {m.name}
                </span>
              ))}
            </Line>
          )
        })}
        {(() => {
          // Everything the bundle ships that is not one of the four above —
          // the llm example's provider rows, say. Grouped under one line
          // because the set is open: a bundle may ship a record of any kind.
          const grouped = new Set<string>(RECORD_KINDS.map((r) => r.kind))
          const rest = records.filter((r) => !grouped.has(r.kind))
          if (!rest.length) return null
          return (
            <Line label="records">
              {rest.map((m) => (
                <span
                  key={`${m.kind}:${m.id}`}
                  className="inline-flex items-center gap-1 rounded border bg-background px-1.5 py-0.5 data text-muted-foreground"
                  title={`${m.kind}/${m.id}`}
                >
                  <BoxIcon className="size-3 shrink-0" />
                  {m.name}
                </span>
              ))}
            </Line>
          )
        })()}
      </dl>
      {missing.length > 0 && (
        <p className="text-warning">{requiresHint(missing)}</p>
      )}
      {(row.upgrade?.blockers?.length ?? 0) > 0 && (
        <div className="space-y-1 text-warning">
          <p>
            The upgrade is blocked: live records still hold the shape it would
            drop, and the server refuses to strand them.
          </p>
          {row.upgrade?.blockers?.map((b) => (
            <p key={b} className="data text-xs">
              {b}
            </p>
          ))}
        </div>
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

/** One tier's table: its own heading, its own column preferences, its own
 * empty state. Two of these are the page (decision record 0048), because the
 * two tiers answer different questions: what connects to a service, and what
 * vocabulary to start from. */
function BundleSection({
  title,
  description,
  rows,
  prefsKey,
  emptyTitle,
  emptyDescription,
  requirements,
  kinds,
  onOpen,
}: {
  title: string
  description: string
  rows: BundleRow[]
  prefsKey: string
  emptyTitle: string
  emptyDescription: string
  requirements: (row: BundleRow) => Requirement[]
  kinds: KindInfo[]
  onOpen: (row: BundleRow) => void
}) {
  const columns = useMemo(() => buildColumns(requirements), [requirements])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => row.id,
    prefsKey,
  })
  const held = rows.filter((r) => r.installed).length
  return (
    <section className="pb-6">
      <div className="flex items-end justify-between gap-3 px-6 pt-4 pb-2">
        <div>
          <h2 className="text-sm font-semibold">{title}</h2>
          <p className="text-xs text-muted-foreground">{description}</p>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">
            {held.toLocaleString()} of {rows.length.toLocaleString()}
          </span>
          <DataTableViewOptions table={table} />
        </div>
      </div>
      <DataTable
        table={table}
        onRowClick={(row) => onOpen(row)}
        renderExpanded={(row) => (
          <BundleDisclosure
            row={row}
            requirements={requirements(row)}
            kinds={kinds}
          />
        )}
        empty={
          <Empty className="py-10">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <BoxesIcon />
              </EmptyMedia>
              <EmptyTitle>{emptyTitle}</EmptyTitle>
              <EmptyDescription>{emptyDescription}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        }
      />
    </section>
  )
}

export function RegistryPage() {
  const navigate = useNavigate()
  const statuses = useQuery(bundleStatusesQueryOptions)
  const catalog = useQuery(catalogQueryOptions)
  // The repository's own record answers ONE question: the authority this
  // repository owns, which is where an imported sample lands (decision records
  // 0046 and 0048) and so what a sample row previews.
  const repository = useQuery(repositoryQueryOptions)
  // The kind registry answers two more: which packages this repository already
  // holds (a requirement is met when its package is live), and where a
  // closure's kinds actually browse once they land.
  const registry = useQuery(kindsQueryOptions)
  const kinds = useMemo(() => registry.data ?? [], [registry.data])
  const home = repository.data?.authority ?? ""

  const allRows = useMemo(
    () => mergeBundles(statuses.data ?? [], catalog.data ?? [], home),
    [statuses.data, catalog.data, home]
  )
  // Presence is computed over EVERY row, never one section's: a package in the
  // other section still satisfies a requirement.
  const present = useMemo(
    () => presentPackages(allRows, kinds),
    [allRows, kinds]
  )
  const requirements = useMemo(() => {
    const byId = new Map<string, Requirement[]>()
    for (const row of allRows) byId.set(row.id, requirementsOf(row, present))
    return (row: BundleRow) => byId.get(row.id) ?? []
  }, [allRows, present])
  const sections = useMemo(() => bundleSections(allRows), [allRows])
  const heldCount = allRows.filter((r) => r.installed).length

  // The kind registry is a read the whole console shares (the sidebar holds it
  // warm); waiting for it here keeps a requirement from reading as missing for
  // one frame and disabling a button that is perfectly legal. The repository
  // read is waited on for the same reason: without the authority a sample row
  // would preview the wrong identity for a frame.
  if (
    statuses.isPending ||
    catalog.isPending ||
    registry.isPending ||
    repository.isPending
  )
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

  // Only a bundle this repository holds has a detail page (it reads runtime
  // status); a catalog closure is taken from its row button and read from the
  // chevron's disclosure.
  const open = (row: BundleRow) => {
    if (row.status)
      void navigate({ to: "/registry/$id", params: { id: row.id } })
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-1">
        <h1 className="text-lg font-semibold">Registry</h1>
        <p className="text-xs text-muted-foreground">
          {heldCount.toLocaleString()} of {allRows.length.toLocaleString()}{" "}
          taken, from <span className="data">/api/v1/catalog</span>
        </p>
        <p className="pt-0.5 text-xs text-muted-foreground">
          A new repository ships{" "}
          <span className="data">substrate.reamde.dev/core</span> alone, and
          every other kind it records into comes from here. Expand a row to see
          what it adds.
        </p>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <BundleSection
          title="Providers"
          description="Packages a publisher owns: they install under the authority that publishes them, and their upgrades arrive here."
          rows={sections.providers}
          prefsKey="registry.providers"
          emptyTitle="No providers"
          emptyDescription="This binary ships no provider packages."
          requirements={requirements}
          kinds={kinds}
          onOpen={open}
        />
        <BundleSection
          title="Samples"
          description={
            home
              ? `Vocabulary to copy: importing one lands it under ${home}, yours to edit.`
              : "Vocabulary to copy: importing one lands it under this repository's own authority, yours to edit."
          }
          rows={sections.samples}
          prefsKey="registry.samples"
          emptyTitle="No samples"
          emptyDescription="This binary ships no sample packages."
          requirements={requirements}
          kinds={kinds}
          onOpen={open}
        />
        {sections.applied.length > 0 && (
          <BundleSection
            title="Applied directly"
            description="Bundles this repository applied outside the shipped catalog, so there is no closure to preview and no tier to place them under."
            rows={sections.applied}
            prefsKey="registry.applied"
            emptyTitle="Nothing applied directly"
            emptyDescription="Every bundle here came from the shipped catalog."
            requirements={requirements}
            kinds={kinds}
            onOpen={open}
          />
        )}
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
          <div
            key={i}
            className="flex h-12 items-center gap-6 border-b last:border-0"
          >
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-4 w-24" />
            <Skeleton className="ml-auto h-4 w-16" />
          </div>
        ))}
      </div>
    </div>
  )
}
