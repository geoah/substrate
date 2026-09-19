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
 * A declared property nothing wrote has no provenance to show, and the
 * built-in `title` is derived storage (decision 0016), never anything a
 * manager holds, so it is not a row. */
function rowsOf(record: SubstrateRecord, kind?: KindInfo): LedgerRow[] {
  const meta = record.propertyMeta ?? {}
  const names = new Set<string>()
  const skip = new Set(["title"])
  for (const name of Object.keys(kind?.definition.properties ?? {})) {
    if (name in meta || record.properties[name] !== undefined) names.add(name)
  }
  const rest = [
    ...Object.keys(meta),
    ...Object.keys(record.properties).filter(
      (n) => record.properties[n] !== undefined
    ),
  ]
    .filter((n) => !names.has(n) && !skip.has(n))
    .sort()
  for (const name of rest) names.add(name)
  return [...names].map((name) => ({
    name,
    value: record.properties[name],
    meta: meta[name] ?? {},
  }))
}

/** A value on one line, the whole value on hover. */
function Value({ value, className }: { value: unknown; className?: string }) {
  if (value === undefined || value === null) {
    return (
      <span className={cn("text-muted-foreground/70", className)}>not set</span>
    )
  }
  const text = cellValue(value)
  return (
    <span className={cn("data break-words", className)} title={text}>
      {text}
    </span>
  )
}

/** Where a value came from, as one line a reader can say aloud: "from
 * ‹source record› by ‹sync of user› · via githubuserperson · 8m ago", or
 * "written by ‹console› · just now" for a hand edit. The two connecting
 * words carry the hover that explains them, so there is no header row to
 * explain a column. */
function Origin({
  actor,
  source,
  mapping,
  at,
  titles,
  kinds,
}: {
  actor?: string
  source?: string
  mapping?: string
  at?: string
  titles: ReadonlyMap<string, string>
  kinds: KindInfo[]
}) {
  const sourceKind = source ? splitRecordPath(source)?.kind : undefined
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground">
      {source ? (
        <>
          <span className="cursor-help" title={WORDS.source}>
            from
          </span>
          <SourcePill source={source} titles={titles} kinds={kinds} />
          {actor && (
            <>
              <span className="cursor-help" title={WORDS.manager}>
                by
              </span>
              <ActorPill actor={actor} sourceKind={sourceKind} />
            </>
          )}
        </>
      ) : actor ? (
        <>
          <span className="cursor-help" title={WORDS.manager}>
            written by
          </span>
          <ActorPill actor={actor} />
        </>
      ) : (
        <span>no manager recorded</span>
      )}
      {mapping && (
        <>
          <span aria-hidden>·</span>
          <span className="truncate" title={mapping}>
            via {splitKind(mapping).name || mapping}
          </span>
        </>
      )}
      {at && (
        <>
          <span aria-hidden>·</span>
          <span title={at}>{relativeTime(at)}</span>
        </>
      )}
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

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <h2 className="text-base font-semibold">Properties</h2>
        <span className="text-sm text-muted-foreground">
          who holds each value, where it came from, and what the sources say
          instead
        </span>
      </div>
      <ul className="flex min-w-0 flex-col gap-3">
        {rows.map((row) => {
          const alts = row.meta.alternatives ?? []
          const held = row.meta.tier === "owner" || row.meta.tier === "bundle"
          return (
            <li
              key={row.name}
              data-property={row.name}
              className="flex min-w-0 flex-col gap-2 rounded-xl border bg-card p-4"
            >
              <div className="flex min-w-0 items-start justify-between gap-3">
                <div className="flex min-w-0 flex-col gap-1">
                  <span className="data text-xs text-muted-foreground">
                    {row.name}
                  </span>
                  <Value value={row.value} className="text-base" />
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  {row.meta.manager && <TierChip tier={row.meta.tier} />}
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
              <Origin
                actor={row.meta.manager}
                source={row.meta.source}
                mapping={
                  row.meta.source
                    ? mappingOfSource(links ?? [], row.meta.source)
                    : undefined
                }
                at={row.meta.updatedAt}
                titles={titles}
                kinds={kinds}
              />
              {alts.length > 0 && (
                <div className="mt-1 flex flex-col gap-1.5 border-t pt-2.5">
                  <p
                    className="cursor-help text-xs text-muted-foreground"
                    title={WORDS.alternative}
                  >
                    {alts.length === 1
                      ? "One live source says otherwise"
                      : `${alts.length} live sources say otherwise`}
                  </p>
                  <ul className="flex flex-col gap-1.5">
                    {alts.map((alt, i) => (
                      <li
                        key={`${alt.actor} ${alt.source ?? i}`}
                        data-alternative
                        className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 rounded-lg bg-muted/40 px-3 py-2"
                      >
                        <Value
                          value={alt.value}
                          className="text-sm font-medium text-foreground"
                        />
                        <Origin
                          actor={alt.actor}
                          source={alt.source}
                          mapping={
                            alt.source
                              ? mappingOfSource(links ?? [], alt.source)
                              : undefined
                          }
                          at={alt.updatedAt}
                          titles={titles}
                          kinds={kinds}
                        />
                        <Button
                          variant="outline"
                          size="sm"
                          className="ml-auto h-6 px-2 text-xs"
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
                    ))}
                  </ul>
                </div>
              )}
            </li>
          )
        })}
      </ul>

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
