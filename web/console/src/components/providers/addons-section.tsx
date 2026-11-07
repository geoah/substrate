/** A provider's add-ons (decision record 0238): the packages the catalog says
 * extend it, listed on its page rather than as providers of their own. Each
 * opens its own page, where its settings are, and is added there or here. */

import { Link } from "@tanstack/react-router"
import { ChevronRightIcon } from "lucide-react"

import { SectionHead } from "@/components/identity/section-head"
import { TakeButton } from "@/components/providers/bundle-actions"
import { StandingPill } from "@/components/providers/provider-marks"
import type { ProviderEntry } from "@/components/providers/use-providers"
import type { RequirementNode } from "@/lib/bundles"
import { bundleParams, firstSentence } from "@/lib/providers"

export function AddonsSection({
  addons,
  providerName,
  chains,
}: {
  addons: ProviderEntry[]
  providerName: string
  chains: (row: ProviderEntry["row"]) => RequirementNode[]
}) {
  if (!addons.length) return null
  return (
    <>
      <SectionHead
        id="addons"
        title="Add-ons"
        hint={`extras for ${providerName}, each added on its own`}
      />
      <ul className="divide-y overflow-hidden rounded-[10px] border bg-background">
        {addons.map((addon) => (
          <AddonRow
            key={addon.row.key}
            entry={addon}
            chain={chains(addon.row)}
          />
        ))}
      </ul>
    </>
  )
}

function AddonRow({
  entry: { row, info, standing },
  chain,
}: {
  entry: ProviderEntry
  chain: RequirementNode[]
}) {
  return (
    <li
      data-slot="addon"
      className="group relative flex items-start gap-3 px-4 py-3 hover:bg-hover"
    >
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <Link
          to="/providers/$authority/$pkg"
          params={bundleParams(row.id)}
          className="font-medium underline-offset-2 after:absolute after:inset-0 hover:underline"
        >
          {info.name}
        </Link>
        {row.catalog?.description && (
          <p className="text-[13px] text-muted-foreground">
            {firstSentence(row.catalog.description)}
          </p>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-1.5 self-center">
        {standing.tone === "add" ? (
          <TakeButton
            row={row}
            chain={chain}
            name={info.name}
            label="Add"
            className="relative z-10 h-7"
          />
        ) : (
          <StandingPill standing={standing} />
        )}
        <ChevronRightIcon
          aria-hidden
          className="size-4 text-faint transition-transform group-hover:translate-x-0.5 group-hover:text-muted-foreground"
        />
      </div>
    </li>
  )
}
