/** What the owner's agents may do with each connected account: the switches
 * the provider's bundle marks `access: write` (decision record 0255), one row
 * per account and switch. Each says whether the switch is off, allowed, or
 * waiting for the provider's approval because the account's sign-in predates
 * it, and offers the one verb that moves it on: Turn on, Reconnect, or Turn
 * off. An account connected before the switch existed reads Off here, and
 * one turned on since without a reconnect reads Needs your approval. */

import { useMutation, useQueryClient } from "@tanstack/react-query"

import { IdText } from "@/components/identity/id-text"
import { ConnectButton } from "@/components/providers/account-actions"
import { ToneText } from "@/components/providers/provider-marks"
import type { ProviderEntry } from "@/components/providers/use-providers"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { splitKind } from "@/lib/api/http"
import { patchRecord } from "@/lib/api/records"
import type { BundleFeature } from "@/lib/api/types"
import {
  permissionState,
  type PermissionState,
  type WordTone,
} from "@/lib/providers"
import { labelAtStart, type AccountView } from "@/lib/sync"

const STATE_WORDS: Record<PermissionState, { text: string; tone: WordTone }> = {
  off: { text: "Off", tone: "muted" },
  approve: { text: "Needs your approval", tone: "warn" },
  allowed: { text: "Allowed", tone: "ok" },
  unconnected: { text: "On, once the account is connected", tone: "muted" },
}

export function AgentAccessSection({ entry }: { entry: ProviderEntry }) {
  const [technical] = useTechnicalDetails()
  const { view, permissions, info } = entry
  if (!view || permissions.length === 0 || view.accounts.length === 0) {
    return null
  }
  const features = new Map(
    (view.status?.features ?? []).map((f) => [f.toggle, f])
  )
  return (
    <div
      data-slot="agent-access"
      className="overflow-x-auto rounded-[10px] border"
    >
      <table className="w-full min-w-[560px] text-[13px]">
        <thead>
          <tr className="border-b text-left text-xs text-faint">
            <th className="px-3 py-2 font-medium">Account</th>
            <th className="px-3 py-2 font-medium">Permission</th>
            <th className="px-3 py-2 font-medium">Status</th>
            <th className="px-3 py-2">
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {view.accounts.flatMap((account) => {
            const listed = permissions.flatMap((p) => {
              const feature = features.get(p.name)
              return feature ? [{ permission: p, feature }] : []
            })
            return listed.map(({ permission, feature }, i) => (
              <PermissionRow
                key={`${account.record.id}|${permission.name}`}
                account={account}
                // The account is named once, on its first row, across all
                // of its permissions.
                accountRows={i === 0 ? listed.length : 0}
                permission={permission}
                feature={feature}
                oauth={view.oauth}
                configured={view.configured}
                providerName={info.name}
                technical={technical}
              />
            ))
          })}
        </tbody>
      </table>
    </div>
  )
}

function PermissionRow({
  account,
  accountRows,
  permission,
  feature,
  oauth,
  configured,
  providerName,
  technical,
}: {
  account: AccountView
  /** How many rows the account's cell spans, on its first row; 0 on the
   * rows after it, which draw no account cell. */
  accountRows: number
  permission: { name: string; label: string; description?: string }
  feature: BundleFeature
  oauth: boolean
  configured: boolean
  providerName: string
  technical: boolean
}) {
  const queryClient = useQueryClient()
  const state = permissionState(account, feature, oauth)
  const words = STATE_WORDS[state]
  const turn = useMutation({
    mutationFn: (on: boolean) => {
      const { authority, pkg, name } = splitKind(account.record.kind)
      return patchRecord(authority, pkg, name, account.record.id, {
        properties: { [permission.name]: on },
      })
    },
    onSuccess: (_, on) => {
      void queryClient.invalidateQueries()
      toast.add(
        on
          ? {
              type: "success",
              title: `${labelAtStart(permission.label)} is on for ${account.label}`,
              description: oauth
                ? `Reconnect the account so ${providerName} can ask you to approve it.`
                : undefined,
            }
          : {
              type: "success",
              title: `${labelAtStart(permission.label)} is off for ${account.label}`,
              description: "Your agents stop using it now.",
            }
      )
    },
    onError: (error, on) =>
      toast.add({
        type: "error",
        title: on
          ? `Couldn’t turn on ${permission.label}`
          : `Couldn’t turn off ${permission.label}`,
        description: error.message,
      }),
  })
  return (
    <tr
      data-slot="permission-row"
      data-state={state}
      className="border-b align-top last:border-0"
    >
      {accountRows > 0 && (
        <td
          rowSpan={accountRows}
          className="border-r px-3 py-2.5 font-medium whitespace-nowrap"
        >
          {account.label}
        </td>
      )}
      <td className="px-3 py-2.5">
        <div>{permission.label}</div>
        {permission.description && (
          <div className="text-xs text-muted-foreground">
            {permission.description}
          </div>
        )}
        {technical && (
          <div className="mt-1 flex flex-col gap-0.5">
            {feature.scopes.map((scope) => (
              <IdText key={scope} value={scope} />
            ))}
          </div>
        )}
      </td>
      <td className="px-3 py-2.5 whitespace-nowrap">
        <ToneText tone={words.tone}>{words.text}</ToneText>
      </td>
      <td className="px-3 py-2">
        <div className="flex items-center justify-end gap-1">
          {state === "approve" && (
            <ConnectButton
              view={account}
              providerName={providerName}
              disabled={!configured}
              reconnect
            />
          )}
          {state === "off" && (
            <Button
              variant="outline"
              size="sm"
              disabled={turn.isPending}
              onClick={() => turn.mutate(true)}
            >
              {turn.isPending && <Spinner className="size-3" />}
              Turn on
            </Button>
          )}
          {(state === "allowed" ||
            state === "approve" ||
            state === "unconnected") && (
            <Button
              variant="ghost"
              size="sm"
              disabled={turn.isPending}
              onClick={() => turn.mutate(false)}
            >
              {turn.isPending && <Spinner className="size-3" />}
              Turn off
            </Button>
          )}
        </div>
      </td>
    </tr>
  )
}
