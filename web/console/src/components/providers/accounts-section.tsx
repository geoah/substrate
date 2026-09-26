/** A provider's connected accounts, one row each: what it is, how its sync
 * is doing in plain words, when it last synced and how often it does, and
 * the verbs an account takes (connect, sync now, pause, change what it
 * brings in, disconnect). Technical mode adds each account's id and its
 * parked deliveries. The row the address names (`?account=`) is scrolled to
 * and highlighted. */

import { useEffect, useMemo, useRef } from "react"
import { useQuery } from "@tanstack/react-query"

import { IdText } from "@/components/identity/id-text"
import {
  AccountMenu,
  ConnectButton,
} from "@/components/providers/account-actions"
import { ToneText } from "@/components/providers/provider-marks"
import { PauseButton, SyncNowButton } from "@/components/sync/sync-panel"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  syncStatusesQueryOptions,
  triggerRecordsQueryOptions,
  triggerStatusesQueryOptions,
} from "@/lib/api/sync"
import { relativeTime } from "@/lib/format"
import { connectionWords, enumLabel, syncWords } from "@/lib/providers"
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
  const triggers = useQuery(triggerRecordsQueryOptions)
  const triggerStatuses = useQuery(triggerStatusesQueryOptions)
  const syncStatuses = useQuery(syncStatusesQueryOptions)
  const parkedOf = useMemo(
    () =>
      new Map(
        (syncStatuses.data ?? []).map((s) => [`${s.kind}|${s.id}`, s.parked])
      ),
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
            return (
              <AccountRow
                key={account.record.id}
                account={account}
                view={view}
                providerName={providerName}
                technical={technical}
                highlighted={highlight === account.record.id}
                requestTriggerIds={requestTriggers(sources).map((s) => s.id)}
                parked={
                  parkedOf.get(`${account.record.kind}|${account.record.id}`) ??
                  totals.parked
                }
                lag={totals.lag}
                connectBlocked={connectBlocked}
              />
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function AccountRow({
  account,
  view,
  providerName,
  technical,
  highlighted,
  requestTriggerIds,
  parked,
  lag,
  connectBlocked,
}: {
  account: AccountView
  view: ProviderView
  providerName: string
  technical: boolean
  highlighted: boolean
  requestTriggerIds: string[]
  parked: number
  lag: number
  connectBlocked: boolean
}) {
  const ref = useRef<HTMLTableRowElement>(null)
  useEffect(() => {
    if (highlighted) ref.current?.scrollIntoView?.({ block: "center" })
  }, [highlighted])
  const connected = !view.oauth || account.tokenStatus === "connected"
  const words = connected
    ? syncWords(
        {
          ...account.sync,
          message: account.sync.message ?? account.legacySyncStatus,
        },
        providerName
      )
    : connectionWords(account.tokenStatus, providerName)
  const every = enumLabel(account.kind, "syncFrequency", account.syncFrequency)
  const at = account.sync.lastSyncedAt
  return (
    <tr
      ref={ref}
      id={`account-${account.record.id}`}
      data-slot="account-row"
      data-highlighted={highlighted || undefined}
      className={cn(
        "border-b align-top last:border-0",
        highlighted && "bg-primary-soft"
      )}
    >
      <td className="px-3 py-2.5">
        <div className="font-medium [overflow-wrap:anywhere]">
          {account.label}
        </div>
        {technical && account.label !== account.record.id && (
          <IdText value={account.record.id} copy />
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
              words.tone === "muted" && "bg-faint"
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
  )
}
