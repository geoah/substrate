/** A status, on a rounded soft fill of the colour of what it means: "On",
 * "Ran 2 min ago", "Having trouble", "Update available". The one pill every
 * page wears for a status; a record's STATE is a `StateBadge`. */

import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

export type PillTone = "ok" | "warn" | "bad" | "neutral" | "accent"

const TONES: Record<PillTone, string> = {
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
  live = false,
  title,
  className,
}: {
  tone: PillTone
  children: ReactNode
  /** The small dot before the words; off for a fact that is not a status
   * ("Update available", a count). */
  dot?: boolean
  /** Something is under way: the dot pulses, where motion is welcome. */
  live?: boolean
  title?: string
  className?: string
}) {
  return (
    <span
      data-slot="pill"
      data-tone={tone}
      title={title}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-px text-xs font-medium whitespace-nowrap",
        TONES[tone],
        className
      )}
    >
      {dot && (
        <span
          aria-hidden
          className={cn(
            "size-1.5 shrink-0 rounded-full bg-current",
            live && "motion-safe:animate-pulse"
          )}
        />
      )}
      {children}
    </span>
  )
}
