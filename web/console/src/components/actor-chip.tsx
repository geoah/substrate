/** An actor, linking to the actor view. Kept as the name existing surfaces
 * import; the mark itself is `ActorRef`. */

import { ActorRef } from "@/components/identity/actor-ref"

export function ActorChip({ actor }: { actor: string }) {
  return <ActorRef actor={actor} />
}
