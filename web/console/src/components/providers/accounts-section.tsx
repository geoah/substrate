/** A provider's connected accounts, one row each: what it is, how its sync
 * is doing in plain words, when it last synced and how often it does, and
 * the verbs an account takes (connect, sync now, pause, change what it
 * brings in, disconnect). Each account links to its own page, and where its
 * kind has record views (decision record 0166) the row opens onto them, the
 * sections its page draws, so a provider shows what it knows of an account
 * (its connection, its warnings, what it runs on) as data it ships. Technical
 * mode adds each account's id and its parked deliveries. The row the
 * address names (`?account=`) is scrolled to, highlighted and opened. */

import { Fragment, useEffect, useMemo, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ChevronRightIcon } from "lucide-react"

import { IdText } from "@/components/identity/id-text"
import {
  AccountMenu,
  ConnectButton,
} from "@/components/providers/account-actions"
import { ToneText } from "@/components/providers/provider-marks"
import { RecordViews } from "@/components/record-view/record-views"
import { PauseButton, SyncNowButton } from "@/components/sync/sync-panel"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useNow } from "@/hooks/use-now"
import { scrollIntoPage } from "@/hooks/use-page-scroll"
import { useRecordViews } from "@/hooks/use-record-views"
import { splitKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import {
  syncStatusesQueryOptions,
  triggerRecordsQueryOptions,
  triggerStatusesQueryOptions,
} from "@/lib/api/sync"
import type { KindInfo } from "@/lib/api/types"
import { relativeTime } from "@/lib/format"
import { connectionWords, enumLabel, syncWords } from "@/lib/providers"
import type { RecordView } from "@/lib/record-view"
import {
  requestTriggers,
  statusesOnKind,
  triggersOnKind,
  triggerTotals,
  type AccountView,
  type ProviderView,
} from "@/lib/sync"
import { cn } from "@/lib/utils"

export function AccountsSection({
  view,
  providerName,
  highlight,
}: {
  view: ProviderView
  providerName: string
  /** The account id the address names. */
  highlight?: string
}) {
  const [technical] = useTechnicalDetails()
  const registry = useQuery(kindsQueryOptions)
  const views = useRecordViews(view.accountKind)
  const triggers = useQuery(triggerRecordsQueryOptions)
  const triggerStatuses = useQuery(triggerStatusesQueryOptions)
  const syncStatuses = useQuery(syncStatusesQueryOptions)
  const syncOf = useMemo(
    () =>
      new Map((syncStatuses.data ?? []).map((s) => [`${s.kind}|${s.id}`, s])),
    [syncStatuses.data]
  )
  // Connecting is refused by the server while the provider is paused or has
  // no sign-in details; the button says so before the server does.
  const connectBlocked =
    !view.status?.installed || !view.status.enabled || !view.configured

  return (
    <div className="overflow-x-auto rounded-[10px] border">
      <table className="w-full min-w-[640px] text-[13px]">
        <thead>
          <tr className="border-b text-left text-xs text-faint">
            <th className="px-3 py-2 font-medium">Account</th>
            <th className="px-3 py-2 font-medium">How it’s doing</th>
            <th className="px-3 py-2 font-medium">Last synced</th>
            <th className="px-3 py-2 font-medium">How often</th>
            {technical && <th className="px-3 py-2 font-medium">Deliveries</th>}
            <th className="px-3 py-2">
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {view.accounts.map((account) => {
            const sources = triggersOnKind(
              triggers.data ?? [],
              account.record.kind
            )
            const totals = triggerTotals(
              statusesOnKind(triggerStatuses.data ?? [], sources)
            )
            const sync = syncOf.get(
              `${account.record.kind}|${account.record.id}`
            )
            return (
              <AccountRow
                key={account.record.id}
                account={account}
                view={view}
                providerName={providerName}
                technical={technical}
                highlighted={highlight === account.record.id}
                requestTriggerIds={requestTriggers(sources).map((s) => s.id)}
                parked={sync?.parked ?? totals.parked}
                lastParked={
                  sync?.lastParkedError
                    ? { error: sync.lastParkedError, at: sync.lastParkedAt }
                    : undefined
                }
                lag={totals.lag}
                connectBlocked={connectBlocked}
                views={views}
                kinds={registry.data ?? NO_KINDS}
              />
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

const NO_KINDS: KindInfo[] = []

function AccountRow({
  account,
  view,
  providerName,
  technical,
  highlighted,
  requestTriggerIds,
  parked,
  lastParked,
  lag,
  connectBlocked,
  views,
  kinds,
}: {
  account: AccountView
  view: ProviderView
  providerName: string
  technical: boolean
  highlighted: boolean
  requestTriggerIds: string[]
  parked: number
  /** The newest parked run of the account's sync: its error and when. */
  lastParked?: { error: string; at?: string }
  lag: number
  connectBlocked: boolean
  /** The account kind's record views: the details the row opens onto. */
  views: readonly RecordView[]
  kinds: KindInfo[]
}) {
  const ref = useRef<HTMLTableRowElement>(null)
  const [open, setOpen] = useState(highlighted)
  const [wasHighlighted, setWasHighlighted] = useState(highlighted)
  if (highlighted !== wasHighlighted) {
    setWasHighlighted(highlighted)
    if (highlighted) setOpen(true)
  }
  const { authority, pkg, name } = splitKind(account.record.kind)
  const detailsId = `account-details-${account.record.id}`
  const columns = technical ? 6 : 5
  useEffect(() => {
    if (highlighted) scrollIntoPage(ref.current, { block: "center" })
  }, [highlighted])
  // A waited-on request turns into "hasn’t started" after a quarter hour;
  // the minute tick shows that without waiting for the next refetch.
  const now = useNow(60_000)
  const connected = !view.oauth || account.tokenStatus === "connected"
  const words = connected
    ? syncWords(
        {
          ...account.sync,
          message: account.sync.message ?? account.legacySyncStatus,
        },
        providerName,
        now
      )
    : connectionWords(account.tokenStatus, providerName, account.tokenError)
  const every = enumLabel(account.kind, "syncFrequency", account.syncFrequency)
  const at = account.sync.lastSyncedAt
  return (
    <Fragment>
      <tr
        ref={ref}
        id={`account-${account.record.id}`}
        data-slot="account-row"
        data-highlighted={highlighted || undefined}
        className={cn(
          "border-b align-top last:border-0",
          open && views.length > 0 && "border-b-0",
          highlighted && "bg-primary-soft"
        )}
      >
        <td className="px-3 py-2.5">
          <div className="font-medium [overflow-wrap:anywhere]">
            <Link
              to="/data/$authority/$pkg/$name/$id"
              params={{ authority, pkg, name, id: account.record.id }}
              className="underline-offset-2 hover:underline"
            >
              {account.label}
            </Link>
          </div>
          {technical && account.label !== account.record.id && (
            <IdText value={account.record.id} copy />
          )}
          {views.length > 0 && (
            <button
              type="button"
              aria-expanded={open}
              aria-controls={detailsId}
              className="mt-0.5 inline-flex cursor-pointer items-center gap-0.5 text-[12px] text-muted-foreground hover:text-foreground"
              onClick={() => setOpen((v) => !v)}
            >
              <ChevronRightIcon
                aria-hidden
                className={cn(
                  "size-3.5 transition-transform",
                  open && "rotate-90"
                )}
              />
              Details
            </button>
          )}
        </td>
        <td className="px-3 py-2.5">
          <span className="inline-flex items-start gap-1.5">
            <span
              aria-hidden
              className={cn(
                "mt-1.5 size-1.5 shrink-0 rounded-full",
                words.tone === "ok" && "bg-ok",
                words.tone === "active" && "animate-pulse bg-primary",
                words.tone === "warn" && "bg-warning",
                words.tone === "bad" && "bg-destructive",
                words.tone === "muted" && "bg-faint-deco"
              )}
            />
            <ToneText
              tone={words.tone === "muted" ? "muted" : words.tone}
              className="[overflow-wrap:anywhere]"
            >
              {words.text}
            </ToneText>
          </span>
        </td>
        <td className="px-3 py-2.5 whitespace-nowrap text-muted-foreground">
          <span title={at}>{at ? relativeTime(at) : "Never"}</span>
        </td>
        <td className="px-3 py-2.5 whitespace-nowrap text-muted-foreground">
          {every ?? "—"}
        </td>
        {technical && (
          <td className="px-3 py-2.5 whitespace-nowrap">
            {parked > 0 ? (
              <span className="text-destructive">{parked} parked</span>
            ) : (
              <span className="text-muted-foreground">None parked</span>
            )}
            {lag > 0 && (
              <span className="text-muted-foreground"> · {lag} behind</span>
            )}
            {parked > 0 && lastParked && (
              <div
                data-slot="account-parked-reason"
                className="mt-1 max-w-72 text-xs whitespace-normal"
              >
                <span className="text-faint">Latest error</span>
                {lastParked.at && (
                  <span className="text-faint" title={lastParked.at}>
                    {" "}
                    · {relativeTime(lastParked.at)}
                  </span>
                )}
                <span className="block break-words text-muted-foreground">
                  {lastParked.error}
                </span>
              </div>
            )}
          </td>
        )}
        <td className="px-3 py-2">
          <div className="flex items-center justify-end gap-1">
            {view.oauth && !connected && (
              <ConnectButton
                view={account}
                providerName={providerName}
                disabled={connectBlocked}
                reconnect={account.tokenStatus === "erroring"}
              />
            )}
            {account.syncable && connected && (
              <>
                <SyncNowButton
                  record={account.record}
                  paused={account.sync.paused}
                  requestTriggerIds={requestTriggerIds}
                />
                <PauseButton
                  record={account.record}
                  name={account.label}
                  paused={account.sync.paused}
                />
              </>
            )}
            <AccountMenu
              view={account}
              provider={view}
              providerName={providerName}
            />
          </div>
        </td>
      </tr>
      {open && views.length > 0 && (
        <tr
          id={detailsId}
          data-slot="account-details"
          className={cn(
            "border-b last:border-0",
            highlighted && "bg-primary-soft"
          )}
        >
          <td
            colSpan={columns}
            className="px-3 pb-4 [&>section:first-child>[data-slot=section-head]]:mt-1"
          >
            <RecordViews
              views={views}
              record={account.record}
              kind={account.kind}
              kinds={kinds}
            />
          </td>
        </tr>
      )}
    </Fragment>
  )
}
