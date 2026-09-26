/** The Providers area's small marks: a provider's logo tile, the rounded
 * state pill its card and page wear.
 * Everything a provider page draws that is not an identity component. */

import type { ReactNode } from "react"

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

export type PillTone = "ok" | "warn" | "bad" | "neutral" | "accent"

const PILL: Record<PillTone, string> = {
  ok: "bg-ok-soft text-ok",
  warn: "bg-warn-soft text-warning",
  bad: "bg-bad-soft text-destructive",
  neutral: "bg-hover text-muted-foreground",
  accent: "bg-primary-soft text-primary-text",
}

export function Pill({
  tone,
  children,
  dot = true,
  className,
}: {
  tone: PillTone
  children: ReactNode
  dot?: boolean
  className?: string
}) {
  return (
    <span
      data-slot="pill"
      data-tone={tone}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-px text-xs font-medium whitespace-nowrap",
        PILL[tone],
        className
      )}
    >
      {dot && <span aria-hidden className="size-1.5 rounded-full bg-current" />}
      {children}
    </span>
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
