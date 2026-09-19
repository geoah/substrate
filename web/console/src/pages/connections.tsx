/** Connections (`/connections`): the operations surface over every provider
 * account, read from the native `accountconfig` records the provider bundles
 * ship and the core `sync` trait they bind (decision 0085). Two halves: the
 * PROVIDERS — every installed bundle whose closure declares an account kind,
 * plus every bundle the catalog calls a provider — with whether its client
 * credentials are set and its accounts by token status; and the ACCOUNTS, one
 * row per record across all providers, with the token, the sync, the last
 * run, the cadence and one health dot, and the four verbs a Connection takes
 * (Connect or Reconnect, Sync now, Edit, Disconnect). Every read is an
 * existing route, and the change feed keeps the page live. */

import { useMemo, useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import {
  ArrowUpRightIcon,
  EllipsisIcon,
  KeyRoundIcon,
  PencilIcon,
  PlugZapIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react"

import type { DataTableColumn } from "@/components/data-table/data-table"
import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { BundleStateBadge, SetupBadge } from "@/components/bundle-state-badge"
import { RecordConfigForm } from "@/components/record-config-form"
import {
  HealthDot,
  SyncActions,
  SyncSummary,
} from "@/components/sync/sync-panel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { useLiveRecords } from "@/hooks/use-live-records"
import { useOAuthConnect } from "@/hooks/use-oauth-connect"
import {
  ACCOUNT_CONFIG_TRAIT,
  bundleState,
  bundleStatusesQueryOptions,
  traitRecordsQueryOptions,
} from "@/lib/api/bundles"
import { catalogQueryOptions } from "@/lib/api/catalog"
import { splitKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordQueryOptions } from "@/lib/api/records"
import {
  deleteRecord,
  triggerRecordsQueryOptions,
  triggerStatusesQueryOptions,
} from "@/lib/api/sync"
import { relativeTime } from "@/lib/format"
import {
  accountViewOf,
  countPhrase,
  providerViews,
  requestTriggers,
  statusesOnKind,
  triggersOnKind,
  triggerTotals,
  type AccountView,
  type ProviderView,
} from "@/lib/sync"

// ── the shared reads ─────────────────────────────────────────────────────────

/** Everything the page reads, in one hook, so the providers half and the
 * accounts half fold off the same cached answers. */
function useConnections() {
  const statuses = useQuery(bundleStatusesQueryOptions)
  const catalog = useQuery(catalogQueryOptions)
  const kinds = useQuery(kindsQueryOptions)
  const accounts = useQuery(traitRecordsQueryOptions(ACCOUNT_CONFIG_TRAIT))
  const triggers = useQuery(triggerRecordsQueryOptions)
  const triggerStatuses = useQuery(triggerStatusesQueryOptions)

  const views = useMemo(
    () =>
      (accounts.data?.records ?? []).map((r) =>
        accountViewOf(r, kinds.data ?? [])
      ),
    [accounts.data, kinds.data]
  )
  const providers = useMemo(() => {
    const tier = new Set(
      (catalog.data ?? [])
        .filter((c) => c.tier === "provider" && c.installed)
        .map((c) => c.id)
    )
    return providerViews(statuses.data ?? [], tier, kinds.data ?? [], views)
  }, [statuses.data, catalog.data, kinds.data, views])

  // The kinds the change feed watches: every account kind, plus the run
  // ledger, so a settled delivery re-reads the triggers half too.
  const liveKinds = useMemo(() => {
    const out = new Set<string>(["substrate.reamde.dev/core/triggerrun"])
    for (const p of providers)
      if (p.accountKind) out.add(p.accountKind.identity)
    for (const v of views) out.add(v.record.kind)
    return [...out]
  }, [providers, views])
  useLiveRecords(liveKinds, [
    ["trait", "records"],
    ["sync"],
    ["triggers"],
    ["record"],
    ["bundles", "status"],
  ])

  return {
    statuses,
    catalog,
    kinds,
    accounts,
    triggers,
    triggerStatuses,
    views,
    providers,
    pending: statuses.isPending || kinds.isPending || accounts.isPending,
    error: statuses.error ?? kinds.error ?? accounts.error ?? undefined,
    refetch: () => {
      void statuses.refetch()
      void kinds.refetch()
      void accounts.refetch()
    },
  }
}

// ── providers ────────────────────────────────────────────────────────────────

/** Credentials present or missing, off the bundle's setup list: what
 * "setup: 1 step" on the Registry row means for a provider. */
function CredentialsBadge({ provider }: { provider: ProviderView }) {
  if (!provider.configKind) return null
  return (
    <Badge
      variant="outline"
      className={
        provider.configured
          ? "gap-1 font-normal"
          : "gap-1 border-warning/40 font-normal text-warning"
      }
    >
      <KeyRoundIcon className="size-3 shrink-0" />
      <span className="data">
        {provider.configured ? "credentials set" : "credentials missing"}
      </span>
    </Badge>
  )
}

/** The config record's form: the `oauth2`-trait client (or a token
 * provider's config), edited through the ordinary record dialog, whose
 * secret inputs are write-only. The header says which secrets are SET,
 * read off the record (a stored secret reads back as its redaction
 * marker, never its value). */
function ConfigDialog({
  provider,
  open,
  onOpenChange,
}: {
  provider: ProviderView
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const kind = provider.configKind
  const parts = kind ? splitKind(kind.identity) : undefined
  const existing = useQuery({
    ...recordQueryOptions(
      parts?.authority ?? "",
      parts?.pkg ?? "",
      kind?.name ?? "",
      provider.configRecord ?? ""
    ),
    enabled: Boolean(kind && provider.configRecord && open),
  })
  if (!kind) return null
  const record = provider.configRecord ? existing.data : undefined
  const secrets = Object.entries(
    (kind.definition.properties ?? {}) as Record<string, { type?: string }>
  )
    .filter(([, p]) => p?.type === "secret")
    .map(([name]) => name)
  const setState = secrets.map((name) => {
    const v = record?.properties?.[name]
    return { name, set: v !== undefined && v !== null && v !== "" }
  })
  if (provider.configRecord && existing.isPending) {
    return null
  }
  return (
    <RecordConfigForm
      type={kind}
      record={record}
      open={open}
      onOpenChange={onOpenChange}
      title={
        record ? `Edit ${provider.name} credentials` : `Set up ${provider.name}`
      }
      description={
        <span className="flex flex-col gap-1">
          <span>
            The client this provider authenticates through. A secret is
            write-only: it never reads back, and a blank one keeps what is
            stored.
          </span>
          {setState.length > 0 && (
            <span className="flex flex-wrap gap-1.5 pt-1">
              {setState.map((s) => (
                <Badge
                  key={s.name}
                  variant="outline"
                  className={
                    s.set
                      ? "gap-1 font-normal"
                      : "gap-1 font-normal text-warning"
                  }
                >
                  <span className="data">{s.name}</span>
                  <span>{s.set ? "· set" : "· not set"}</span>
                </Badge>
              ))}
            </span>
          )}
        </span>
      }
    />
  )
}

function ProviderCard({ provider }: { provider: ProviderView }) {
  const [editing, setEditing] = useState(false)
  const [adding, setAdding] = useState(false)
  const status = provider.status
  return (
    <div className="flex flex-col gap-3 rounded-md border p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <Link
              to="/registry/$id"
              params={{ id: provider.id }}
              className="font-medium hover:underline"
            >
              {provider.name}
            </Link>
            {status && <BundleStateBadge state={bundleState(status)} />}
            {status && <SetupBadge count={provider.setupSteps} />}
          </div>
          <div className="data text-xs break-all text-muted-foreground">
            {provider.id}
          </div>
        </div>
        <PlugZapIcon className="size-4 shrink-0 text-muted-foreground" />
      </div>
      <dl className="grid grid-cols-[7rem_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-xs">
        <dt className="text-muted-foreground">Credentials</dt>
        <dd className="flex flex-wrap items-center gap-2">
          <CredentialsBadge provider={provider} />
          {provider.configKind && (
            <Button
              variant="ghost"
              size="sm"
              className="h-6 gap-1 px-1.5 text-xs"
              onClick={() => setEditing(true)}
            >
              <PencilIcon className="size-3" />
              {provider.configured ? "Edit" : "Set up"}
            </Button>
          )}
          {!provider.configKind && (
            <span className="text-muted-foreground">none declared</span>
          )}
        </dd>
        <dt className="text-muted-foreground">Accounts</dt>
        <dd className="flex flex-wrap items-center gap-2">
          <span className="data">
            {provider.accounts.length === 0
              ? "none"
              : countPhrase(provider.byTokenStatus)}
          </span>
          {provider.accountKind && (
            <Button
              variant="ghost"
              size="sm"
              className="h-6 gap-1 px-1.5 text-xs"
              onClick={() => setAdding(true)}
            >
              <PlusIcon className="size-3" />
              Add account
            </Button>
          )}
        </dd>
      </dl>
      {editing && (
        <ConfigDialog
          provider={provider}
          open={editing}
          onOpenChange={setEditing}
        />
      )}
      {adding && provider.accountKind && (
        <RecordConfigForm
          type={provider.accountKind}
          open={adding}
          onOpenChange={setAdding}
          title={`Add ${provider.name} account`}
          description="Create the account, then press Connect to approve it with the provider. What it syncs takes effect once it is connected."
        />
      )}
    </div>
  )
}

// ── accounts ─────────────────────────────────────────────────────────────────

/** The connect button and its confirm, on one row. */
function ConnectButton({
  view,
  disabled,
}: {
  view: AccountView
  disabled: boolean
}) {
  const [confirming, setConfirming] = useState(false)
  const connect = useOAuthConnect(view.record.id, view.label)
  const connected = view.tokenStatus === "connected"
  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className="h-7 px-2 text-xs"
        disabled={disabled || connect.isPending}
        onClick={(e) => {
          e.stopPropagation()
          setConfirming(true)
        }}
      >
        {connect.isPending && <Spinner className="size-3" />}
        {connected ? "Reconnect" : "Connect"}
      </Button>
      {confirming && (
        <Dialog
          open
          onOpenChange={(open) =>
            !open && !connect.isPending && setConfirming(false)
          }
        >
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>
                {connected ? "Reconnect" : "Connect"} {view.label}?
              </DialogTitle>
              <DialogDescription>
                This opens the provider in a new tab. Once you approve, this
                account starts syncing the data you turned on.
                {connected && " Reconnecting replaces the current approval."}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button
                variant="outline"
                disabled={connect.isPending}
                onClick={() => setConfirming(false)}
              >
                Cancel
              </Button>
              <Button
                disabled={connect.isPending}
                onClick={() =>
                  connect.mutate(undefined, {
                    onSettled: () => setConfirming(false),
                  })
                }
              >
                {connect.isPending && <Spinner className="size-3.5" />}
                {connected ? "Reconnect" : "Connect"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}

/** Edit and Disconnect, behind the row's menu. */
function RowMenu({ view }: { view: AccountView }) {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [removing, setRemoving] = useState(false)
  const disconnect = useMutation({
    mutationFn: () => deleteRecord(view.record),
    onSuccess: () => {
      toast.add({ type: "success", title: `${view.label} disconnected` })
      setRemoving(false)
      void queryClient.invalidateQueries({ queryKey: ["trait", "records"] })
      void queryClient.invalidateQueries({ queryKey: ["sync"] })
      void queryClient.invalidateQueries({ queryKey: ["bundles", "status"] })
    },
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Disconnect failed",
        description: error.message,
      }),
  })
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="sm"
              className="h-7 w-7 px-0"
              aria-label={`More actions for ${view.label}`}
              onClick={(e) => e.stopPropagation()}
            />
          }
        >
          <EllipsisIcon className="size-3.5" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            disabled={!view.kind}
            onClick={() => setEditing(true)}
          >
            <PencilIcon /> Edit toggles, frequency and depth
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setRemoving(true)}
          >
            <Trash2Icon /> Disconnect
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {view.kind && editing && (
        <RecordConfigForm
          type={view.kind}
          record={view.record}
          open={editing}
          onOpenChange={setEditing}
          title={`Edit ${view.label}`}
          description="Change what this account syncs, how often, and how far back. The connection itself is not edited here."
        />
      )}
      {removing && (
        <Dialog
          open
          onOpenChange={(open) =>
            !open && !disconnect.isPending && setRemoving(false)
          }
        >
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Disconnect {view.label}?</DialogTitle>
              <DialogDescription>
                The account record is deleted, its grant revoked with the
                provider where one is declared, and its stored credential
                dropped. The records it mirrored stay until the bundle is
                purged.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button
                variant="outline"
                disabled={disconnect.isPending}
                onClick={() => setRemoving(false)}
              >
                Cancel
              </Button>
              <Button
                variant="destructive"
                disabled={disconnect.isPending}
                onClick={() => disconnect.mutate()}
              >
                {disconnect.isPending && <Spinner className="size-3.5" />}
                Disconnect
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}

/** The token status, with the granted scopes on hover. */
function TokenCell({ view }: { view: AccountView }) {
  const status = view.tokenStatus ?? "not connected"
  const tone =
    view.tokenStatus === "connected"
      ? "text-foreground"
      : view.tokenStatus === "erroring"
        ? "text-destructive"
        : "text-muted-foreground"
  const chip = <span className={`data text-xs ${tone}`}>{status}</span>
  if (!view.grantedScopes.length) return chip
  return (
    <HoverCard>
      <HoverCardTrigger
        render={
          <span className="cursor-help underline decoration-dotted underline-offset-4" />
        }
      >
        {chip}
      </HoverCardTrigger>
      <HoverCardContent className="w-80 max-w-[calc(100vw-2rem)] overflow-hidden text-xs">
        <div className="mb-1 font-medium">Granted scopes</div>
        <ul className="flex flex-col gap-0.5">
          {view.grantedScopes.map((s) => (
            <li key={s} className="data break-all text-muted-foreground">
              {s}
            </li>
          ))}
        </ul>
      </HoverCardContent>
    </HoverCard>
  )
}

interface AccountRow {
  view: AccountView
  providerName: string
  /** The on-request triggers on the kind, for Sync now. */
  requestTriggerIds: string[]
  parked: number
  lag: number
  connectBlocked: boolean
}

function accountColumns(): DataTableColumn<AccountRow>[] {
  return [
    {
      id: "health",
      accessorFn: (r) => r.view.health,
      enableSorting: true,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="" />
      ),
      cell: ({ row }) => <HealthDot health={row.original.view.health} />,
      meta: { label: "health", width: 44 },
    },
    {
      id: "provider",
      accessorFn: (r) => r.providerName,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="provider" />
      ),
      cell: ({ row }) => (
        <div className="min-w-0">
          <div className="truncate font-medium">
            {row.original.providerName}
          </div>
          <div
            className="truncate data text-xs text-muted-foreground"
            title={row.original.view.provider}
          >
            {row.original.view.provider}
          </div>
        </div>
      ),
      meta: { label: "provider", size: { min: 140, max: 260, weight: 1 } },
    },
    {
      id: "account",
      accessorFn: (r) => r.view.label,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="account" />
      ),
      cell: ({ row }) => {
        const v = row.original.view
        const parts = splitKind(v.record.kind)
        return (
          <div className="min-w-0">
            <Link
              to="/connections/$authority/$pkg/$name/$id"
              params={{ ...parts, id: v.record.id }}
              className="block truncate font-medium hover:underline"
              onClick={(e) => e.stopPropagation()}
            >
              {v.label}
            </Link>
            <div
              className="truncate data text-xs text-muted-foreground"
              title={v.record.id}
            >
              {v.record.id}
            </div>
          </div>
        )
      },
      meta: { label: "account", size: { min: 180, max: 360, weight: 1.4 } },
    },
    {
      id: "token",
      accessorFn: (r) => r.view.tokenStatus ?? "",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="token" />
      ),
      cell: ({ row }) => <TokenCell view={row.original.view} />,
      meta: { label: "token", width: 120 },
    },
    {
      id: "sync",
      accessorFn: (r) => r.view.sync.state,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="sync" />
      ),
      cell: ({ row }) => (
        <SyncSummary
          fields={row.original.view.sync}
          legacyStatus={row.original.view.legacySyncStatus}
          compact
        />
      ),
      meta: { label: "sync", size: { min: 200, max: 480, weight: 1.6 } },
    },
    {
      id: "lastSynced",
      accessorFn: (r) => r.view.sync.lastSyncedAt ?? "",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="last synced" />
      ),
      cell: ({ row }) => {
        const at = row.original.view.sync.lastSyncedAt
        return (
          <span className="text-xs text-muted-foreground" title={at}>
            {at ? relativeTime(at) : "never"}
          </span>
        )
      },
      meta: { label: "last synced", width: 110 },
    },
    {
      id: "frequency",
      accessorFn: (r) => r.view.syncFrequency ?? "",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="frequency" />
      ),
      cell: ({ row }) => (
        <span className="data text-xs">
          {row.original.view.syncFrequency ?? "—"}
        </span>
      ),
      meta: { label: "frequency", width: 96 },
    },
    {
      id: "depth",
      accessorFn: (r) => r.view.backfillDepth ?? "",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="backfill" />
      ),
      cell: ({ row }) => (
        <span className="data text-xs">
          {row.original.view.backfillDepth ?? "—"}
        </span>
      ),
      meta: { label: "backfill", width: 96 },
    },
    {
      id: "deliveries",
      accessorFn: (r) => r.parked,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="deliveries" />
      ),
      cell: ({ row }) => (
        <span className="text-xs">
          {row.original.parked > 0 ? (
            <span className="text-destructive">
              {row.original.parked} parked
            </span>
          ) : (
            <span className="text-muted-foreground">none parked</span>
          )}
          {row.original.lag > 0 && (
            <span className="text-muted-foreground">
              {" "}
              · {row.original.lag} behind
            </span>
          )}
        </span>
      ),
      meta: { label: "deliveries", width: 130 },
    },
    {
      id: "actions",
      enableSorting: false,
      enableHiding: false,
      header: () => <span className="sr-only">Actions</span>,
      cell: ({ row }) => {
        const r = row.original
        return (
          <div
            className="flex items-center justify-end gap-1"
            onClick={(e) => e.stopPropagation()}
          >
            <ConnectButton view={r.view} disabled={r.connectBlocked} />
            {r.view.syncable && (
              <SyncActions
                record={r.view.record}
                paused={r.view.sync.paused}
                requestTriggerIds={r.requestTriggerIds}
                size="xs"
              />
            )}
            <RowMenu view={r.view} />
          </div>
        )
      },
      meta: { label: "actions", width: 330 },
    },
  ]
}

// ── the page ─────────────────────────────────────────────────────────────────

function LoadFailed({
  message,
  onRetry,
}: {
  message: string
  onRetry: () => void
}) {
  return (
    <Empty className="my-6 w-full rounded-md border py-10">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <PlugZapIcon />
        </EmptyMedia>
        <EmptyTitle>The connections didn't load</EmptyTitle>
        <EmptyDescription>{message}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" size="sm" onClick={onRetry}>
          Retry
        </Button>
      </EmptyContent>
    </Empty>
  )
}

export function ConnectionsPage() {
  const navigate = useNavigate()
  const c = useConnections()

  const rows = useMemo<AccountRow[]>(() => {
    const names = new Map(c.providers.map((p) => [p.id, p.name]))
    return c.views.map((view) => {
      const sources = triggersOnKind(c.triggers.data ?? [], view.record.kind)
      const totals = triggerTotals(
        statusesOnKind(c.triggerStatuses.data ?? [], sources)
      )
      const provider = c.providers.find((p) => p.id === view.provider)
      return {
        view,
        providerName: names.get(view.provider) ?? view.provider,
        requestTriggerIds: requestTriggers(sources).map((s) => s.id),
        parked: totals.parked,
        lag: totals.lag,
        connectBlocked: Boolean(
          provider &&
          (!provider.status?.installed ||
            !provider.status.enabled ||
            !provider.configured)
        ),
      }
    })
  }, [c.views, c.providers, c.triggers.data, c.triggerStatuses.data])

  const columns = useMemo(() => accountColumns(), [])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => `${row.view.record.kind}|${row.view.record.id}`,
    prefsKey: "connections",
  })

  const broken = rows.filter((r) => r.view.health === "broken").length

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <div className="shrink-0 px-6 pt-5 pb-2">
        <h1 className="text-2xl font-semibold tracking-tight">Connections</h1>
        <p className="text-xs text-muted-foreground">
          {c.pending
            ? "Reading the providers and their accounts…"
            : `${c.providers.length} ${c.providers.length === 1 ? "provider" : "providers"}, ${rows.length} ${rows.length === 1 ? "account" : "accounts"}${broken > 0 ? `, ${broken} needing a hand` : ""}. The page follows the change feed.`}
        </p>
      </div>

      {c.error ? (
        <div className="flex flex-1 px-6">
          <LoadFailed message={c.error.message} onRetry={c.refetch} />
        </div>
      ) : (
        <>
          <section className="flex flex-col gap-2 px-6 pb-4">
            <h2 className="text-sm font-medium">Providers</h2>
            {c.pending ? (
              <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {Array.from({ length: 3 }, (_, i) => (
                  <Skeleton key={i} className="h-32 rounded-md" />
                ))}
              </div>
            ) : c.providers.length === 0 ? (
              <Empty className="rounded-md border py-8">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <PlugZapIcon />
                  </EmptyMedia>
                  <EmptyTitle>No providers installed</EmptyTitle>
                  <EmptyDescription>
                    Install one from the Registry; its accounts appear here.
                  </EmptyDescription>
                </EmptyHeader>
                <EmptyContent>
                  <Button
                    variant="outline"
                    size="sm"
                    render={<Link to="/registry" />}
                  >
                    Open the Registry
                    <ArrowUpRightIcon className="size-3.5" />
                  </Button>
                </EmptyContent>
              </Empty>
            ) : (
              <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {c.providers.map((p) => (
                  <ProviderCard key={p.id} provider={p} />
                ))}
              </div>
            )}
          </section>

          <section className="flex flex-col">
            <div className="flex items-center justify-between px-6">
              <h2 className="text-sm font-medium">Accounts</h2>
              <DataTableViewOptions table={table} />
            </div>
            {c.accounts.data?.capped && (
              <p className="mx-6 mt-2 rounded-md border bg-muted/40 px-4 py-2 text-xs text-muted-foreground">
                Showing the first accounts only. Open an account kind under Data
                to browse them all.
              </p>
            )}
            <DataTable
              table={table}
              loading={c.pending}
              onRowClick={(row) => {
                const parts = splitKind(row.view.record.kind)
                void navigate({
                  to: "/connections/$authority/$pkg/$name/$id",
                  params: { ...parts, id: row.view.record.id },
                })
              }}
              empty={
                <Empty className="py-12">
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <PlugZapIcon />
                    </EmptyMedia>
                    <EmptyTitle>No accounts yet</EmptyTitle>
                    <EmptyDescription>
                      Add an account on a provider above, then connect it.
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              }
            />
          </section>
        </>
      )}
    </div>
  )
}
