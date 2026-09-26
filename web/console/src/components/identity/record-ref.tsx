/** A record, marked: its kind's glyph and its title, in the mention style
 * (underlined on hover). Never a bare id — a record without a title reads
 * "Untitled <kind>", and its full reference is one hover away. Where only the
 * reference is known, the title is read (batched with every other mark on the
 * page). Links to the record; a click never reaches the row around it. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { IdentityCard, IdentityHoverCard } from "./identity-hover-card"
import { KindGlyph } from "./kind-glyph"
import { useHasQueryClient } from "@/hooks/use-has-query-client"
import { providerOfKind } from "@/lib/actor-identity"
import { splitKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordQueryOptions } from "@/lib/api/records"
import { columnProperties, kindByIdentity } from "@/lib/definition"
import { cellValue, recordTitle, referenceCell } from "@/lib/format"
import { displayName, displayPlural, untitled } from "@/lib/kind-names"
import { recordTitleQueryOptions } from "@/lib/reference-titles"
import { cn } from "@/lib/utils"

export interface RecordRefProps {
  /** The record's kind reference, `<authority>/<package>/<name>`. */
  kind: string
  id: string
  /** The title, when the caller has it; otherwise it is read. */
  title?: string
  /** `mention` is inline text; `chip` sits on a soft pill. */
  variant?: "mention" | "chip"
  link?: boolean
  className?: string
}

export function RecordRef(props: RecordRefProps) {
  const client = useHasQueryClient()
  const { authority, pkg, name } = splitKind(props.kind)
  if (!props.title && client && authority && pkg && name) {
    return <ResolvingRecordRef {...props} />
  }
  return <RecordRefView {...props} />
}

function ResolvingRecordRef(props: RecordRefProps) {
  const kinds = useQuery(kindsQueryOptions)
  // A kind the repository never declared refuses the whole batched read, so
  // its records are never asked about; they read as untitled.
  const known = Boolean(kindByIdentity(kinds.data ?? [], props.kind))
  const title = useQuery({
    ...recordTitleQueryOptions(props.kind, props.id),
    enabled: known,
  })
  const loading = kinds.isPending || (known && title.isPending)
  return (
    <RecordRefView
      {...props}
      title={title.data ?? undefined}
      loading={loading}
    />
  )
}

function RecordRefView({
  kind,
  id,
  title,
  variant = "mention",
  link = true,
  className,
  loading,
}: RecordRefProps & { loading?: boolean }) {
  const { authority, pkg, name } = splitKind(kind)
  const routable = Boolean(authority && pkg && name)
  const trigger =
    link && routable ? (
      <Link
        to="/data/$authority/$pkg/$name/$id"
        params={{ authority, pkg, name, id }}
        onClick={(e) => e.stopPropagation()}
      />
    ) : (
      <span />
    )
  const label = title || (loading ? displayName(kind) : untitled(kind))
  return (
    <IdentityHoverCard
      trigger={trigger}
      label={title ? undefined : `${label}, ${kind}/${id}`}
      className={cn(
        "group/rref inline-flex max-w-full min-w-0 items-center gap-[5px] align-middle text-foreground no-underline outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        variant === "chip" &&
          "rounded-full bg-hover py-px pr-2 pl-[3px] text-[0.93em] leading-[1.55] hover:bg-border-strong",
        className
      )}
      card={(open) => open && <RecordCard kind={kind} id={id} title={title} />}
    >
      <KindGlyph
        kind={kind}
        size="xs"
        className={variant === "chip" ? "rounded-full" : undefined}
      />
      <span
        className={cn(
          "truncate",
          variant === "mention" &&
            "border-b border-border-strong leading-tight group-hover/rref:border-muted-foreground",
          !title && "text-muted-foreground"
        )}
      >
        {label}
      </span>
    </IdentityHoverCard>
  )
}

function RecordCard(props: { kind: string; id: string; title?: string }) {
  const client = useHasQueryClient()
  const { authority, pkg, name } = splitKind(props.kind)
  return client && authority && pkg && name ? (
    <LiveRecordCard {...props} />
  ) : (
    <RecordCardView {...props} />
  )
}

/** Datatypes whose values are paragraphs or blobs, never a fact line. */
const NOT_A_FACT = new Set(["text", "markdown", "json", "secret", "object"])

function LiveRecordCard({
  kind,
  id,
  title,
}: {
  kind: string
  id: string
  title?: string
}) {
  const { authority, pkg, name } = splitKind(kind)
  const kinds = useQuery(kindsQueryOptions)
  const declared = kindByIdentity(kinds.data ?? [], kind)
  const record = useQuery({
    ...recordQueryOptions(authority, pkg, name, id),
    enabled: Boolean(declared),
  })
  const facts: Array<{ label: string; value: string }> = []
  if (declared && record.data) {
    for (const prop of columnProperties(declared)) {
      if (NOT_A_FACT.has(prop.kind)) continue
      const value = record.data.properties[prop.name]
      if (value === undefined || value === null) continue
      const text =
        prop.kind === "reference" ? referenceCell(value) : cellValue(value)
      if (!text) continue
      facts.push({ label: prop.name, value: text })
      if (facts.length === 3) break
    }
  }
  return (
    <RecordCardView
      kind={kind}
      id={id}
      title={(record.data && recordTitle(record.data.properties)) || title}
      facts={facts}
      loading={Boolean(declared) && record.isPending}
    />
  )
}

function RecordCardView({
  kind,
  id,
  title,
  facts,
  loading,
}: {
  kind: string
  id: string
  title?: string
  facts?: Array<{ label: string; value: string }>
  loading?: boolean
}) {
  const provider = providerOfKind(kind)
  return (
    <IdentityCard
      mark={<KindGlyph kind={kind} size="sm" />}
      title={title || untitled(kind)}
      sub={`${displayPlural(kind)}${provider ? ` · from ${provider.name}` : ""}`}
      facts={facts}
      loading={loading}
      reference={`${kind}/${id}`}
    />
  )
}
