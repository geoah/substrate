/** A record, marked: its kind's glyph and its title, in the mention style
 * (underlined on hover). Never a bare id — a record without a title reads
 * "Untitled <kind>", and its full reference is one hover away. Where only the
 * reference is known, the title is read (batched with every other mark on the
 * page). Links to the record; a click never reaches the row around it. */

import { useQueries, useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import {
  IdentityCard,
  IdentityHoverCard,
  type IdentityFact,
} from "./identity-hover-card"
import { KindGlyph } from "./kind-glyph"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useHasQueryClient } from "@/hooks/use-has-query-client"
import { providerOfKind } from "@/lib/actor-identity"
import { splitKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordQueryOptions } from "@/lib/api/records"
import { readReference } from "@/lib/api/types"
import { columnProperties, kindByIdentity } from "@/lib/definition"
import { cellValue, recordTitle, referenceCell } from "@/lib/format"
import { displayName, displayPlural, untitled } from "@/lib/kind-names"
import { splitRecordPath } from "@/lib/record-path"
import { humanizeName, propSpecs } from "@/lib/record-schema"
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

/** The record's hover card body: its title, its collection, up to three
 * property values under their declared labels (a reference as its referent's
 * title) and the full reference. Property keys show in technical mode. */
export function RecordCard(props: {
  kind: string
  id: string
  title?: string
}) {
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

/** How many facts the card shows: enough to tell two records apart. */
const FACTS = 3

/** A reference value in a fact: the referents' titles, read through the
 * batched title read every other mark on the page shares, never their ids. */
function ReferenceTitles({ value }: { value: unknown }) {
  const paths = (Array.isArray(value) ? value : [value]).flatMap((one) => {
    const held = readReference(one)
    const parts = held ? splitRecordPath(held.path) : undefined
    return parts ? [parts] : []
  })
  const first = paths.slice(0, 2)
  // A kind the repository never declared refuses the whole batched read.
  const kinds = useQuery(kindsQueryOptions)
  const titles = useQueries({
    queries: first.map((p) => ({
      ...recordTitleQueryOptions(p.kind, p.id),
      enabled: Boolean(kindByIdentity(kinds.data ?? [], p.kind)),
    })),
  })
  const words = first.map(
    (p, i) =>
      titles[i]?.data || (titles[i]?.isFetching ? "…" : untitled(p.kind))
  )
  const more = paths.length - first.length
  return (
    <>
      {words.join(", ")}
      {more > 0 && ` and ${more} more`}
    </>
  )
}

function LiveRecordCard({
  kind,
  id,
  title,
}: {
  kind: string
  id: string
  title?: string
}) {
  const [technical] = useTechnicalDetails()
  const { authority, pkg, name } = splitKind(kind)
  const kinds = useQuery(kindsQueryOptions)
  const declared = kindByIdentity(kinds.data ?? [], kind)
  const record = useQuery({
    ...recordQueryOptions(authority, pkg, name, id),
    enabled: Boolean(declared),
  })
  const facts: IdentityFact[] = []
  if (declared && record.data) {
    const labels = new Map(propSpecs(declared).map((p) => [p.name, p.label]))
    for (const prop of columnProperties(declared)) {
      if (NOT_A_FACT.has(prop.kind)) continue
      const value = record.data.properties[prop.name]
      if (value === undefined || value === null) continue
      const label = labels.get(prop.name) ?? humanizeName(prop.name)
      const detail = technical ? prop.name : undefined
      if (prop.kind === "reference") {
        if (!referenceCell(value)) continue
        facts.push({ label, detail, value: <ReferenceTitles value={value} /> })
      } else {
        const text = cellValue(value)
        if (!text) continue
        facts.push({ label, detail, value: text })
      }
      if (facts.length === FACTS) break
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
  facts?: IdentityFact[]
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
