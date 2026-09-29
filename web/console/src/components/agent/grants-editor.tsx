/** What an agent may see and change, edited by collection: each grant is a
 * row of the collections it names (a `KindRef` each, or "All your data"),
 * each with a remove, and Add opens the collections to pick from. Every pick
 * is one PATCH of the agent record's `permissions` under `ifVersion`
 * (`useRecordPatch`, the record page's own write), carrying the rest of the
 * object through (`permissionsWith`). An edit that would leave a tool the
 * agent holds without its grant is held back and says why
 * (`grantEditProblem`), because the substrate would refuse the agent at
 * load. An edit that takes a collection away asks first (`grantNarrowing`).
 *
 * Two layouts, one set of labels: `rows` is the agent page's label column,
 * `stacked` the chat panel's narrow sections. */

import { useMemo, useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { PlusIcon, XIcon } from "lucide-react"

import {
  useRecordPatch,
  writeError,
} from "@/components/property-sheet/use-record-patch"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { KindPath, KindRef } from "@/components/identity/kind-ref"
import { Button } from "@/components/ui/button"
import { ChoiceList, type ChoiceOption } from "@/components/ui/choice-list"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { agentName, canChange, joinWords } from "@/lib/agent-chat"
import {
  ALL_KINDS,
  PERMISSIONS_PROPERTY,
  grantCovers,
  grantEditProblem,
  grantKindsOf,
  grantNarrowing,
  grantOffers,
  isGrantGlob,
  nextGrant,
  permissionsWith,
  type GrantSide,
} from "@/lib/agent-grants"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindPurpose } from "@/lib/definition"
import { displayPlural } from "@/lib/kind-names"
import { kindPatternWords } from "@/lib/tools"

const ALL_WORDS = "All your data"

const SIDES: Record<
  GrantSide,
  {
    label: string
    hint: string
    add: string
    verb: string
    stopping: string
    losing: string
  }
> = {
  reads: {
    label: "Can see",
    hint: "What it can look up in your data",
    add: "Let it see",
    verb: "see",
    stopping: "seeing",
    losing: "look up",
  },
  writes: {
    label: "Can change",
    hint: "What it can add to, change and delete",
    add: "Let it change",
    verb: "change",
    stopping: "changing",
    losing: "add to, change or delete",
  },
}

/** The read view's sentence for a grant that names nothing. */
function noneWords(agent: SubstrateRecord, side: GrantSide): string {
  return side === "reads"
    ? "Nothing. It can’t look things up in your data."
    : canChange(agent)
}

/** One grant entry in words: a collection's plural, or the glob's words. */
function entryWords(entry: string): string {
  if (entry === ALL_KINDS) return ALL_WORDS
  if (isGrantGlob(entry)) return kindPatternWords(entry, displayPlural)
  return displayPlural(entry)
}

/** Entries as a phrase inside a sentence: "all your data" lowercased, a
 * collection's name as it is. */
function entriesPhrase(entries: string[]): string {
  return joinWords(
    entries.map((e) => (e === ALL_KINDS ? "all your data" : entryWords(e)))
  )
}

/** What the confirmation asks before an edit takes collections away. */
function narrowingWords(
  agent: SubstrateRecord,
  side: GrantSide,
  lost: string[],
  next: string[]
): { title: string; consequence: string; confirm: string } {
  const words = SIDES[side]
  const name = agentName(agent.id)
  if (lost.includes(ALL_KINDS) && next.length > 0) {
    return {
      title: `Only let ${name} ${words.verb} ${entriesPhrase(next)}?`,
      consequence: `It won’t be able to ${words.losing} anything else in your data.`,
      confirm: "Change",
    }
  }
  return {
    title: `Stop ${name} ${words.stopping} ${entriesPhrase(lost)}?`,
    consequence: `It won’t be able to ${words.losing} ${entriesPhrase(lost)} any more. You can add ${lost.length === 1 ? "it" : "them"} back here.`,
    confirm: "Remove",
  }
}

/** A choice's words, with the full reference under them in technical mode:
 * wrapped, never cut. */
function OptionLabel({
  glyph,
  label,
  reference,
}: {
  glyph?: ReactNode
  label: string
  reference?: string
}) {
  return (
    <span className="flex min-w-0 items-start gap-2">
      {glyph && <span className="mt-[3px] shrink-0">{glyph}</span>}
      <span className="flex min-w-0 flex-col">
        <span className="[overflow-wrap:anywhere]">{label}</span>
        {reference &&
          (isGrantGlob(reference) ? (
            <span className="font-mono text-[11.5px] [overflow-wrap:anywhere] text-faint">
              {reference}
            </span>
          ) : (
            <KindPath reference={reference} className="text-[11.5px]" />
          ))}
      </span>
    </span>
  )
}

function Chip({
  kind,
  onRemove,
  disabled,
}: {
  kind: string
  onRemove: () => void
  disabled: boolean
}) {
  const [technical] = useTechnicalDetails()
  const label = entryWords(kind)
  return (
    <li className="inline-flex max-w-full items-center gap-1 rounded-md border border-border-strong bg-background py-0.5 pr-0.5 pl-2 text-[13px]">
      {isGrantGlob(kind) ? (
        <span className="min-w-0">
          {label}
          {technical && (
            <span className="ml-1.5 font-mono text-[11.5px] [overflow-wrap:anywhere] text-faint">
              {kind}
            </span>
          )}
        </span>
      ) : (
        <KindRef kind={kind} />
      )}
      <Button
        size="icon-xs"
        variant="ghost"
        aria-label={`Remove ${label}`}
        className="text-faint hover:text-foreground"
        disabled={disabled}
        onClick={onRemove}
      >
        <XIcon />
      </Button>
    </li>
  )
}

function GrantRow({
  agent,
  side,
  registry,
  layout,
}: {
  agent: SubstrateRecord
  side: GrantSide
  registry: KindInfo[]
  layout: "rows" | "stacked"
}) {
  const [technical] = useTechnicalDetails()
  const [open, setOpen] = useState(false)
  const [problem, setProblem] = useState<string>()
  // An edit that takes something away, held until the reader confirms it:
  // the grant it leaves, the whole `permissions` it writes and the version
  // both were read from. Confirming asserts that version, so a record that
  // moved while the dialog was open is refused rather than overwritten.
  const [confirming, setConfirming] = useState<{
    next: string[]
    lost: string[]
    permissions: Record<string, unknown>
    version: number
  }>()
  const patch = useRecordPatch(agent, confirming?.version)
  const words = SIDES[side]
  const held = grantKindsOf(agent.properties, side)
  const holdsAll = held.includes(ALL_KINDS)

  const options = useMemo((): ChoiceOption[] => {
    const kinds = registry
      .filter((k) => grantOffers(side, k.identity))
      .filter((k) => technical || kindPurpose(k) === "primary")
      .map((k) => ({
        value: k.identity,
        label: displayPlural(k),
        display: (
          <OptionLabel
            glyph={<KindGlyph kind={k} size="xs" />}
            label={displayPlural(k)}
            reference={technical ? k.identity : undefined}
          />
        ),
        hint:
          holdsAll && grantCovers(ALL_KINDS, k.identity)
            ? "Included"
            : undefined,
      }))
      .sort((a, b) => a.label.localeCompare(b.label))
    const extra = held
      .filter((k) => k !== ALL_KINDS && !registry.some((r) => r.identity === k))
      .map((k) => ({
        value: k,
        label: entryWords(k),
        display: (
          <OptionLabel
            label={entryWords(k)}
            reference={technical ? k : undefined}
          />
        ),
      }))
    return [
      {
        value: ALL_KINDS,
        label: ALL_WORDS,
        display: (
          <OptionLabel
            label={ALL_WORDS}
            reference={technical ? ALL_KINDS : undefined}
          />
        ),
      },
      ...extra,
      ...kinds,
    ]
  }, [registry, technical, held, holdsAll, side])

  function save(picked: string[]) {
    const next = nextGrant(held, picked)
    const reason = grantEditProblem(agent.properties, side, next)
    setProblem(reason)
    if (reason) return
    const permissions = permissionsWith(agent.properties, side, next)
    const lost = grantNarrowing(held, next)
    if (lost.length > 0) {
      setOpen(false)
      patch.reset()
      setConfirming({ next, lost, permissions, version: agent.version })
      return
    }
    patch.mutate({ [PERMISSIONS_PROPERTY]: permissions })
  }

  const labelId = `grant-${side}-${agent.id}`
  const chips = (
    <div className="flex min-w-0 flex-col gap-1.5">
      <ul
        aria-labelledby={labelId}
        className="flex flex-wrap items-center gap-1.5"
      >
        {held.length === 0 && (
          <li className="py-1 text-[13px] text-muted-foreground">
            {noneWords(agent, side)}
          </li>
        )}
        {held.map((kind) => (
          <Chip
            key={kind}
            kind={kind}
            disabled={patch.isPending}
            onRemove={() => save(held.filter((k) => k !== kind))}
          />
        ))}
        <li>
          <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger
              render={
                <Button
                  size="xs"
                  variant="ghost"
                  className="text-muted-foreground"
                  disabled={patch.isPending}
                />
              }
            >
              <PlusIcon />
              Add
            </PopoverTrigger>
            <PopoverContent align="start" className="w-72 p-1">
              <ChoiceList
                label={words.add}
                heading={words.add}
                multiple
                filter
                showValues={false}
                options={options.map((o) => ({
                  ...o,
                  disabled: patch.isPending,
                }))}
                selected={held}
                onChange={(next) => save(next)}
              />
            </PopoverContent>
          </Popover>
        </li>
      </ul>
      {problem && (
        <p role="alert" className="text-[12.5px] text-warning">
          {problem}
        </p>
      )}
      {patch.error && !confirming && (
        <p role="alert" className="text-[12.5px] text-destructive">
          {writeError(patch.error)}
        </p>
      )}
      {confirming && (
        <ConfirmDialog
          {...narrowingWords(agent, side, confirming.lost, confirming.next)}
          destructive
          pending={patch.isPending}
          error={patch.error ? writeError(patch.error) : undefined}
          onConfirm={() =>
            patch.mutate(
              { [PERMISSIONS_PROPERTY]: confirming.permissions },
              { onSuccess: () => setConfirming(undefined) }
            )
          }
          onClose={() => {
            patch.reset()
            setConfirming(undefined)
          }}
        />
      )}
    </div>
  )

  if (layout === "stacked") {
    return (
      <section className="flex flex-col gap-1.5 text-[13px]">
        <h3 id={labelId} className="text-[11.5px] font-medium text-faint">
          {words.label}
        </h3>
        {chips}
      </section>
    )
  }
  return (
    <div className="grid grid-cols-[minmax(96px,120px)_minmax(0,1fr)] gap-x-3 gap-y-1 py-2 sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)]">
      <div className="flex flex-col pt-1">
        <span id={labelId} className="text-[13.5px] text-muted-foreground">
          {words.label}
        </span>
        <span className="text-[12px] text-faint">{words.hint}</span>
      </div>
      {chips}
    </div>
  )
}

export function GrantsEditor({
  agent,
  layout = "rows",
}: {
  agent: SubstrateRecord
  /** `rows` beside a label column (the agent page), `stacked` under a small
   * heading (the chat panel). */
  layout?: "rows" | "stacked"
}) {
  const registry = useQuery(kindsQueryOptions)
  const kinds = registry.data ?? []
  // Keyed on the agent: the chat panel swaps agents under one editor, and a
  // pending confirmation, problem or error belongs to the agent it was for.
  return (
    <div
      data-slot="grants-editor"
      className={
        layout === "stacked" ? "flex flex-col gap-5" : "flex flex-col divide-y"
      }
    >
      <GrantRow
        key={`${agent.id}:reads`}
        agent={agent}
        side="reads"
        registry={kinds}
        layout={layout}
      />
      <GrantRow
        key={`${agent.id}:writes`}
        agent={agent}
        side="writes"
        registry={kinds}
        layout={layout}
      />
    </div>
  )
}
