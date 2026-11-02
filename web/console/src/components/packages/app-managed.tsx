/** A package an approved app manages (decision 0205): which app, what that
 * means, and the owner's one way out, Take over, confirmed by typing the
 * package's identity. The app declared the package under its `declare:`
 * scope, so its collections' definitions change only through it; the
 * records in them are the owner's throughout. */

import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"

import { ActorRef } from "@/components/identity/actor-ref"
import { SectionHead } from "@/components/identity/section-head"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import { Input } from "@/components/ui/input"
import { toast } from "@/components/ui/toast"
import { actorIdentity } from "@/lib/actor-identity"
import { kindsQueryOptions, takeOverPackage } from "@/lib/api/kinds"
import { appManagedLine, packagesQueryOptions } from "@/lib/packages"

export function AppManagedSection({
  app,
  packageId,
}: {
  /** The app's actor, `app:<host>`. */
  app: string
  /** `<authority>/<package>`. */
  packageId: string
}) {
  const client = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const [typed, setTyped] = useState("")
  const name = actorIdentity(app).name
  const take = useMutation({
    mutationFn: () => takeOverPackage(packageId),
    onSuccess: async () => {
      setConfirming(false)
      await Promise.all([
        client.invalidateQueries({ queryKey: packagesQueryOptions.queryKey }),
        client.invalidateQueries({ queryKey: kindsQueryOptions.queryKey }),
        client.invalidateQueries({ queryKey: ["records"] }),
      ])
      toast.add({ type: "success", title: "The package is yours now" })
    },
  })
  const close = () => {
    setConfirming(false)
    setTyped("")
    take.reset()
  }
  return (
    <section>
      <SectionHead title="Managed by" />
      <div className="flex max-w-md flex-col gap-2 overflow-hidden rounded-[10px] border border-border px-3 py-2.5">
        <ActorRef actor={app} />
        <p className="text-[13px] text-muted-foreground">
          {appManagedLine(app)} {name} adds to them as it updates, and you can’t
          change them here.
        </p>
        <div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setConfirming(true)}
          >
            Take over…
          </Button>
        </div>
      </div>
      {confirming && (
        <ConfirmDialog
          title={`Take over from ${name}?`}
          consequence={`You’ll manage these collections’ definitions yourself, and ${name} won’t be able to update them any more. Nothing else changes: your records stay as they are. This can’t be handed back.`}
          confirm="Take over"
          destructive
          pending={take.isPending}
          disabled={typed.trim() !== packageId}
          error={take.error?.message}
          onConfirm={() => take.mutate()}
          onClose={close}
        >
          <label className="flex flex-col gap-1.5 text-sm">
            <span>
              Type <span className="data">{packageId}</span> to confirm.
            </span>
            <Input
              value={typed}
              autoComplete="off"
              spellCheck={false}
              aria-label="The package to take over"
              onChange={(e) => setTyped(e.target.value)}
            />
          </label>
        </ConfirmDialog>
      )}
    </section>
  )
}
