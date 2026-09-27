/** What an agent may see and change, edited by collection: each grant is a
 * row of the collections it names (a `KindRef` each, or "All your data"),
 * each with a remove, and Add opens the collections to pick from. Every pick
 * is one PATCH of the agent record's `permissions` under `ifVersion`
 * (`useRecordPatch`), carrying the rest of the object through
 * (`permissionsWith`). An edit that would leave a tool the agent holds
 * without its grant is held back and says why (`grantEditProblem`), because
 * the substrate would refuse the agent at load. */

import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { PlusIcon, XIcon } from "lucide-react"

import {
  useRecordPatch,
  writeError,
} from "@/components/property-sheet/use-record-patch"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { KindRef } from "@/components/identity/kind-ref"
import { Button } from "@/components/ui/button"
import { ChoiceList, type ChoiceOption } from "@/components/ui/choice-list"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  PERMISSIONS_PROPERTY,
  grantEditProblem,
  grantKindsOf,
  permissionsWith,
  type GrantSide,
} from "@/lib/agent-grants"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindPurpose } from "@/lib/definition"
import { displayPlural } from "@/lib/kind-names"
import { kindPatternWords } from "@/lib/tools"

const ALL = "*"

const SIDES: Record<
  GrantSide,
  { label: string; hint: string; none: string; add: string }
> = {
  reads: {
    label: "Can see",
    hint: "What it can look up in your data",
    none: "Nothing. It can’t look things up in your data.",
    add: "Let it see",
  },
  writes: {
    label: "Can change",
    hint: "What it can add to, change and delete",
    none: "Nothing on its own.",
    add: "Let it change",
  },
}

/** A grant entry that is not one kind, in words. */
function patternWords(pattern: string): string {
  if (pattern === ALL) return "All your data"
  return kindPatternWords(pattern, displayPlural)
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
  const exact = kind !== ALL && !kind.endsWith("/*")
  const label = exact ? displayPlural(kind) : patternWords(kind)
  return (
    <li className="inline-flex max-w-full items-center gap-1 rounded-md border border-border-strong bg-background py-0.5 pr-0.5 pl-2 text-[13px]">
      {exact ? (
        <KindRef kind={kind} />
      ) : (
        <span className="min-w-0">
          {label}
          {technical && (
            <span className="ml-1.5 font-mono text-[11.5px] text-faint">
              {kind}
            </span>
          )}
        </span>
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
}: {
  agent: SubstrateRecord
  side: GrantSide
  registry: KindInfo[]
}) {
  const [technical] = useTechnicalDetails()
  const [open, setOpen] = useState(false)
  const [problem, setProblem] = useState<string>()
  const patch = useRecordPatch(agent)
  const words = SIDES[side]
  const held = grantKindsOf(agent.properties, side)

  const options = useMemo((): ChoiceOption[] => {
    const kinds = registry
      .filter((k) => technical || kindPurpose(k) === "primary")
      .map((k) => ({
        value: k.identity,
        label: displayPlural(k),
        display: (
          <span className="flex min-w-0 items-center gap-2">
            <KindGlyph kind={k} size="xs" />
            <span className="truncate">{displayPlural(k)}</span>
          </span>
        ),
      }))
      .sort((a, b) => a.label.localeCompare(b.label))
    const extra = held
      .filter((k) => k !== ALL && !registry.some((r) => r.identity === k))
      .map((k) => ({ value: k, label: patternWords(k) }))
    return [{ value: ALL, label: "All your data" }, ...extra, ...kinds]
  }, [registry, technical, held])

  function save(next: string[]) {
    const reason = grantEditProblem(agent.properties, side, next)
    setProblem(reason)
    if (reason) return
    patch.mutate({
      [PERMISSIONS_PROPERTY]: permissionsWith(agent.properties, side, next),
    })
  }

  const labelId = `grant-${side}`
  return (
    <div className="grid grid-cols-[minmax(96px,120px)_minmax(0,1fr)] gap-x-3 gap-y-1 py-2 sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)]">
      <div className="flex flex-col pt-1">
        <span id={labelId} className="text-[13.5px] text-muted-foreground">
          {words.label}
        </span>
        <span className="text-[12px] text-faint">{words.hint}</span>
      </div>
      <div className="flex min-w-0 flex-col gap-1.5">
        <ul
          aria-labelledby={labelId}
          className="flex flex-wrap items-center gap-1.5"
        >
          {held.length === 0 && (
            <li className="py-1 text-[13px] text-muted-foreground">
              {words.none}
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
        {patch.error && (
          <p role="alert" className="text-[12.5px] text-destructive">
            {writeError(patch.error)}
          </p>
        )}
      </div>
    </div>
  )
}

export function GrantsEditor({ agent }: { agent: SubstrateRecord }) {
  const registry = useQuery(kindsQueryOptions)
  return (
    <div data-slot="grants-editor" className="flex flex-col divide-y">
      <GrantRow agent={agent} side="reads" registry={registry.data ?? []} />
      <GrantRow agent={agent} side="writes" registry={registry.data ?? []} />
    </div>
  )
}
