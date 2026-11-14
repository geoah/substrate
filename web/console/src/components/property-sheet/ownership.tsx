/** Where one value comes from, at the end of its row: a labelled chip ("You",
 * or the provider's badge and name), an amber "<Provider> differs" pill when
 * a live source offers something else, one hover summary, and a click that
 * opens the detail under the row: who set it and whether it is pinned, the
 * source records behind it, what each source record says instead, and the
 * writes that choose between them (decision 0284). "Use <Provider>’s" writes
 * that value, which pins it as yours; "Stop overriding" releases the
 * property, so sync picks it again among the source records; "Keep this
 * value" pins a value sync wrote. Each asks first. */

import { useState, type ReactNode } from "react"
import { Undo2Icon } from "lucide-react"

import { ago } from "./dates"
import { DeclaredValue } from "./property-value"
import { type SheetRow } from "./sheet-rows"
import { useMarkAction, useRecordPatch, writeError } from "./use-record-patch"
import { ActorRef } from "@/components/identity/actor-ref"
import { IdText } from "@/components/identity/id-text"
import { IdentityHoverCard } from "@/components/identity/identity-hover-card"
import { OriginMark } from "@/components/identity/origin-mark"
import { Pill } from "@/components/identity/pill"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { RecordRef } from "@/components/identity/record-ref"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { providerOfKind } from "@/lib/actor-identity"
import type {
  PropertyAlternative,
  PropertyMark,
  SubstrateRecord,
} from "@/lib/api/types"
import {
  differsLabel,
  holderOf,
  offerSource,
  setByOf,
  sourceName,
  standingExplanation,
  standingLabel,
  standingOf,
  unionMembers,
  type Standing,
} from "@/lib/provenance"
import { splitRecordPath } from "@/lib/record-path"
import { typeLabel, type PropSpec } from "@/lib/record-schema"
import { cn } from "@/lib/utils"
import { scalarText } from "@/lib/format"
import { lowerFirst } from "@/lib/kind-names"
import { originOfActor, originOfKind } from "@/lib/origin"

const STANDING_TONES: Record<Standing, string> = {
  yours: "bg-primary-soft text-primary-text",
  synced: "bg-ok-soft text-ok",
  agent: "bg-warn-soft text-warning",
  provider: "bg-warn-soft text-warning",
  app: "bg-warn-soft text-warning",
  pinned: "bg-warn-soft text-warning",
  unpinned: "bg-hover text-muted-foreground",
  none: "bg-hover text-muted-foreground",
}

export function StandingTag({ mark }: { mark: PropertyMark | undefined }) {
  const [technical] = useTechnicalDetails()
  const standing = standingOf(mark)
  return (
    <span
      data-standing={standing}
      className={cn(
        "rounded-[4px] px-[7px] text-[11.5px] leading-[19px] font-medium whitespace-nowrap",
        STANDING_TONES[standing]
      )}
    >
      {standingLabel(standing)}
      {technical && mark ? ` · ${mark.pinned ? "pinned" : "not pinned"}` : ""}
    </span>
  )
}

/** The chip's mark and words. */
function Holding({ mark }: { mark: PropertyMark }) {
  const holder = holderOf(mark)
  if (!holder) return null
  const by = setByOf(mark)
  const source = mark.derivedFrom?.[0]?.ref
  const origin = by
    ? originOfActor(by)
    : originOfKind(splitRecordPath(source ?? "")?.kind ?? "")
  return (
    <span
      data-slot="owner-chip"
      data-holder={holder.mark}
      className="inline-flex h-[22px] items-center rounded-full border border-border-strong bg-background pr-2 pl-[3px] text-xs whitespace-nowrap text-muted-foreground group-aria-expanded/own:border-primary group-aria-expanded/own:text-primary-text"
    >
      <OriginMark origin={origin} short />
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

/** One sentence: who set it and since when, through which mapping, and
 * what the sources say. */
function summaryOf(
  mark: PropertyMark,
  spec: PropSpec,
  through?: Through
): string {
  const holder = holderOf(mark)
  const who =
    holder?.mark === "you"
      ? "You set this"
      : setByOf(mark)
        ? `${holder?.label} set this`
        : `Kept up to date by ${holder?.label ?? "a provider"}`
  const when = mark.setAt ? ` · ${ago(mark.setAt)}` : ""
  const alts = (mark.alternatives ?? [])
    .map((a) => `${sourceName(offerSource(a))} says “${plain(a.value, spec)}”`)
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
  return scalarText(value)
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
  const mark = row.meta
  if (!mark || !holderOf(mark)) return null
  const differs = differsLabel(mark)
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
              <StandingTag mark={mark} />
            </div>
            <div className="text-muted-foreground">
              {summaryOf(mark, row.spec, through)}
            </div>
          </div>
          <div className="border-t bg-panel px-3 py-[7px] text-[11.5px] text-faint">
            Click to see details and choose a different value
          </div>
        </div>
      }
    >
      {differs && (
        <Pill tone="warn" dot={false}>
          {differs}
        </Pill>
      )}
      <Holding mark={mark} />
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
      {technical && <IdText value={path} copy className="text-[11.5px]" />}
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

/** A write awaiting its confirmation, and the version the reader chose it
 * on: a re-read while the dialog is open must not carry it past a change. */
type Pending = { base: number } & (
  | { kind: "use"; alternative: PropertyAlternative }
  | { kind: "release"; follow: string }
  | { kind: "pin" }
)

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
  const mark: PropertyMark = row.meta ?? {}
  const standing = standingOf(row.meta)
  const holder = holderOf(mark)
  const by = setByOf(mark)
  const alts = mark.alternatives ?? []
  const union = unionMembers(row.value, mark)
  const [pending, setPending] = useState<Pending | null>(null)
  const patch = useRecordPatch(record, pending?.base)
  const action = useMarkAction(record, pending?.base)
  const [failed, setFailed] = useState<string>()
  const who = holder?.label ?? "Someone"
  const when = mark.setAt && <span title={mark.setAt}>{ago(mark.setAt)}</span>

  let head: ReactNode
  if (standing === "yours") {
    head = (
      <Line>
        <StandingTag mark={mark} />
        <b className="font-medium text-foreground">{who} set this</b>
        {when}
      </Line>
    )
  } else if (mark.pinned || by) {
    head = (
      <Line>
        <StandingTag mark={mark} />
        <b className="font-medium text-foreground">Set by {who}</b>
        {when}
      </Line>
    )
  } else {
    head = (
      <Line>
        <StandingTag mark={mark} />
        <b className="font-medium text-foreground">Kept up to date by {who}</b>
        {mark.setAt && (
          <span title={mark.setAt}>last synced {ago(mark.setAt)}</span>
        )}
      </Line>
    )
  }

  // Whom a release follows: a source offering something else, or else one
  // that offers the pinned value itself; none where nothing offers one.
  const followed = alts.length
    ? offerSource(alts[0])
    : mark.derivedFrom?.[0]?.ref
  const follow = followed ? sourceName(followed) : ""
  const close = () => {
    setPending(null)
    setFailed(undefined)
  }
  const done = {
    onSuccess: close,
    onError: (error: Error) => setFailed(writeError(error)),
  }

  return (
    <div
      data-slot="ownership-detail"
      className="col-span-full my-0.5 mb-3 flex min-w-0 flex-col gap-2.5 rounded-lg border bg-panel px-3.5 py-3 text-[13px]"
    >
      {head}
      {(mark.derivedFrom ?? []).length > 0 && (
        <Line>
          <span>From</span>
          {(mark.derivedFrom ?? []).map((l) => (
            <Source key={l.ref} path={l.ref} />
          ))}
        </Line>
      )}
      {through && <ThroughLine through={through} />}
      <div className="text-[12.5px] text-faint">
        {readOnly
          ? "This is a copy of what the provider has. Change it there and it updates here."
          : standingExplanation(standing)}
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
              const source = offerSource(alt)
              const provider = providerOfKind(
                splitRecordPath(source)?.kind ?? ""
              )
              const name = sourceName(source)
              return (
                <div
                  key={source || i}
                  data-alternative
                  className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 border-t py-2"
                >
                  <span className="min-w-0 font-medium">
                    <AltValue value={alt.value} spec={row.spec} />
                  </span>
                  <span className="flex flex-wrap items-center gap-1.5 text-xs text-faint">
                    {provider && (
                      <ProviderBadge provider={provider} size="xs" />
                    )}
                    {name} has this
                    {source && (
                      <>
                        <span>· from</span>
                        <Source path={source} />
                      </>
                    )}
                    <span title={alt.updatedAt}>· {ago(alt.updatedAt)}</span>
                  </span>
                  {!readOnly && (
                    <span className="col-start-2 row-span-2 row-start-1">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          setPending({
                            kind: "use",
                            alternative: alt,
                            base: record.version,
                          })
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
      {(!readOnly && (mark.pinned || standing !== "none")) || technical ? (
        <div className="flex flex-wrap items-center gap-2 border-t pt-2.5">
          {!readOnly && mark.pinned && (
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                setPending({ kind: "release", follow, base: record.version })
              }
            >
              <Undo2Icon />
              {alts.length > 0
                ? `Stop overriding · follow ${follow}`
                : follow
                  ? `Stop keeping · follow ${follow}`
                  : "Stop keeping this value"}
            </Button>
          )}
          {!readOnly && !mark.pinned && standing !== "none" && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => setPending({ kind: "pin", base: record.version })}
            >
              Keep this value
            </Button>
          )}
          {!readOnly && mark.pinned && standing !== "yours" && onEdit && (
            <Button size="sm" variant="outline" onClick={onEdit}>
              Use my own value
            </Button>
          )}
          {technical && (
            <span className="ml-auto flex flex-wrap items-center gap-1 font-mono text-[11.5px] text-faint">
              {row.name} · {typeLabel(row.spec)} · set by{" "}
              {by ? <ActorRef actor={by} link={false} /> : "sync"}
            </span>
          )}
        </div>
      ) : null}

      {pending && (
        <ConfirmDialog
          title={
            pending.kind === "use"
              ? `Use ${sourceName(offerSource(pending.alternative))}’s ${lowerFirst(row.spec.label)}?`
              : pending.kind === "release"
                ? pending.follow
                  ? `Follow ${pending.follow} for ${lowerFirst(row.spec.label)}?`
                  : `Stop keeping this ${lowerFirst(row.spec.label)}?`
                : `Keep this ${lowerFirst(row.spec.label)}?`
          }
          consequence={
            pending.kind === "use" ? (
              <>
                “{plain(pending.alternative.value, row.spec)}” replaces the
                current value and becomes yours. Later changes from{" "}
                {sourceName(offerSource(pending.alternative))} show up here as
                another version but won’t replace it, until you choose to follow
                it again.
              </>
            ) : pending.kind === "release" && pending.follow ? (
              <>
                Your value is no longer kept. {row.spec.label} takes what{" "}
                {pending.follow} has now, and changes whenever it changes there.
              </>
            ) : pending.kind === "release" ? (
              <>
                Your value is no longer kept, so a sync may replace it. No
                source offers one now: where a sync fills{" "}
                {lowerFirst(row.spec.label)}, it is cleared until one does,
                unless it is required.
              </>
            ) : (
              <>
                Syncs leave “{plain(row.value, row.spec)}” as it is. What they
                offer shows up here as another version, until you choose to
                follow it again.
              </>
            )
          }
          confirm={
            pending.kind === "use"
              ? `Use ${sourceName(offerSource(pending.alternative))}’s`
              : pending.kind === "release"
                ? pending.follow
                  ? `Follow ${pending.follow}`
                  : "Stop keeping"
                : "Keep this value"
          }
          pending={patch.isPending || action.isPending}
          error={failed}
          onConfirm={() =>
            pending.kind === "use"
              ? patch.mutate({ [row.name]: pending.alternative.value }, done)
              : action.mutate(
                  { action: pending.kind, property: row.name },
                  done
                )
          }
          onClose={close}
        />
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
