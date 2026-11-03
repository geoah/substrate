/** A package its publisher's authority document publishes (decision 0212):
 * it follows the publisher's releases, its declarations are not the
 * owner's to edit, the bundle's runtime switch stays the owner's, and Take
 * over… is the way out, confirmed by typing the package's identity. Taking
 * it over makes it the owner's, editable, and stops the publisher's
 * upgrades; the records in it are the owner's throughout. */

import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { SectionHead } from "@/components/identity/section-head"
import { PauseBundleButton } from "@/components/providers/bundle-actions"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import { Input } from "@/components/ui/input"
import { toast } from "@/components/ui/toast"
import { bundleStatusQueryOptions } from "@/lib/api/bundles"
import { kindsQueryOptions, takeOverPackage } from "@/lib/api/kinds"
import { packagesQueryOptions } from "@/lib/packages"

export function PublishedPackageSection({
  publisher,
  packageId,
}: {
  /** The authority that publishes the package. */
  publisher: string
  /** `<authority>/<package>`. */
  packageId: string
}) {
  const client = useQueryClient()
  const bundle = useQuery(bundleStatusQueryOptions(packageId))
  const [confirming, setConfirming] = useState(false)
  const [typed, setTyped] = useState("")
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
      <SectionHead title="Updates" />
      <div className="flex max-w-md flex-col gap-2 overflow-hidden rounded-[10px] border border-border px-3 py-2.5">
        <p className="text-sm text-muted-foreground">
          This package follows {publisher}’s releases, so you can’t change its
          collections’ definitions here. You can pause it and resume it. Take it
          over to edit its definitions yourself and stop receiving updates.
        </p>
        <div className="flex flex-wrap gap-2">
          {bundle.data && (
            <PauseBundleButton bundle={bundle.data} name={publisher} />
          )}
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
          title={`Take over from ${publisher}?`}
          consequence={`You’ll manage these collections’ definitions yourself, and ${publisher}’s releases won’t update them any more. Nothing else changes: your records stay as they are. This can’t be handed back.`}
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
