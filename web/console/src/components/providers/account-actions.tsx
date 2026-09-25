/** What a person does to one connected account: connect or reconnect it
 * (the provider's consent, in a new tab), and, behind its menu, change what
 * it brings in or disconnect it. Sync now and Pause are the sync panel's.
 * Every verb that leaves the page or loses something asks first. */

import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import {
  EllipsisIcon,
  PencilIcon,
  RefreshCwIcon,
  Trash2Icon,
} from "lucide-react"

import { AccountDialog } from "@/components/sync/account-dialog"
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
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { useOAuthConnect, type OAuthConnect } from "@/hooks/use-oauth-connect"
import { deleteRecord } from "@/lib/api/sync"
import { recordPath } from "@/lib/record-path"
import type { AccountView, ProviderView } from "@/lib/sync"

/** The confirmation a connect asks: the provider opens in a new tab. The
 * caller owns the connect, so the consent's return is heard after this
 * closes. */
function ConnectConfirm({
  view,
  providerName,
  connect,
  onClose,
}: {
  view: AccountView
  providerName: string
  connect: OAuthConnect
  onClose: () => void
}) {
  const connected = view.tokenStatus === "connected"
  const verb = connected ? "Reconnect" : "Connect"
  return (
    <Dialog
      open
      onOpenChange={(open) => !open && !connect.isPending && onClose()}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {verb} {view.label}?
          </DialogTitle>
          <DialogDescription>
            {providerName} opens in a new tab and asks you to approve access.
            Once you do, this account starts bringing in what you turned on.
            {connected && " Reconnecting replaces the current approval."}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            variant="outline"
            disabled={connect.isPending}
            onClick={onClose}
          >
            Cancel
          </Button>
          <Button
            disabled={connect.isPending}
            onClick={() => connect.mutate(undefined, { onSettled: onClose })}
          >
            {connect.isPending && <Spinner className="size-3.5" />}
            {verb}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Connect or Reconnect, with the confirmation that says a tab opens. */
export function ConnectButton({
  view,
  providerName,
  disabled,
  variant = "outline",
}: {
  view: AccountView
  providerName: string
  disabled?: boolean
  variant?: "outline" | "default"
}) {
  const [confirming, setConfirming] = useState(false)
  const connect = useOAuthConnect(
    recordPath(view.record.kind, view.record.id),
    view.label
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
        {view.tokenStatus === "connected" ? "Reconnect" : "Connect"}
      </Button>
      {confirming && (
        <ConnectConfirm
          view={view}
          providerName={providerName}
          connect={connect}
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
  const [editing, setEditing] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [reconnecting, setReconnecting] = useState(false)
  const connect = useOAuthConnect(
    recordPath(view.record.kind, view.record.id),
    view.label
  )
  const disconnect = useMutation({
    mutationFn: () => deleteRecord(view.record),
    onSuccess: () => {
      toast.add({ type: "success", title: `${view.label} disconnected.` })
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
            <PencilIcon /> Change what it brings in
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
          onClose={() => setReconnecting(false)}
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
                {providerName} stops bringing anything in from this account, and
                its approval is withdrawn. What it already brought in stays
                until you remove {providerName}. Connecting it again means
                approving it again.
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
