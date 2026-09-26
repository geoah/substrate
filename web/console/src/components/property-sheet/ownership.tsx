/** Where one value comes from, at the end of its row: a labelled chip ("You",
 * or the provider's badge and name), an amber "<Provider> differs" pill when
 * a live source offers something else, one hover summary, and a click that
 * opens the detail under the row: who holds it at which tier, the source
 * record, what each source says instead, and the two writes that choose
 * between them. Both writes are the ordinary PATCH docs/projection.md
 * describes: "Use <Provider>’s" writes that value, which makes it yours;
 * "Stop overriding" patches the property to null, so the same write refills
 * it from the live sources and it follows them again. Each asks first. */

import { useState, type ReactNode } from "react"
import { Undo2Icon, UserRoundIcon } from "lucide-react"

import { ago } from "./dates"
import { DeclaredValue } from "./property-value"
import { type SheetRow } from "./sheet-rows"
import { useRecordPatch, writeError } from "./use-record-patch"
import { ActorMark, ActorRef } from "@/components/identity/actor-ref"
import { IdText } from "@/components/identity/id-text"
import { IdentityHoverCard } from "@/components/identity/identity-hover-card"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { RecordRef } from "@/components/identity/record-ref"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Spinner } from "@/components/ui/spinner"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { actorIdentity } from "@/lib/actor-identity"
import type {
  PropertyAlternative,
  PropertyMeta,
  SubstrateRecord,
} from "@/lib/api/types"
import {
  differsLabel,
  holderOf,
  sourceName,
  tierExplanation,
  tierLabel,
  unionMembers,
  type Tier,
} from "@/lib/provenance"
import { splitRecordPath } from "@/lib/record-path"
import { typeLabel, type PropSpec } from "@/lib/record-schema"
import { cn } from "@/lib/utils"
import { lowerFirst } from "@/lib/kind-names"

const TIER_TONES: Record<Tier, string> = {
  owner: "bg-primary-soft text-primary-text",
  machine: "bg-ok-soft text-ok",
  bundle: "bg-warn-soft text-warning",
}

export function TierTag({
  tier,
  actor,
}: {
  tier: Tier | undefined
  actor?: string
}) {
  const [technical] = useTechnicalDetails()
  return (
    <span
      data-tier={tier ?? "none"}
      className={cn(
        "rounded-[4px] px-[7px] text-[11.5px] leading-[19px] font-medium whitespace-nowrap",
        tier ? TIER_TONES[tier] : "bg-hover text-muted-foreground"
      )}
    >
      {tierLabel(tier, actor)}
      {technical && tier ? ` · ${tier}` : ""}
    </span>
  )
}

/** The chip's mark and words. */
function Holding({ meta }: { meta: PropertyMeta }) {
  const holder = holderOf(meta)
  if (!holder) return null
  return (
    <span
      data-slot="owner-chip"
      data-holder={holder.mark}
      className="inline-flex h-[22px] items-center gap-[5px] rounded-full border border-border-strong bg-background pr-2 pl-[5px] text-xs whitespace-nowrap text-muted-foreground group-aria-expanded/own:border-primary group-aria-expanded/own:text-primary-text"
    >
      {holder.mark === "you" ? (
        <UserRoundIcon aria-hidden className="size-3" />
      ) : holder.mark === "provider" ? (
        <ProviderBadge
          provider={holder.identity.provider!}
          size="xs"
          className="size-3.5 border-0 text-[10.5px]"
        />
      ) : (
        <ActorMark identity={holder.identity} size="xs" />
      )}
      {holder.label}
    </span>
  )
}

/** The mapping that links a synced value's source here. */
export interface Through {
  /** The mapping's id, its reference. */
  id: string
  /** In a reader's words. */
  label: string
}

/** One sentence: who holds it and since when, through which mapping, and
 * what the sources say. */
function summaryOf(
  meta: PropertyMeta,
  spec: PropSpec,
  through?: Through
): string {
  const holder = holderOf(meta)
  const who =
    holder?.mark === "you"
      ? "You set this"
      : meta.tier === "bundle"
        ? `${holder?.label} set this`
        : `Kept up to date by ${holder?.label ?? "a provider"}`
  const when = meta.updatedAt ? ` · ${ago(meta.updatedAt)}` : ""
  const alts = (meta.alternatives ?? [])
    .map((a) => `${sourceName(a.actor)} says “${plain(a.value, spec)}”`)
    .join(". ")
  const via = through ? ` Linked through ${through.label}.` : ""
  return `${who}${when}.${via}${alts ? ` ${alts}.` : ""}`
}

/** A value in a sentence. */
function plain(value: unknown, spec: PropSpec): string {
  if (value === null || value === undefined) return "nothing"
  if (spec.kind === "reference") return "another record"
  if (Array.isArray(value)) return value.map(String).join(", ")
  if (typeof value === "object") return JSON.stringify(value)
  return String(value)
}

export function OwnershipChip({
  row,
  open,
  onToggle,
  through,
}: {
  row: SheetRow
  open: boolean
  onToggle: () => void
  through?: Through
}) {
  const meta = row.meta
  if (!meta?.manager) return null
  const differs = differsLabel(meta)
  return (
    <IdentityHoverCard
      trigger={
        <button
          type="button"
          aria-expanded={open}
          onClick={(e) => {
            e.stopPropagation()
            onToggle()
          }}
          onKeyDown={(e) => e.stopPropagation()}
        />
      }
      label={`Where ${row.spec.label} comes from`}
      className="group/own inline-flex shrink-0 cursor-pointer items-center gap-1.5 rounded-[5px] px-1 py-0.5 outline-none hover:bg-border focus-visible:ring-2 focus-visible:ring-ring/50 sm:ml-auto"
      card={
        <div className="flex flex-col">
          <div className="flex flex-col gap-1.5 px-3 pt-3 pb-2.5">
            <div>
              <TierTag tier={meta.tier} actor={meta.manager} />
            </div>
            <div className="text-muted-foreground">
              {summaryOf(meta, row.spec, through)}
            </div>
          </div>
          <div className="border-t bg-panel px-3 py-[7px] text-[11.5px] text-faint">
            Click to see details and choose a different value
          </div>
        </div>
      }
    >
      {differs && (
        <span
          data-slot="differs"
          className="rounded-full bg-warn-soft px-[7px] text-[11.5px] font-medium whitespace-nowrap text-warning"
        >
          {differs}
        </span>
      )}
      <Holding meta={meta} />
    </IdentityHoverCard>
  )
}

/** A source record by its reference, and its path in technical mode. */
function Source({ path }: { path: string }) {
  const [technical] = useTechnicalDetails()
  const target = splitRecordPath(path)
  if (!target) return <span className="font-mono text-xs">{path}</span>
  return (
    <>
      <RecordRef kind={target.kind} id={target.id} />
      {technical && <IdText value={path} copy className="text-[11px]" />}
    </>
  )
}

/** The mapping a synced value came through: its words, and in technical mode
 * its id to copy. */
function ThroughLine({ through }: { through: Through }) {
  const [technical] = useTechnicalDetails()
  return (
    <Line>
      <span>Linked through</span>
      <b className="font-medium text-foreground">{through.label}</b>
      {technical && <IdText value={through.id} copy />}
    </Line>
  )
}

function Line({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5 text-muted-foreground">
      {children}
    </div>
  )
}

function SubHead({ children }: { children: ReactNode }) {
  return (
    <div className="-mb-1 text-[11.5px] font-medium text-faint">{children}</div>
  )
}

type Pending =
  | { kind: "use"; alternative: PropertyAlternative }
  | { kind: "release"; follow: string }

export function OwnershipDetail({
  row,
  record,
  readOnly,
  onEdit,
  through,
}: {
  row: SheetRow
  record: SubstrateRecord
  /** The mapping behind a synced value. */
  through?: Through
  /** A provider's own copy: no writes offered. */
  readOnly?: boolean
  /** Opens the row's editor ("Use my own value"). */
  onEdit?: () => void
}) {
  const [technical] = useTechnicalDetails()
  const meta = row.meta ?? {}
  const holder = holderOf(meta)
  const alts = meta.alternatives ?? []
  const union = unionMembers(row.value, meta)
  const [pending, setPending] = useState<Pending | null>(null)
  const patch = useRecordPatch(record)
  const [failed, setFailed] = useState<string>()
  const who = holder?.label ?? "Someone"

  let head: ReactNode
  if (meta.tier === "owner" || holder?.mark === "you") {
    head = (
      <Line>
        <TierTag tier={meta.tier} actor={meta.manager} />
        <b className="font-medium text-foreground">{who} set this</b>
        {meta.updatedAt && (
          <span title={meta.updatedAt}>{ago(meta.updatedAt)}</span>
        )}
      </Line>
    )
  } else if (meta.tier === "bundle") {
    head = (
      <Line>
        <TierTag tier={meta.tier} actor={meta.manager} />
        <b className="font-medium text-foreground">Set by {who}</b>
        {meta.updatedAt && (
          <span title={meta.updatedAt}>{ago(meta.updatedAt)}</span>
        )}
      </Line>
    )
  } else {
    head = (
      <Line>
        <TierTag tier={meta.tier} actor={meta.manager} />
        <b className="font-medium text-foreground">Kept up to date by {who}</b>
        {meta.updatedAt && (
          <span title={meta.updatedAt}>last synced {ago(meta.updatedAt)}</span>
        )}
      </Line>
    )
  }

  const follow = alts.length ? sourceName(alts[0].actor) : ""

  return (
    <div
      data-slot="ownership-detail"
      className="col-span-full my-0.5 mb-3 flex min-w-0 flex-col gap-2.5 rounded-lg border bg-panel px-3.5 py-3 text-[13px]"
    >
      {head}
      {meta.tier === "machine" && meta.source && (
        <Line>
          <span>From</span>
          <Source path={meta.source} />
        </Line>
      )}
      {through && <ThroughLine through={through} />}
      <div className="text-[12.5px] text-faint">
        {readOnly
          ? "This is a copy of what the provider has. Change it there and it updates here."
          : tierExplanation(meta.tier)}
      </div>
      {union.some((u) => u.sources.length > 0) && (
        <>
          <SubHead>Where each one comes from</SubHead>
          <div className="flex flex-col gap-1.5">
            {union.map((u, i) => (
              <Line key={i}>
                <b className="font-medium text-foreground">
                  {plain(u.item, row.spec)}
                </b>
                {u.sources.length ? (
                  <>
                    <span>from</span>
                    {u.sources.map((s) => (
                      <Source key={s} path={s} />
                    ))}
                  </>
                ) : (
                  <span>added here</span>
                )}
              </Line>
            ))}
          </div>
        </>
      )}
      {alts.length > 0 && (
        <>
          <SubHead>Other versions</SubHead>
          <div className="flex flex-col">
            {alts.map((alt, i) => {
              const identity = actorIdentity(alt.actor)
              const name = sourceName(alt.actor)
              return (
                <div
                  key={`${alt.actor} ${alt.source ?? i}`}
                  data-alternative
                  className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 border-t py-2"
                >
                  <span className="min-w-0 font-medium">
                    <AltValue value={alt.value} spec={row.spec} />
                  </span>
                  <span className="flex flex-wrap items-center gap-1.5 text-xs text-faint">
                    {identity.provider ? (
                      <ProviderBadge provider={identity.provider} size="xs" />
                    ) : (
                      <ActorMark identity={identity} size="xs" />
                    )}
                    {name} has this
                    {alt.source && (
                      <>
                        <span>· from</span>
                        <Source path={alt.source} />
                      </>
                    )}
                    {technical && (
                      <span className="font-mono text-[11px] [overflow-wrap:anywhere]">
                        · {alt.actor}
                      </span>
                    )}
                    <span title={alt.updatedAt}>· {ago(alt.updatedAt)}</span>
                  </span>
                  {!readOnly && (
                    <span className="col-start-2 row-span-2 row-start-1">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          setPending({ kind: "use", alternative: alt })
                        }
                      >
                        Use {name}’s
                      </Button>
                    </span>
                  )}
                </div>
              )
            })}
          </div>
        </>
      )}
      {(!readOnly &&
        ((meta.tier === "owner" && alts.length > 0) ||
          meta.tier === "bundle")) ||
      technical ? (
        <div className="flex flex-wrap items-center gap-2 border-t pt-2.5">
          {!readOnly && meta.tier === "owner" && alts.length > 0 && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => setPending({ kind: "release", follow })}
            >
              <Undo2Icon />
              Stop overriding · follow {follow}
            </Button>
          )}
          {!readOnly && meta.tier === "bundle" && onEdit && (
            <Button size="sm" variant="outline" onClick={onEdit}>
              Use my own value
            </Button>
          )}
          {technical && (
            <span className="ml-auto flex flex-wrap items-center gap-1 font-mono text-[11.5px] text-faint">
              {row.name} · {typeLabel(row.spec)} · manager{" "}
              {meta.manager ? (
                <ActorRef actor={meta.manager} link={false} />
              ) : (
                "none"
              )}
            </span>
          )}
        </div>
      ) : null}

      {pending && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open && !patch.isPending) {
              setPending(null)
              setFailed(undefined)
            }
          }}
        >
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>
                {pending.kind === "use"
                  ? `Use ${sourceName(pending.alternative.actor)}’s ${lowerFirst(row.spec.label)}?`
                  : `Follow ${pending.follow} for ${lowerFirst(row.spec.label)}?`}
              </DialogTitle>
              <DialogDescription>
                {pending.kind === "use" ? (
                  <>
                    “{plain(pending.alternative.value, row.spec)}” replaces the
                    current value and becomes yours. Later changes from{" "}
                    {sourceName(pending.alternative.actor)} show up here as
                    another version but won’t replace it, until you choose to
                    follow it again.
                  </>
                ) : (
                  <>
                    Your value is removed. {row.spec.label} takes what{" "}
                    {pending.follow} has now, and changes whenever it changes
                    there.
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
                disabled={patch.isPending}
                onClick={() => {
                  setPending(null)
                  setFailed(undefined)
                }}
              >
                Cancel
              </Button>
              <Button
                disabled={patch.isPending}
                onClick={() =>
                  patch.mutate(
                    {
                      [row.name]:
                        pending.kind === "use"
                          ? pending.alternative.value
                          : null,
                    },
                    {
                      onSuccess: () => {
                        setPending(null)
                        setFailed(undefined)
                      },
                      onError: (error) => setFailed(writeError(error)),
                    }
                  )
                }
              >
                {patch.isPending && <Spinner className="size-3.5" />}
                {pending.kind === "use"
                  ? `Use ${sourceName(pending.alternative.actor)}’s`
                  : `Follow ${pending.follow}`}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}

function AltValue({ value, spec }: { value: unknown; spec: PropSpec }) {
  if (
    spec.kind === "reference" ||
    (typeof value === "object" && value !== null)
  ) {
    return <DeclaredValue spec={spec} value={value} />
  }
  return <>“{plain(value, spec)}”</>
}
