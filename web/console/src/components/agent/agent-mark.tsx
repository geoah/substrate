/** An agent's mark: the actor mark every surface draws for an agent (a bot on
 * a purple disc), sized for the chat. */

import { ActorMark } from "@/components/identity/actor-ref"
import { actorIdentity } from "@/lib/actor-identity"
import { agentActor } from "@/lib/agent-chat"

export function AgentMark({
  id,
  size = "md",
}: {
  /** The agent record's id, `<authority>/<package>/<name>`. */
  id: string
  size?: "xs" | "sm" | "md" | "lg"
}) {
  return <ActorMark identity={actorIdentity(agentActor(id))} size={size} />
}
