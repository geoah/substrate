/** The cross-cutting actor grammar: a name pill, identical everywhere an
 * actor appears (changelog rows, activity rows, property managers), linking to
 * the actor view. Name only — no avatar bubble (owner redline, 2026-08-06).
 * Unregistered names still link — the actor view renders an identity stub
 * over that actor's real changelog. */

import { Link } from "@tanstack/react-router"

import { actorShortName } from "@/lib/format"

export function ActorChip({ actor }: { actor: string }) {
  return (
    <Link
      to="/actors/$actorId"
      params={{ actorId: actor }}
      title={actor}
      // max-w-full + inner truncate: in a table cell the pill ellipsizes at
      // the column boundary instead of being cut mid-border (table ruling).
      className="inline-flex max-w-full min-w-0 items-center rounded-full border bg-muted/50 px-2 py-0.5 text-xs transition-colors hover:bg-muted"
      onClick={(e) => e.stopPropagation()}
    >
      <span className="data truncate">{actorShortName(actor)}</span>
    </Link>
  )
}
