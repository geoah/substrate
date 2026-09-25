/** A record's state: a dot coloured by what the state means and the state in
 * plain words ("Suggested", "Dropped"); technical mode appends the stored
 * value. */

import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { stateTone, stateWord, type StateTone } from "@/lib/state-words"
import { cn } from "@/lib/utils"

const TONES: Record<StateTone, { ink: string; tag: string }> = {
  active: { ink: "text-primary-text", tag: "bg-primary-soft" },
  ok: { ink: "text-ok", tag: "bg-ok-soft" },
  pending: { ink: "text-warning", tag: "bg-warn-soft" },
  stopped: { ink: "text-faint", tag: "bg-hover" },
  bad: { ink: "text-destructive", tag: "bg-bad-soft" },
}

export function StateBadge({
  value,
  initial,
  variant = "dot",
  className,
}: {
  /** The stored state value. */
  value: string
  /** The machine's initial state, which colours a state the words do not
   * know. */
  initial?: string
  /** `tag` sets the word on a soft fill of the same meaning. */
  variant?: "dot" | "tag"
  className?: string
}) {
  const [technical] = useTechnicalDetails()
  const tone = TONES[stateTone(value, initial)]
  const word = stateWord(value)
  return (
    <span
      data-slot="state-badge"
      data-tone={stateTone(value, initial)}
      className={cn(
        "inline-flex items-center gap-1.5 align-middle whitespace-nowrap",
        tone.ink,
        variant === "tag" && ["rounded-[5px] px-2 py-px font-medium", tone.tag],
        className
      )}
    >
      <span aria-hidden className="size-2 shrink-0 rounded-full bg-current" />
      <span className={variant === "tag" ? undefined : "text-foreground"}>
        {word}
      </span>
      {technical && (
        <span className="font-mono text-[11px] text-faint">{value}</span>
      )}
    </span>
  )
}
