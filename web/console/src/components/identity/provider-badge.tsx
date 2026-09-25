/** A provider's mark: its letter on a small bordered square, in its brand
 * colour. The only way a provider is marked. */

import { providerInfo, type ProviderInfo } from "@/lib/actor-identity"
import { cn } from "@/lib/utils"

export function ProviderBadge({
  provider,
  size = "sm",
  className,
}: {
  /** The provider's package word (`google`) or its resolved info. */
  provider: string | ProviderInfo
  size?: "xs" | "sm" | "md"
  className?: string
}) {
  const info = typeof provider === "string" ? providerInfo(provider) : provider
  return (
    <span
      aria-hidden
      data-slot="provider-badge"
      style={{ color: info.color }}
      className={cn(
        "inline-grid shrink-0 place-items-center border border-border-strong bg-background leading-none font-bold",
        size === "xs" && "size-4 rounded-[4px] text-[9.5px]",
        size === "sm" && "size-[18px] rounded-[5px] text-[10px]",
        size === "md" && "size-7 rounded-[7px] text-[13px]",
        className
      )}
    >
      {info.letter}
    </span>
  )
}
