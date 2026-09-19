/** An actor as the THING it is, never as its wire spelling.
 *
 * `function:providers.substrate.reamde.dev:beeper:beepersync` is the
 * engine's derived name for a function (decision 0025), and it is the right
 * name on the changelog, where identity is the point. On the Provenance tab
 * the reader is asking a different question — where did this value come from
 * — so the pill says "sync of user" and links the function's own record; the
 * full actor string is the hover, because identity is moved off the line,
 * never shortened away (house rule: show full references where identity
 * matters). A name a request asserted (`console`, `api`) has no record, so it
 * keeps the ActorChip every other surface uses. */

import { Link } from "@tanstack/react-router"
import { BotIcon, BoxesIcon, CogIcon, FunctionSquareIcon } from "lucide-react"

import { ActorChip } from "@/components/actor-chip"
import { splitKind } from "@/lib/api/http"
import { actorWords } from "@/lib/provenance"
import { cn } from "@/lib/utils"

const ICONS = {
  function: FunctionSquareIcon,
  agent: BotIcon,
  bundle: BoxesIcon,
  engine: CogIcon,
} as const

export function ActorPill({
  actor,
  sourceKind,
  className,
}: {
  actor: string
  /** The kind of the source record the value came from, when known: turns a
   * function into "sync of <kind>". */
  sourceKind?: string
  className?: string
}) {
  const words = actorWords(actor, sourceKind)
  if (words.kind === "plain") return <ActorChip actor={actor} />
  const Icon = ICONS[words.kind]
  const pill = cn(
    "inline-flex max-w-full items-center gap-1 rounded-full border bg-muted/40 px-2 py-0.5",
    "align-middle text-xs font-medium text-foreground no-underline",
    "transition-colors hover:border-foreground/20 hover:bg-muted",
    className
  )
  if (!words.record) {
    return (
      <span className={pill} title={words.actor}>
        <Icon aria-hidden className="size-3 shrink-0 text-muted-foreground" />
        <span className="truncate">{words.label}</span>
      </span>
    )
  }
  const { authority, pkg, name } = splitKind(words.record.kind)
  return (
    <Link
      to="/data/$authority/$pkg/$name/$id"
      params={{ authority, pkg, name, id: words.record.id }}
      className={pill}
      title={words.actor}
      onClick={(e) => e.stopPropagation()}
    >
      <Icon aria-hidden className="size-3 shrink-0 text-muted-foreground" />
      <span className="truncate">{words.label}</span>
    </Link>
  )
}
