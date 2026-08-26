/** RecordPill is THE way a record is referenced from somewhere else: one
 * pill, one icon, everywhere — an agent's trigger, a property value, a
 * proposal's target, a graph node. A reference that always looks the same is
 * what tells the reader "this is a record; click it".
 */

import { Link } from "@tanstack/react-router"
import { Box } from "lucide-react"

import { splitKind } from "@/lib/api/http"
import { cn } from "@/lib/utils"

export function RecordPill({
  kind,
  id,
  title,
  className,
}: {
  /** The record's kind reference, `<authority>/<name>`. */
  kind: string
  id: string
  /** The record's display title; the id stands in when absent. */
  title?: string
  className?: string
}) {
  const { authority, name } = splitKind(kind)
  const label = title || id
  const pill = cn(
    "inline-flex max-w-full items-center gap-1 rounded-full border bg-muted/40 px-2 py-0.5",
    "align-middle text-xs font-medium text-foreground no-underline",
    "transition-colors hover:border-foreground/20 hover:bg-muted",
    "outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
    className
  )
  // A kind reference without its one slash names nothing routable; the pill
  // still renders, inert, rather than minting a broken link.
  if (!authority || !name) {
    return (
      <span className={pill}>
        <Box aria-hidden className="size-3 shrink-0 text-muted-foreground" />
        <span className="truncate">{label}</span>
      </span>
    )
  }
  return (
    <Link
      to="/data/$authority/$name/$id"
      params={{ authority, name, id }}
      className={pill}
      title={`${kind}/${id}`}
    >
      <Box aria-hidden className="size-3 shrink-0 text-muted-foreground" />
      <span className="truncate">{label}</span>
    </Link>
  )
}
