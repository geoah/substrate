/** Who did something, marked: "You" (a person on a neutral disc, never a
 * letter), an agent (a bot, purple), a provider's function or bundle (the
 * provider's badge), a tool of the repository's own, the substrate itself (a
 * shield). Plain names on the page; the raw actor string is in the hover card
 * and, in technical mode, inline. */

import { Link } from "@tanstack/react-router"
import { Bot, Box, ShieldCheck, UserRound, Zap } from "lucide-react"

import { IdentityCard, IdentityHoverCard } from "./identity-hover-card"
import { ProviderBadge } from "./provider-badge"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { actorIdentity, type ActorIdentity } from "@/lib/actor-identity"
import { splitKind } from "@/lib/api/http"
import { cn } from "@/lib/utils"

const DISCS = {
  you: { icon: UserRound, tone: "bg-kind-gray-bg text-muted-foreground" },
  agent: { icon: Bot, tone: "bg-kind-purple-bg text-kind-purple-fg" },
  function: { icon: Zap, tone: "bg-kind-orange-bg text-kind-orange-fg" },
  bundle: { icon: Box, tone: "bg-kind-gray-bg text-kind-gray-fg" },
  engine: { icon: ShieldCheck, tone: "bg-kind-gray-bg text-kind-gray-fg" },
} as const

const MARK_SIZES = {
  xs: { disc: "size-4", icon: "size-[10px]" },
  sm: { disc: "size-5", icon: "size-3" },
  md: { disc: "size-6", icon: "size-3.5" },
  lg: { disc: "size-8", icon: "size-4" },
} as const

/** The actor's mark on its own. */
export function ActorMark({
  identity,
  size = "sm",
}: {
  identity: ActorIdentity
  size?: "xs" | "sm" | "md" | "lg"
}) {
  if (identity.provider) {
    return (
      <ProviderBadge
        provider={identity.provider}
        size={size === "lg" ? "md" : size}
      />
    )
  }
  const { icon: Icon, tone } = DISCS[identity.cls]
  return (
    <span
      aria-hidden
      data-slot="actor-mark"
      data-actor={identity.cls}
      className={cn(
        "inline-grid shrink-0 place-items-center rounded-full",
        MARK_SIZES[size].disc,
        tone
      )}
    >
      <Icon className={MARK_SIZES[size].icon} strokeWidth={2} />
    </span>
  )
}

export function ActorRef({
  actor,
  link = "actor",
  className,
}: {
  /** The actor string as stored (`console`, `agent:<authority>:<pkg>:<name>`,
   * `substrate`, …). */
  actor: string
  /** Where a click goes: the actor view, the actor's declaration record
   * (falling back to the actor view), or nowhere. */
  link?: "actor" | "record" | false
  className?: string
}) {
  const [technical] = useTechnicalDetails()
  const identity = actorIdentity(actor)
  const record = identity.record ? splitKind(identity.record.kind) : undefined
  const trigger =
    link === "record" && identity.record && record ? (
      <Link
        to="/data/$authority/$pkg/$name/$id"
        params={{
          authority: record.authority,
          pkg: record.pkg,
          name: record.name,
          id: identity.record.id,
        }}
        onClick={(e) => e.stopPropagation()}
      />
    ) : link ? (
      <Link
        to="/actors/$actorId"
        params={{ actorId: actor }}
        onClick={(e) => e.stopPropagation()}
      />
    ) : (
      <span />
    )
  return (
    <IdentityHoverCard
      trigger={trigger}
      className={cn(
        "group/actor inline-flex max-w-full min-w-0 items-center gap-1.5 align-middle text-foreground no-underline outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        className
      )}
      card={
        <IdentityCard
          mark={<ActorMark identity={identity} />}
          title={identity.name}
          sub={identity.via ? `via ${identity.via}` : undefined}
          description={identity.description}
          reference={actor}
        />
      }
    >
      <ActorMark identity={identity} />
      <span className="truncate underline-offset-2 group-hover/actor:underline">
        {identity.name}
      </span>
      {identity.cls === "agent" && <span className="text-faint">agent</span>}
      {technical && (
        <span className="font-mono text-[11px] [overflow-wrap:anywhere] text-faint">
          {actor}
        </span>
      )}
    </IdentityHoverCard>
  )
}
