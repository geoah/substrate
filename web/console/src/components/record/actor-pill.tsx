/** An actor on the Provenance tab, linking to its declaration record (the
 * function, agent or bundle) where it has one. Kept as the name the tab
 * imports; the mark itself is `ActorRef`. */

import { ActorRef } from "@/components/identity/actor-ref"

export function ActorPill({
  actor,
  className,
}: {
  actor: string
  /** The kind of the source record the value came from. The actor's own
   * name says what it is now, so this no longer changes the words. */
  sourceKind?: string
  className?: string
}) {
  return <ActorRef actor={actor} link="record" className={className} />
}
