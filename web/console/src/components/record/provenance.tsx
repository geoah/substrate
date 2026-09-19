/** The property ledger: per property, the stored value, who holds it, the
 * source record it came from, at which tier — and beneath it every
 * alternative a live source offers, each with "Use this".
 *
 * The wire already carried the whole ledger (`propertyMeta`: manager, tier,
 * alternatives) and the old tab printed the manager as an actor string with a
 * count of "offers" hidden behind a caret. This reads the same sidecar in the
 * reader's words: a function actor is "sync of <kind>" linked to its
 * declaration (actor-pill.tsx), the SOURCE RECORD behind the value is a pill
 * of its own (decision 0094 put it on the wire), the tier is a chip that says
 * what it means for THIS value, and the alternatives are rows, not a number.
 *
 * Two writes live here, both the ordinary PATCH docs/projection.md describes.
 * "Use this" writes an alternative's value, which makes the owner the manager
 * at the owner tier; the row then reads "held by you" and offers "Release",
 * which patches the property to null so the same transaction recomputes it
 * from the live sources. Each asks first, naming the consequence: a held value
 * ignores fresher source values until released. */

import { useMemo, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { FileLockIcon } from "lucide-react"

import { ActorPill } from "@/components/record/actor-pill"
import { RecordPill } from "@/components/record-pill"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"
import { splitKind } from "@/lib/api/http"
import { patchRecord } from "@/lib/api/records"
import type {
  KindInfo,
  PropertyAlternative,
  PropertyMeta,
  SubstrateRecord,
} from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { cellValue, relativeTime } from "@/lib/format"
import {
  WORDS,
  mappingOfSource,
  sourceTitles,
  tierWords,
  type Tier,
} from "@/lib/provenance"
import { splitRecordPath } from "@/lib/record-path"
import { cn } from "@/lib/utils"

interface LedgerRow {
  name: string
  value: unknown
  meta: PropertyMeta
}

/** One write the reader is about to make, held while the dialog asks. */
type Pending =
  | { kind: "use"; property: string; alternative: PropertyAlternative }
  | { kind: "release"; property: string }

/** The properties the ledger lists, in the declaration's order and then the
 * rest alphabetically: every property with a manager row or a stored value.
 * A declared property nothing wrote has no provenance to show. */
function rowsOf(record: SubstrateRecord, kind?: KindInfo): LedgerRow[] {
  const meta = record.propertyMeta ?? {}
  const names = new Set<string>()
  for (const name of Object.keys(kind?.definition.properties ?? {})) {
    if (name in meta || record.properties[name] !== undefined) names.add(name)
  }
  const rest = [
    ...Object.keys(meta),
    ...Object.keys(record.properties).filter(
      (n) => record.properties[n] !== undefined
    ),
  ]
    .filter((n) => !names.has(n))
    .sort()
  for (const name of rest) names.add(name)
  return [...names].map((name) => ({
    name,
    value: record.properties[name],
    meta: meta[name] ?? {},
  }))
}

/** A value on one line, the whole value on hover. */
function Value({ value }: { value: unknown }) {
  if (value === undefined || value === null) {
    return <span className="text-muted-foreground/70">not set</span>
  }
  const text = cellValue(value)
  return (
    <span className="data break-words" title={text}>
      {text}
    </span>
  )
}

function TierChip({ tier }: { tier: Tier | undefined }) {
  const words = tierWords(tier)
  return (
    <span
      className={cn(
        "inline-flex cursor-help items-center rounded-full border px-2 py-0.5 text-[0.7rem] font-medium",
        tier === "owner" && "border-primary/40 bg-primary/10 text-primary",
        tier === "bundle" && "border-amber-500/40 bg-amber-500/10",
        (tier === "machine" || !tier) && "text-muted-foreground"
      )}
      title={`${WORDS.tier}\n\n${words.detail}`}
      data-tier={tier ?? "none"}
    >
      {words.label}
    </span>
  )
}

/** The source record behind a value, as the pill every other surface uses,
 * titled off the record's own links. */
function SourcePill({
  source,
  titles,
  kinds,
}: {
  source: string
  titles: ReadonlyMap<string, string>
  kinds: KindInfo[]
}) {
  const target = splitRecordPath(source)
  if (!target) return null
  return (
    <RecordPill
      kind={kindByIdentity(kinds, target.kind) ? target.kind : ""}
      id={target.id}
      title={titles.get(source)}
    />
  )
}

function Header() {
  return (
    <div className="grid grid-cols-[minmax(6rem,1fr)_minmax(0,2fr)_minmax(0,2fr)_auto] items-center gap-x-4 border-b px-4 py-2 text-xs text-muted-foreground">
      <span>property</span>
      <span>value</span>
      <span className="flex items-center gap-1">
        <span className="cursor-help" title={WORDS.manager}>
          manager
        </span>
        <span aria-hidden>·</span>
        <span className="cursor-help" title={WORDS.source}>
          source
        </span>
        <span aria-hidden>·</span>
        <span className="cursor-help" title={WORDS.tier}>
          tier
        </span>
      </span>
      <span className="text-right">action</span>
    </div>
  )
}

export function ProvenanceRail({
  record,
  kind,
  kinds,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  kinds: KindInfo[]
}) {
  const client = useQueryClient()
  const rows = useMemo(() => rowsOf(record, kind), [record, kind])
  const links = record.linkedFrom
  const titles = useMemo(() => sourceTitles(links ?? []), [links])
  const [pending, setPending] = useState<Pending | null>(null)
  const [failed, setFailed] = useState<string | null>(null)

  const { authority, pkg, name: kindName } = splitKind(record.kind)
  const write = useMutation({
    mutationFn: (p: Pending) =>
      patchRecord(authority, pkg, kindName, record.id, {
        properties: {
          [p.property]: p.kind === "use" ? p.alternative.value : null,
        },
        ifVersion: record.version,
      }),
    onSuccess: async () => {
      setPending(null)
      setFailed(null)
      await client.invalidateQueries({
        queryKey: ["record", authority, pkg, kindName, record.id],
      })
    },
    onError: (err: Error) => {
      setFailed(err.message)
    },
  })

  if (!rows.length) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FileLockIcon />
          </EmptyMedia>
          <EmptyTitle>No properties yet</EmptyTitle>
          <EmptyDescription>
            Nothing has written to this record's properties.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  const kindOf = (path: string | undefined) =>
    path ? splitRecordPath(path)?.kind : undefined

  return (
    <div className="flex min-w-0 flex-col">
      <div className="flex items-baseline gap-2 pb-2">
        <h2 className="text-base font-semibold">Properties</h2>
        <span className="text-sm text-muted-foreground">
          who holds each value, where it came from, and what the sources say
          instead
        </span>
      </div>
      <div className="min-w-0 rounded-xl border bg-card">
        <Header />
        <ul>
          {rows.map((row) => {
            const alts = row.meta.alternatives ?? []
            const held = row.meta.tier === "owner" || row.meta.tier === "bundle"
            return (
              <li
                key={row.name}
                className="border-b last:border-b-0"
                data-property={row.name}
              >
                <div className="grid grid-cols-[minmax(6rem,1fr)_minmax(0,2fr)_minmax(0,2fr)_auto] items-start gap-x-4 px-4 py-2.5 text-sm">
                  <span className="data break-all">{row.name}</span>
                  <div className="min-w-0">
                    <Value value={row.value} />
                  </div>
                  <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                    {row.meta.manager ? (
                      <ActorPill
                        actor={row.meta.manager}
                        sourceKind={kindOf(row.meta.source)}
                      />
                    ) : (
                      <span className="text-xs text-muted-foreground/70">
                        no manager recorded
                      </span>
                    )}
                    {row.meta.source && (
                      <>
                        <span className="text-xs text-muted-foreground">
                          from
                        </span>
                        <SourcePill
                          source={row.meta.source}
                          titles={titles}
                          kinds={kinds}
                        />
                      </>
                    )}
                    {row.meta.manager && <TierChip tier={row.meta.tier} />}
                    {row.meta.updatedAt && (
                      <span
                        className="text-xs text-muted-foreground"
                        title={row.meta.updatedAt}
                      >
                        {relativeTime(row.meta.updatedAt)}
                      </span>
                    )}
                  </div>
                  <div className="text-right">
                    {held && (
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-6 px-2 text-xs"
                        onClick={() =>
                          setPending({ kind: "release", property: row.name })
                        }
                      >
                        Release
                      </Button>
                    )}
                  </div>
                </div>
                {alts.length > 0 && (
                  <ul className="mx-4 mb-2.5 rounded-lg border bg-muted/30">
                    <li
                      className="border-b px-3 py-1 text-[0.7rem] text-muted-foreground"
                      title={WORDS.alternative}
                    >
                      {alts.length === 1
                        ? "1 alternative from a live source"
                        : `${alts.length} alternatives from live sources`}
                    </li>
                    {alts.map((alt, i) => {
                      const mapping = alt.source
                        ? mappingOfSource(links ?? [], alt.source)
                        : undefined
                      return (
                        <li
                          key={`${alt.actor} ${alt.source ?? i}`}
                          className="grid grid-cols-[minmax(0,2fr)_minmax(0,2fr)_auto] items-center gap-x-4 px-3 py-1.5 text-sm"
                          data-alternative
                        >
                          <div className="min-w-0">
                            <Value value={alt.value} />
                          </div>
                          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                            <ActorPill
                              actor={alt.actor}
                              sourceKind={kindOf(alt.source)}
                            />
                            {alt.source && (
                              <>
                                <span className="text-xs text-muted-foreground">
                                  from
                                </span>
                                <SourcePill
                                  source={alt.source}
                                  titles={titles}
                                  kinds={kinds}
                                />
                              </>
                            )}
                            {mapping && (
                              <span
                                className="truncate text-xs text-muted-foreground"
                                title={mapping}
                              >
                                via {splitKind(mapping).name || mapping}
                              </span>
                            )}
                            <span
                              className="text-xs text-muted-foreground"
                              title={alt.updatedAt}
                            >
                              {relativeTime(alt.updatedAt)}
                            </span>
                          </div>
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-6 px-2 text-xs"
                            onClick={() =>
                              setPending({
                                kind: "use",
                                property: row.name,
                                alternative: alt,
                              })
                            }
                          >
                            Use this
                          </Button>
                        </li>
                      )
                    })}
                  </ul>
                )}
              </li>
            )
          })}
        </ul>
      </div>

      {pending && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open && !write.isPending) {
              setPending(null)
              setFailed(null)
            }
          }}
        >
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>
                {pending.kind === "use"
                  ? `Use this value for ${pending.property}?`
                  : `Release ${pending.property}?`}
              </DialogTitle>
              <DialogDescription>
                {pending.kind === "use" ? (
                  <>
                    <span className="data">
                      {cellValue(pending.alternative.value)}
                    </span>{" "}
                    is written to <code>{pending.property}</code>, and you
                    become its manager at the owner tier. A held value ignores
                    fresher source values until you release it; the sources'
                    values stay readable here as alternatives.
                  </>
                ) : (
                  <>
                    The value is cleared and its hold with it, and projection
                    refills <code>{pending.property}</code> from the live
                    sources in the same write, so the property follows them
                    again from here on.
                  </>
                )}
              </DialogDescription>
            </DialogHeader>
            {failed && (
              <p role="alert" className="text-sm text-destructive">
                {failed}
              </p>
            )}
            <DialogFooter>
              <Button
                variant="outline"
                disabled={write.isPending}
                onClick={() => {
                  setPending(null)
                  setFailed(null)
                }}
              >
                Cancel
              </Button>
              <Button
                disabled={write.isPending}
                onClick={() => write.mutate(pending)}
              >
                {write.isPending && <Spinner className="size-3.5" />}
                {pending.kind === "use" ? "Use this" : "Release"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}
