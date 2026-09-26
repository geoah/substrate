/** Where something comes from, marked: the actor mark of its origin (a
 * person for yours, the provider's badge, a bot for an agent, a shield for
 * substrate) and the origin in words. The one rendering of "yours / from
 * Google" on tool cards, collection cards and a value's ownership chip. */

import { ActorMark } from "./actor-ref"
import type { ActorClass } from "@/lib/actor-identity"
import { originWords, type Origin } from "@/lib/origin"
import { cn } from "@/lib/utils"

const CLASS: Record<
  Exclude<Origin["kind"], "actor" | "provider">,
  ActorClass
> = {
  yours: "you",
  core: "engine",
  other: "bundle",
}

export function OriginMark({
  origin,
  short = false,
  className,
}: {
  origin: Origin
  /** The name alone ("You", "Google"), for a chip. */
  short?: boolean
  className?: string
}) {
  const identity =
    origin.kind === "actor"
      ? origin.identity
      : origin.kind === "provider"
        ? { cls: "bundle" as const, provider: origin.provider }
        : { cls: CLASS[origin.kind] }
  return (
    <span
      data-slot="origin-mark"
      data-origin={origin.kind}
      className={cn(
        "inline-flex min-w-0 items-center gap-1.5 whitespace-nowrap",
        className
      )}
    >
      <ActorMark identity={identity} size="xs" />
      <span className="truncate">{originWords(origin, short)}</span>
    </span>
  )
}
