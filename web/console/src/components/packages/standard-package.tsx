/** A package its publisher keeps up to date, on its All data page (decision
 * record 0238): a standard collection (`health`, `clinical`) is no provider, so
 * Providers does not list it, and this is where its update, its pause and
 * its removal live. A package from an authority document keeps its own
 * section (published-package.tsx), which takes the update control from here. */

import { useCallback, useState } from "react"

import { SectionHead } from "@/components/identity/section-head"
import {
  LossyUpgradeDialog,
  PauseBundleButton,
  RemoveBundleButton,
  UpgradeBlockedNote,
  UpgradeButton,
} from "@/components/providers/bundle-actions"
import { upgradeAvailable, upgradeBlocked, type BundleRow } from "@/lib/bundles"

/** Update, when one is offered: its button, or why it is blocked. */
export function PackageUpdate({ row, name }: { row: BundleRow; name: string }) {
  const [lossy, setLossy] = useState(false)
  const close = useCallback(() => setLossy(false), [])
  const open = useCallback(() => setLossy(true), [])
  if (upgradeBlocked(row)) return <UpgradeBlockedNote row={row} />
  if (!upgradeAvailable(row)) return null
  return (
    <>
      <UpgradeButton row={row} name={name} onConfirmLoss={open} />
      <LossyUpgradeDialog
        row={lossy ? row : undefined}
        name={name}
        onClose={close}
      />
    </>
  )
}

export function StandardPackageSection({
  row,
  name,
}: {
  row: BundleRow
  name: string
}) {
  const status = row.status
  if (!status) return null
  return (
    <section>
      <SectionHead title="Updates" />
      <div className="flex max-w-md flex-col gap-2 overflow-hidden rounded-[10px] border border-border px-3 py-2.5">
        <p className="text-sm text-muted-foreground">
          {row.authority} keeps this package up to date for you, so apps and
          providers can fill it in. You add the records; its collections’
          definitions aren’t yours to change.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <PackageUpdate row={row} name={name} />
          <PauseBundleButton bundle={status} name={name} />
          <RemoveBundleButton
            bundle={status}
            name={name}
            owned
            leaveTo="/data"
          />
        </div>
      </div>
    </section>
  )
}
