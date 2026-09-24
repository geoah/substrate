/** Connections (`/connections`): the operations surface over every provider
 * account, read from the native `accountconfig` records the provider bundles
 * ship and the core `sync` trait they bind (decision 0085). Two halves: the
 * PROVIDERS — every installed bundle whose closure declares an account kind,
 * plus every bundle the catalog calls a provider — each card numbering the
 * two things a person does (set the credentials, add an account) and saying
 * which comes next; and the ACCOUNTS, one row per record across all
 * providers, with the token, the sync, the last run, the cadence and one
 * health dot, and the four verbs a Connection takes (Connect or Reconnect,
 * Sync now, Edit, Disconnect). Adding an account is one dialog that asks
 * only what the owner decides and, on an OAuth provider, creates the record
 * and opens the consent in one press (`AccountDialog`). Every read is an
 * existing route, and the change feed keeps the page live. */

import { useMemo, useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import {
  ArrowUpRightIcon,
  EllipsisIcon,
  KeyRoundIcon,
  PauseIcon,
  PencilIcon,
  PlayIcon,
  PlugZapIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react"

import type { DataTableColumn } from "@/components/data-table/data-table"
import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { BundleStateBadge, SetupBadge } from "@/components/bundle-state-badge"
import { AccountDialog } from "@/components/sync/account-dialog"
import { CredentialsDialog } from "@/components/sync/credentials-dialog"
import {
  HealthDot,
  SyncNowButton,
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
import {
  deleteRecord,
  setSyncPaused,
  syncStatusesQueryOptions,
  triggerRecordsQueryOptions,
  triggerStatusesQueryOptions,
} from "@/lib/api/sync"
import { relativeTime } from "@/lib/format"
import { recordPath } from "@/lib/record-path"
import {
  accountViewOf,
  countPhrase,
  providerNextStep,
  providerViews,
  requestTriggers,
  statusesOnKind,
  triggersOnKind,
  triggerTotals,
  type AccountView,
  type ProviderNextStep,
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
  // The status read is what knows each record's OWN parked deliveries; the
  // trigger statuses count every record's.
  const syncStatuses = useQuery(syncStatusesQueryOptions)

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
    syncStatuses,
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

/** One step of the card: its number, what it is, and the facts and the door
 * beside it. Done steps wear a filled number so the eye finds the open one. */
function StepRow({
  n,
  done,
  label,
  children,
}: {
  n: number
  done: boolean
  label: string
  children: React.ReactNode
}) {
  return (
    <li className="grid grid-cols-[1.25rem_6rem_minmax(0,1fr)] items-start gap-x-2 gap-y-1">
      <span
        aria-hidden
        className={
          done
            ? "mt-px inline-flex size-4 items-center justify-center rounded-full bg-primary text-[0.65rem] font-medium text-primary-foreground"
            : "mt-px inline-flex size-4 items-center justify-center rounded-full border text-[0.65rem] font-medium"
        }
      >
        {n}
      </span>
      <span className="pt-px text-muted-foreground">{label}</span>
      <span className="flex flex-wrap items-center gap-2">{children}</span>
    </li>
  )
}

/** The one sentence a person reads to know what to do on this provider, and
 * the button that does it: enable the bundle, set the credentials, add an
 * account, connect the one that is waiting, or nothing. */
function NextStep({
  provider,
  next,
  onSetUp,
  onAdd,
}: {
  provider: ProviderView
  next: ProviderNextStep
  onSetUp: () => void
  onAdd: () => void
}) {
  const name = provider.name
  let text: React.ReactNode
  let action: React.ReactNode
  switch (next.step) {
    case "install":
      text = `${name} is ${provider.status?.installed ? "disabled" : "not installed"}. Enable it in the Registry before adding accounts.`
      action = (
        <Button
          variant="outline"
          size="sm"
          className="h-7 gap-1 px-2 text-xs"
          render={<Link to="/registry/$id" params={{ id: provider.id }} />}
        >
          Open in the Registry
          <ArrowUpRightIcon className="size-3" />
        </Button>
      )
      break
    case "credentials":
      text = provider.oauth
        ? `Create an OAuth client with ${name} and paste its client ID and secret here. Every account you add connects through it.`
        : `Paste the token or key you created with ${name}. Every account you add uses it.`
      action = (
        <Button size="sm" className="h-7 gap-1 px-2 text-xs" onClick={onSetUp}>
          <KeyRoundIcon className="size-3" />
          Set up credentials
        </Button>
      )
      break
    case "account":
      text = provider.oauth
        ? `Add an account: choose what to sync, then approve access with ${name} in a new tab.`
        : "Add an account and choose what to sync. It starts syncing on its own."
      action = (
        <Button size="sm" className="h-7 gap-1 px-2 text-xs" onClick={onAdd}>
          <PlusIcon className="size-3" />
          Add account
        </Button>
      )
      break
    case "connect":
      text = `${next.account?.label} is not connected yet. Approve access with ${name} to start syncing.`
      action = next.account && (
        <ConnectButton view={next.account} disabled={false} />
      )
      break
    case "none":
      return (
        <p className="rounded-md bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
          This provider has no accounts to connect.
        </p>
      )
    default:
      return (
        <p className="rounded-md bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
          {provider.accounts.length === 1
            ? "Connected and syncing."
            : "All accounts are connected and syncing."}
        </p>
      )
  }
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 rounded-md bg-muted/40 px-3 py-2 text-xs">
      <p className="min-w-0 flex-1">
        <span className="font-medium">Next: </span>
        {text}
      </p>
      {action}
    </div>
  )
}

function ProviderCard({ provider }: { provider: ProviderView }) {
  const [editing, setEditing] = useState(false)
  const [adding, setAdding] = useState(false)
  // The card owns the connect so the consent's return is still heard after
  // the Add account dialog that started it has closed.
  const connect = useOAuthConnect()
  const status = provider.status
  const next = providerNextStep(provider)
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
      <ol className="flex flex-col gap-2 text-xs">
        <StepRow
          n={1}
          label="Credentials"
          done={!provider.configKind || provider.configured}
        >
          <CredentialsBadge provider={provider} />
          {provider.configKind ? (
            <Button
              variant="ghost"
              size="sm"
              className="h-6 gap-1 px-1.5 text-xs"
              onClick={() => setEditing(true)}
            >
              <PencilIcon className="size-3" />
              {provider.configured ? "Edit" : "Set up"}
            </Button>
          ) : (
            <span className="text-muted-foreground">none needed</span>
          )}
        </StepRow>
        <StepRow n={2} label="Accounts" done={provider.accounts.length > 0}>
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
        </StepRow>
      </ol>
      <NextStep
        provider={provider}
        next={next}
        onSetUp={() => setEditing(true)}
        onAdd={() => setAdding(true)}
      />
      {editing && (
        <CredentialsDialog
          provider={provider}
          open={editing}
          onOpenChange={setEditing}
        />
      )}
      {adding && provider.accountKind && (
        <AccountDialog
          kind={provider.accountKind}
          providerName={provider.name}
          oauth={provider.oauth}
          configured={provider.configured}
          connect={provider.oauth ? connect : undefined}
          onSetUpCredentials={
            provider.configKind
              ? () => {
                  setAdding(false)
                  setEditing(true)
                }
              : undefined
          }
          open={adding}
          onOpenChange={setAdding}
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
  const connect = useOAuthConnect(
    recordPath(view.record.kind, view.record.id),
    view.label
  )
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

/** Pause or Resume, Edit and Disconnect, behind the row's menu. */
function RowMenu({
  view,
  provider,
}: {
  view: AccountView
  provider?: ProviderView
}) {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [removing, setRemoving] = useState(false)
  const pause = useMutation({
    mutationFn: () => setSyncPaused(view.record, !view.sync.paused),
    onSuccess: () => {
      toast.add({
        type: "success",
        title: view.sync.paused ? "Sync resumed" : "Sync paused",
      })
      void queryClient.invalidateQueries({ queryKey: ["trait", "records"] })
      void queryClient.invalidateQueries({ queryKey: ["sync"] })
    },
    onError: (error) =>
      toast.add({
        type: "error",
        title: view.sync.paused ? "Resume failed" : "Pause failed",
        description: error.message,
      }),
  })
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
          {view.syncable && (
            <DropdownMenuItem
              disabled={pause.isPending}
              onClick={() => pause.mutate()}
            >
              {view.sync.paused ? <PlayIcon /> : <PauseIcon />}
              {view.sync.paused ? "Resume sync" : "Pause sync"}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem
            disabled={!view.kind}
            onClick={() => setEditing(true)}
          >
            <PencilIcon /> Edit what it syncs
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
        <AccountDialog
          kind={view.kind}
          providerName={provider?.name ?? view.provider}
          oauth={provider?.oauth ?? false}
          configured={provider?.configured ?? true}
          record={view.record}
          label={view.label}
          open={editing}
          onOpenChange={setEditing}
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
  provider?: ProviderView
  providerName: string
  /** The provider connects through consent; a token provider has no
   * Connect. */
  oauth: boolean
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
      meta: { label: "health", width: 36 },
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
      meta: { label: "provider", size: { min: 120, max: 200, weight: 0.7 } },
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
      meta: { label: "account", size: { min: 150, max: 300, weight: 1.1 } },
    },
    {
      id: "token",
      accessorFn: (r) => r.view.tokenStatus ?? "",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="token" />
      ),
      cell: ({ row }) => <TokenCell view={row.original.view} />,
      meta: { label: "token", width: 104 },
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
      meta: { label: "sync", size: { min: 170, max: 400, weight: 1.6 } },
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
      meta: { label: "last synced", width: 96 },
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
      meta: { label: "frequency", width: 82 },
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
      meta: { label: "backfill", width: 84 },
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
      meta: { label: "deliveries", width: 104 },
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
            {r.oauth && (
              <ConnectButton view={r.view} disabled={r.connectBlocked} />
            )}
            {r.view.syncable && (
              <SyncNowButton
                record={r.view.record}
                paused={r.view.sync.paused}
                requestTriggerIds={r.requestTriggerIds}
              />
            )}
            <RowMenu view={r.view} provider={r.provider} />
          </div>
        )
      },
      meta: { label: "actions", width: 212 },
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
    const parkedOf = new Map(
      (c.syncStatuses.data ?? []).map((s) => [`${s.kind}|${s.id}`, s.parked])
    )
    return c.views.map((view) => {
      const sources = triggersOnKind(c.triggers.data ?? [], view.record.kind)
      const totals = triggerTotals(
        statusesOnKind(c.triggerStatuses.data ?? [], sources)
      )
      const provider = c.providers.find((p) => p.id === view.provider)
      return {
        view,
        provider,
        providerName: names.get(view.provider) ?? view.provider,
        oauth: provider?.oauth ?? true,
        requestTriggerIds: requestTriggers(sources).map((s) => s.id),
        parked:
          parkedOf.get(`${view.record.kind}|${view.record.id}`) ??
          totals.parked,
        lag: totals.lag,
        connectBlocked: Boolean(
          provider &&
          (!provider.status?.installed ||
            !provider.status.enabled ||
            !provider.configured)
        ),
      }
    })
  }, [
    c.views,
    c.providers,
    c.triggers.data,
    c.triggerStatuses.data,
    c.syncStatuses.data,
  ])

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
            : `${c.providers.length} ${c.providers.length === 1 ? "provider" : "providers"}, ${rows.length} ${rows.length === 1 ? "account" : "accounts"}${broken > 0 ? `, ${broken} needing a hand` : ""}. This page updates on its own as accounts connect and sync.`}
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
                      Press Add account on a provider above. You choose what to
                      sync, then approve access with the provider in a new tab.
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
