/** A kind, marked: its glyph and its display plural ("People") in label
 * mode, or its full reference with the authority and package toned down and
 * the name emphasised in reference mode. Technical mode shows the full
 * reference beside a label too. Links to the kind's collection. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { IdentityCard, IdentityHoverCard } from "./identity-hover-card"
import { KindGlyph } from "./kind-glyph"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useHasQueryClient } from "@/hooks/use-has-query-client"
import { splitKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo } from "@/lib/api/types"
import { kindPurpose, type KindPurpose } from "@/lib/definition"
import { displayPlural } from "@/lib/kind-names"
import { cn } from "@/lib/utils"

const PURPOSE_WORDS: Record<KindPurpose, string> = {
  primary: "A collection",
  supporting: "Supporting records",
  internal: "Internal machinery",
}

/** A kind reference with its authority and package toned down and its name
 * emphasised. Wraps; never truncates. */
export function KindPath({
  reference,
  className,
}: {
  reference: string
  className?: string
}) {
  const { authority, pkg, name } = splitKind(reference)
  if (!authority) {
    return (
      <span className={cn("font-mono text-[12.5px]", className)}>{name}</span>
    )
  }
  return (
    <span
      className={cn(
        "font-mono text-[12.5px] [overflow-wrap:anywhere]",
        className
      )}
    >
      <span className="text-faint">{authority}</span>
      <span className="px-px text-faint">/</span>
      <span className="text-muted-foreground">{pkg}</span>
      <span className="px-px text-faint">/</span>
      <span className="font-medium text-foreground">{name}</span>
    </span>
  )
}

export function KindRef({
  kind,
  mode = "label",
  link = true,
  count,
  className,
}: {
  /** A registry entry or a full kind reference. */
  kind: KindInfo | string
  mode?: "label" | "reference"
  link?: boolean
  /** How many records the kind holds, for the hover card, when known. */
  count?: number
  className?: string
}) {
  const [technical] = useTechnicalDetails()
  const reference = typeof kind === "string" ? kind : kind.identity
  const { authority, pkg, name } = splitKind(reference)
  const routable = Boolean(authority && pkg && name)
  const trigger =
    link && routable ? (
      <Link
        to="/data/$authority/$pkg/$name"
        params={{ authority, pkg, name }}
        onClick={(e) => e.stopPropagation()}
      />
    ) : (
      <span />
    )
  return (
    <IdentityHoverCard
      trigger={trigger}
      className={cn(
        "inline-flex max-w-full min-w-0 items-center gap-1.5 align-middle text-foreground no-underline",
        mode === "label" && "hover:[&>.kind-label]:underline",
        "underline-offset-2 outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        className
      )}
      card={(open) => open && <KindCard kind={kind} count={count} />}
    >
      <KindGlyph kind={kind} size="xs" />
      {mode === "label" ? (
        <>
          <span className="kind-label truncate">{displayPlural(kind)}</span>
          {technical && (
            <KindPath reference={reference} className="text-[11.5px]" />
          )}
        </>
      ) : (
        <KindPath reference={reference} />
      )}
    </IdentityHoverCard>
  )
}

function KindCard({
  kind,
  count,
}: {
  kind: KindInfo | string
  count?: number
}) {
  const client = useHasQueryClient()
  return client ? (
    <RegistryKindCard kind={kind} count={count} />
  ) : (
    <KindCardView kind={kind} count={count} />
  )
}

function RegistryKindCard({
  kind,
  count,
}: {
  kind: KindInfo | string
  count?: number
}) {
  const kinds = useQuery({
    ...kindsQueryOptions,
    enabled: typeof kind === "string",
  })
  const resolved =
    typeof kind === "string"
      ? (kinds.data?.find((k) => k.identity === kind) ?? kind)
      : kind
  return <KindCardView kind={resolved} count={count} />
}

function KindCardView({
  kind,
  count,
}: {
  kind: KindInfo | string
  count?: number
}) {
  const reference = typeof kind === "string" ? kind : kind.identity
  const facts = [
    ...(count !== undefined
      ? [
          {
            label: "Records",
            value: count ? count.toLocaleString() : "None yet",
          },
        ]
      : []),
    { label: "Listed as", value: PURPOSE_WORDS[kindPurpose(kind)] },
  ]
  return (
    <IdentityCard
      mark={<KindGlyph kind={kind} size="sm" />}
      title={displayPlural(kind)}
      description={
        typeof kind === "string" ? undefined : kind.description || undefined
      }
      facts={facts}
      reference={reference}
    />
  )
}
