/** The Providers area's small marks: a provider's logo tile and the state
 * its card and page wear.
 * Everything a provider page draws that is not an identity component. */

import type { ReactNode } from "react"

import { Pill, type PillTone } from "@/components/identity/pill"
import { ProviderBadge } from "@/components/identity/provider-badge"
import type { ProviderInfo } from "@/lib/actor-identity"
import type { ProviderStanding, WordTone } from "@/lib/providers"
import { cn } from "@/lib/utils"

/** The provider's letter in its brand colour, on a larger tile than the
 * badge a row carries: 32px on a card, 44px in a page header. */
export function ProviderLogo({
  info,
  size = "md",
}: {
  info: ProviderInfo
  size?: "md" | "lg"
}) {
  return (
    <ProviderBadge
      provider={info}
      size="md"
      className={cn(
        "border-border",
        size === "md" && "size-8 rounded-lg text-sm",
        size === "lg" && "size-11 rounded-[10px] text-lg"
      )}
    />
  )
}

const STANDING_TONE: Record<ProviderStanding["tone"], PillTone> = {
  on: "ok",
  setup: "warn",
  attention: "bad",
  paused: "neutral",
  add: "neutral",
}

export function StandingPill({ standing }: { standing: ProviderStanding }) {
  if (!standing.pill) return null
  return <Pill tone={STANDING_TONE[standing.tone]}>{standing.pill}</Pill>
}

const WORD_TONE: Record<WordTone, string> = {
  ok: "text-ok",
  active: "text-primary-text",
  warn: "text-warning",
  bad: "text-destructive",
  muted: "text-muted-foreground",
}

/** A phrase from `lib/providers` in the colour of what it means. */
export function ToneText({
  tone,
  children,
  className,
}: {
  tone: WordTone
  children: ReactNode
  className?: string
}) {
  return <span className={cn(WORD_TONE[tone], className)}>{children}</span>
}

/** A bordered list of rows, the `.io` shape: one fact per row, hairlines
 * between. */
export function RowList({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        "divide-y divide-border overflow-hidden rounded-[10px] border",
        className
      )}
    >
      {children}
    </div>
  )
}
