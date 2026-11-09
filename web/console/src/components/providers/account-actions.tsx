/** What a person does to one connected account: connect or reconnect it
 * (the provider's consent, in a new tab), and, behind its menu, change what
 * it brings in or disconnect it. Sync now and Pause are the sync panel's.
 * Every verb that leaves the page or loses something asks first. */

import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  EllipsisIcon,
  PencilIcon,
  RefreshCwIcon,
  Trash2Icon,
} from "lucide-react"

import { AccountDialog } from "@/components/sync/account-dialog"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { useOAuthConnect, type OAuthConnect } from "@/hooks/use-oauth-connect"
import { useUnits } from "@/hooks/use-units"
import { accountFormGroups } from "@/lib/account-form"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { deleteRecord } from "@/lib/api/sync"
import { displayPlural } from "@/lib/kind-names"
import { andList } from "@/lib/providers"
import { recordPath } from "@/lib/record-path"
import {
  cascadingKinds,
  labelAtStart,
  type AccountView,
  type ProviderView,
} from "@/lib/sync"

/** The confirmation a connect asks: the provider opens in a new tab. The
 * caller owns the connect, so the consent's return is heard after this
 * closes. */
function ConnectConfirm({
  view,
  providerName,
  connect,
  reconnect,
  onClose,
}: {
  view: AccountView
  providerName: string
  connect: OAuthConnect
  reconnect: boolean
  onClose: () => void
}) {
  const verb = reconnect ? "Reconnect" : "Connect"
  return (
    <ConfirmDialog
      title={`${verb} ${view.label}?`}
      consequence={`${providerName} opens in a new tab and asks you to approve access. Once you do, this account starts bringing in what you turned on.${reconnect ? " Reconnecting replaces the current approval." : ""}`}
      confirm={verb}
      pending={connect.isPending}
      onConfirm={() => connect.mutate(undefined, { onSettled: onClose })}
      onClose={onClose}
    />
  )
}

/** Connect or Reconnect, with the confirmation that says a tab opens. */
export function ConnectButton({
  view,
  providerName,
  disabled,
  variant = "outline",
  reconnect = view.tokenStatus === "connected",
}: {
  view: AccountView
  providerName: string
  disabled?: boolean
  variant?: "outline" | "default"
  /** Says Reconnect: the account had an approval, working or not. */
  reconnect?: boolean
}) {
  const [confirming, setConfirming] = useState(false)
  const connect = useOAuthConnect(
    recordPath(view.record.kind, view.record.id),
    view.label,
    view.syncable ? view.sync : undefined
  )
  return (
    <>
      <Button
        variant={variant}
        size="sm"
        disabled={disabled || connect.isPending}
        onClick={() => setConfirming(true)}
      >
        {connect.isPending && <Spinner className="size-3" />}
        {reconnect ? "Reconnect" : "Connect"}
      </Button>
      {confirming && (
        <ConnectConfirm
          view={view}
          providerName={providerName}
          connect={connect}
          reconnect={reconnect}
          onClose={() => setConfirming(false)}
        />
      )}
    </>
  )
}

/** Change what it brings in, and Disconnect, behind the account's menu. */
export function AccountMenu({
  view,
  provider,
  providerName,
}: {
  view: AccountView
  provider: ProviderView | undefined
  providerName: string
}) {
  const queryClient = useQueryClient()
  const units = useUnits()
  const registry = useQuery(kindsQueryOptions)
  const [editing, setEditing] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [reconnecting, setReconnecting] = useState(false)
  // A kind with nothing to turn on has settings only.
  const brings = view.kind
    ? accountFormGroups(units, view.kind).toggles.length > 0
    : true
  // What deleting the account takes with it: the records whose reference
  // to it cascades, which its provider wrote from this account.
  const cascades = cascadingKinds(registry.data ?? [], view.kind)
  const connect = useOAuthConnect(
    recordPath(view.record.kind, view.record.id),
    view.label,
    view.syncable ? view.sync : undefined
  )
  const disconnect = useMutation({
    mutationFn: () => deleteRecord(view.record),
    onSuccess: () => {
      toast.add({
        type: "success",
        title: `${labelAtStart(view.label)} disconnected.`,
      })
      setRemoving(false)
      void queryClient.invalidateQueries({ queryKey: ["trait", "records"] })
      void queryClient.invalidateQueries({ queryKey: ["sync"] })
      void queryClient.invalidateQueries({ queryKey: ["bundles", "status"] })
    },
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Disconnecting failed",
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
              size="icon-sm"
              aria-label={`More for ${view.label}`}
            />
          }
        >
          <EllipsisIcon className="size-4" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            disabled={!view.kind}
            onClick={() => setEditing(true)}
          >
            <PencilIcon />{" "}
            {brings ? "Change what it brings in" : "Change its settings"}
          </DropdownMenuItem>
          {provider?.oauth && view.tokenStatus === "connected" && (
            <DropdownMenuItem onClick={() => setReconnecting(true)}>
              <RefreshCwIcon /> Reconnect…
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setRemoving(true)}
          >
            <Trash2Icon /> Disconnect…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {view.kind && editing && (
        <AccountDialog
          kind={view.kind}
          providerName={providerName}
          oauth={provider?.oauth ?? false}
          configured={provider?.configured ?? true}
          record={view.record}
          label={view.label}
          open={editing}
          onOpenChange={setEditing}
        />
      )}
      {reconnecting && (
        <ConnectConfirm
          view={view}
          providerName={providerName}
          connect={connect}
          reconnect={view.tokenStatus === "connected"}
          onClose={() => setReconnecting(false)}
        />
      )}
      {removing && (
        <ConfirmDialog
          title={`Disconnect ${view.label}?`}
          consequence={
            cascades.length
              ? `${providerName} stops bringing anything in from this account and deletes everything it brought in from it: its ${andList(cascades.map((k) => displayPlural(k).toLowerCase()))}. Your own records stay.`
              : `${providerName} stops bringing anything in from this account, and its approval is withdrawn. What it already brought in stays until you remove ${providerName}. Connecting it again means approving it again.`
          }
          confirm="Disconnect"
          destructive
          pending={disconnect.isPending}
          onConfirm={() => disconnect.mutate()}
          onClose={() => setRemoving(false)}
        />
      )}
    </>
  )
}
